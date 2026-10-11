// Package fakes3 is an in-memory, versioned S3 bucket for tests. It implements the calls the audit anchor
// writer and verifier make, with pagination, and keeps every version of a key (like a bucket with Object Lock,
// which always has versioning on).
package fakes3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// DefaultPageSize matches S3's maximum page size for listings.
const DefaultPageSize = 1000

type entry struct {
	key          string
	versionID    string
	body         []byte
	lastModified time.Time
	deleteMarker bool
}

// Bucket is a fake versioned bucket. The zero value isn't usable; call New.
type Bucket struct {
	// PageSize is the number of keys (or versions) per listing page.
	PageSize int

	mu       sync.Mutex
	entries  map[string][]*entry // oldest first
	clock    time.Time
	versions int
}

// New returns an empty bucket whose clock starts at start. Each write without an explicit time is stamped one
// second after the latest write so far.
func New(start time.Time) *Bucket {
	return &Bucket{PageSize: DefaultPageSize, entries: map[string][]*entry{}, clock: start}
}

// PutAt stores a new version of key written at modified.
func (b *Bucket) PutAt(key string, body []byte, modified time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.add(&entry{key: key, body: bytes.Clone(body), lastModified: modified})
}

// AddDeleteMarker hides key behind a delete marker, as an unversioned DELETE does in a versioned bucket.
func (b *Bucket) AddDeleteMarker(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.add(&entry{key: key, deleteMarker: true, lastModified: b.clock.Add(time.Second)})
}

// Versions returns the bodies of every version of key, oldest first.
func (b *Bucket) Versions(key string) [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	bodies := make([][]byte, 0, len(b.entries[key]))
	for _, e := range b.entries[key] {
		if !e.deleteMarker {
			bodies = append(bodies, bytes.Clone(e.body))
		}
	}
	return bodies
}

func (b *Bucket) add(e *entry) string {
	b.versions++
	e.versionID = fmt.Sprintf("v%08d", b.versions)
	if e.lastModified.After(b.clock) {
		b.clock = e.lastModified
	}
	b.entries[e.key] = append(b.entries[e.key], e)
	return e.versionID
}

// latest returns the current entry of key (nil if there is none or it's a delete marker).
func (b *Bucket) latest(key string) *entry {
	versions := b.entries[key]
	if len(versions) == 0 || versions[len(versions)-1].deleteMarker {
		return nil
	}
	return versions[len(versions)-1]
}

func (b *Bucket) sortedKeys(prefix string) []string {
	keys := make([]string, 0, len(b.entries))
	for key := range b.entries {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// HeadObject implements the S3 call.
func (b *Bucket) HeadObject(
	_ context.Context,
	params *s3.HeadObjectInput,
	_ ...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.latest(aws.ToString(params.Key))
	if e == nil {
		return nil, &types.NotFound{}
	}
	return &s3.HeadObjectOutput{LastModified: aws.Time(e.lastModified), VersionId: aws.String(e.versionID)}, nil
}

// PutObject implements the S3 call. The object is stamped one second after the latest write.
func (b *Bucket) PutObject(
	_ context.Context,
	params *s3.PutObjectInput,
	_ ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	body, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	versionID := b.add(&entry{key: aws.ToString(params.Key), body: body, lastModified: b.clock.Add(time.Second)})
	return &s3.PutObjectOutput{VersionId: aws.String(versionID)}, nil
}

// GetObject implements the S3 call, including VersionId.
func (b *Bucket) GetObject(
	_ context.Context,
	params *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := aws.ToString(params.Key)
	e := b.latest(key)
	if versionID := aws.ToString(params.VersionId); versionID != "" {
		e = nil
		for _, candidate := range b.entries[key] {
			if candidate.versionID == versionID && !candidate.deleteMarker {
				e = candidate
			}
		}
	}
	if e == nil {
		return nil, &types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body:         io.NopCloser(bytes.NewReader(e.body)),
		LastModified: aws.Time(e.lastModified),
		VersionId:    aws.String(e.versionID),
	}, nil
}

// ListObjectsV2 implements the S3 call: current versions only, in key order, with StartAfter and pagination.
func (b *Bucket) ListObjectsV2(
	_ context.Context,
	params *s3.ListObjectsV2Input,
	_ ...func(*s3.Options),
) (*s3.ListObjectsV2Output, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	after := aws.ToString(params.StartAfter)
	if token := aws.ToString(params.ContinuationToken); token != "" {
		after = token
	}
	output := &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}
	for _, key := range b.sortedKeys(aws.ToString(params.Prefix)) {
		e := b.latest(key)
		if key <= after || e == nil {
			continue
		}
		if len(output.Contents) == b.PageSize {
			output.IsTruncated = aws.Bool(true)
			output.NextContinuationToken = output.Contents[len(output.Contents)-1].Key
			break
		}
		output.Contents = append(output.Contents, types.Object{
			Key:          aws.String(key),
			LastModified: aws.Time(e.lastModified),
		})
	}
	return output, nil
}

// ListObjectVersions implements the S3 call: every version and delete marker in key order (newest version of a
// key first), paginated with KeyMarker and VersionIdMarker.
func (b *Bucket) ListObjectVersions(
	_ context.Context,
	params *s3.ListObjectVersionsInput,
	_ ...func(*s3.Options),
) (*s3.ListObjectVersionsOutput, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	all := b.versionsInListingOrder(aws.ToString(params.Prefix))
	start := listingStart(all, aws.ToString(params.KeyMarker), aws.ToString(params.VersionIdMarker))

	output := &s3.ListObjectVersionsOutput{IsTruncated: aws.Bool(false)}
	for i := start; i < len(all); i++ {
		if i-start == b.PageSize {
			last := all[i-1]
			output.IsTruncated = aws.Bool(true)
			output.NextKeyMarker = aws.String(last.key)
			output.NextVersionIdMarker = aws.String(last.versionID)
			break
		}
		versions := b.entries[all[i].key]
		appendVersion(output, all[i], versions[len(versions)-1] == all[i])
	}
	return output, nil
}

func (b *Bucket) versionsInListingOrder(prefix string) []*entry {
	all := make([]*entry, 0, len(b.entries))
	for _, key := range b.sortedKeys(prefix) {
		versions := b.entries[key]
		for i := len(versions) - 1; i >= 0; i-- {
			all = append(all, versions[i])
		}
	}
	return all
}

// listingStart returns the index of the first entry after the markers.
func listingStart(all []*entry, keyMarker, versionIDMarker string) int {
	if keyMarker == "" {
		return 0
	}
	for i, e := range all {
		if versionIDMarker != "" && e.key == keyMarker && e.versionID == versionIDMarker {
			return i + 1
		}
		if versionIDMarker == "" && e.key > keyMarker {
			return i
		}
	}
	return len(all)
}

func appendVersion(output *s3.ListObjectVersionsOutput, e *entry, isLatest bool) {
	if e.deleteMarker {
		output.DeleteMarkers = append(output.DeleteMarkers, types.DeleteMarkerEntry{
			Key:          aws.String(e.key),
			VersionId:    aws.String(e.versionID),
			LastModified: aws.Time(e.lastModified),
			IsLatest:     aws.Bool(isLatest),
		})
		return
	}
	output.Versions = append(output.Versions, types.ObjectVersion{
		Key:          aws.String(e.key),
		VersionId:    aws.String(e.versionID),
		LastModified: aws.Time(e.lastModified),
		IsLatest:     aws.Bool(isLatest),
	})
}
