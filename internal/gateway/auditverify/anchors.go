package auditverify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"

	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
)

// anchorRecord is one version of an anchor object.
type anchorRecord struct {
	key          string
	versionID    string
	lastModified time.Time
	// size is the version's size, as listed.
	size    int64
	raw     []byte
	payload jobaudit.AnchorPayload
	// tooLarge is set for a version bigger than any anchor, which isn't read.
	tooLarge bool
	// invalid is set when raw isn't anchor JSON, or wasn't read (already reported).
	invalid bool
}

// The most a run reads: maxAnchorVersions versions, adding up to maxAnchorBytes. Anchors are written hourly and
// are a few hundred bytes, so that's decades of them. A chain's prefix holding more isn't one the anchor writer
// filled, and reading it all could exhaust memory, so the run fails instead.
const (
	maxAnchorVersions = 1_000_000
	maxAnchorBytes    = 256 << 20
)

// writtenBefore orders anchors by when they were written, then by key: S3 times are whole seconds, and
// orderTiesByLinks then orders anchors written in the same second the way the writer chained them.
func (a *anchorRecord) writtenBefore(other *anchorRecord) bool {
	if !a.lastModified.Equal(other.lastModified) {
		return a.lastModified.Before(other.lastModified)
	}
	if a.key != other.key {
		return a.key < other.key
	}
	return a.versionID < other.versionID
}

// loadAnchors reads every version of every anchor under the chain's prefix, in the order they were written.
// Object Lock keeps every version, so an overwritten anchor shows up as a key with several versions and a
// deleted one as a delete marker. Both are failures, and every version is still checked.
func loadAnchors(ctx context.Context, opts AnchorOptions, report *Report) ([]*anchorRecord, error) {
	records, err := listAnchorVersions(ctx, opts, report)
	if err != nil {
		return nil, err
	}
	reportOverwrittenAnchors(records, report)
	if err := checkAnchorTotal(opts, records); err != nil {
		return nil, err
	}
	if err := fetchAnchors(ctx, opts, records); err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.tooLarge {
			record.invalid = true
			report.fail("anchor %s (version %s) is %s: it wasn't read", record.key, record.versionID,
				jobaudit.ErrAnchorTooLarge)
			continue
		}
		if err := json.Unmarshal(record.raw, &record.payload); err != nil {
			record.invalid = true
			report.fail("anchor %s (version %s) is not valid anchor JSON: %v", record.key, record.versionID, err)
		}
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].writtenBefore(records[j]) })
	orderTiesByLinks(records)
	return records, nil
}

// orderTiesByLinks reorders anchors S3 stored in the same second by their links, as the writer picks the latest
// (S3 times are whole seconds): a delayed write for an earlier hour can follow a later hour's anchor within the
// second, linking to it, so their keys don't say which came first.
func orderTiesByLinks(records []*anchorRecord) {
	for start := 0; start < len(records); {
		end := start + 1
		for end < len(records) && records[end].lastModified.Equal(records[start].lastModified) {
			end++
		}
		if end-start > 1 {
			prev := ""
			if start > 0 {
				prev = anchorHash(records[start-1])
			}
			chainTies(records[start:end], prev)
		}
		start = end
	}
}

// chainTies orders tied anchors (in key order) so each follows the one its prev_anchor_hash names. The first is the
// one that links to prev (the anchor before them), or else the first that links to none of the others. One that
// links nowhere in the group goes after those that do, for the link checks to report.
func chainTies(ties []*anchorRecord, prev string) {
	remaining := append([]*anchorRecord(nil), ties...)
	ordered := make([]*anchorRecord, 0, len(ties))
	for len(remaining) > 0 {
		next := linkedFrom(remaining, prev)
		if next < 0 {
			next = chainStart(remaining)
		}
		ordered = append(ordered, remaining[next])
		prev = anchorHash(remaining[next])
		remaining = append(remaining[:next], remaining[next+1:]...)
	}
	copy(ties, ordered)
}

// linkedFrom returns the index of the first record whose prev_anchor_hash is prev, or -1.
func linkedFrom(records []*anchorRecord, prev string) int {
	for i, record := range records {
		if !record.invalid && strings.EqualFold(record.payload.PrevAnchorHash, prev) {
			return i
		}
	}
	return -1
}

// chainStart returns the index of the first record that links to none of the others.
func chainStart(records []*anchorRecord) int {
	hashes := make(map[string]bool, len(records))
	for _, record := range records {
		hashes[anchorHash(record)] = true
	}
	for i, record := range records {
		if record.invalid || !hashes[strings.ToLower(record.payload.PrevAnchorHash)] {
			return i
		}
	}
	return 0
}

// anchorHash is the SHA-256 (hex) of an anchor's body, as the next anchor's prev_anchor_hash names it.
func anchorHash(record *anchorRecord) string {
	sum := sha256.Sum256(record.raw)
	return hex.EncodeToString(sum[:])
}

func listAnchorVersions(ctx context.Context, opts AnchorOptions, report *Report) ([]*anchorRecord, error) {
	prefix := opts.ChainID + "/"
	input := &s3.ListObjectVersionsInput{Bucket: aws.String(opts.Bucket), Prefix: aws.String(prefix)}
	var records []*anchorRecord
	ignored := 0
	for {
		page, err := opts.Store.ListObjectVersions(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list anchor versions in s3://%s/%s: %w", opts.Bucket, prefix, err)
		}
		records, ignored = collectAnchorVersions(page, records, ignored, report)
		if len(records) > maxAnchorVersions {
			return nil, fmt.Errorf("s3://%s/%s holds more than %d anchor versions, more than a run reads",
				opts.Bucket, prefix, maxAnchorVersions)
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		if aws.ToString(page.NextKeyMarker) == "" {
			return nil, errors.New("anchor version listing was truncated without a next key marker")
		}
		input.KeyMarker = page.NextKeyMarker
		input.VersionIdMarker = page.NextVersionIdMarker
	}
	if ignored > 0 {
		report.warn("ignored %d objects under s3://%s/%s that aren't anchors (no .json suffix)",
			ignored, opts.Bucket, prefix)
	}
	return records, nil
}

// collectAnchorVersions adds a listing page's anchor versions to records, counts other objects in ignored and
// reports delete markers.
func collectAnchorVersions(
	page *s3.ListObjectVersionsOutput,
	records []*anchorRecord,
	ignored int,
	report *Report,
) ([]*anchorRecord, int) {
	for _, marker := range page.DeleteMarkers {
		report.fail("anchor %s was deleted (delete marker %s); its locked versions are still checked",
			aws.ToString(marker.Key), aws.ToString(marker.VersionId))
	}
	for _, version := range page.Versions {
		key := aws.ToString(version.Key)
		if !strings.HasSuffix(key, ".json") {
			ignored++
			continue
		}
		records = append(records, &anchorRecord{
			key:          key,
			versionID:    aws.ToString(version.VersionId),
			lastModified: aws.ToTime(version.LastModified),
			size:         aws.ToInt64(version.Size),
		})
	}
	return records, ignored
}

func reportOverwrittenAnchors(records []*anchorRecord, report *Report) {
	versions := map[string]int{}
	for _, record := range records {
		versions[record.key]++
	}
	keys := make([]string, 0, len(versions))
	for key, count := range versions {
		if count > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		report.fail("anchor %s has %d versions: it was overwritten after it was first written", key, versions[key])
	}
}

// checkAnchorTotal fails a run whose anchor versions add up to more than maxAnchorBytes. Versions bigger than any
// anchor aren't read, so they don't count.
func checkAnchorTotal(opts AnchorOptions, records []*anchorRecord) error {
	var total int64
	for _, record := range records {
		if record.size <= jobaudit.MaxAnchorSize {
			total += record.size
		}
	}
	if total > maxAnchorBytes {
		return fmt.Errorf("the anchor versions in s3://%s/%s/ add up to %d bytes, more than a run reads (%d)",
			opts.Bucket, opts.ChainID, total, maxAnchorBytes)
	}
	return nil
}

// anchorReadConcurrency is how many anchor versions are read at once. A full check reads every anchor ever
// written, one GET of about 100ms each and about 8,800 a year, so one at a time it would take most of an hour
// after a few years.
const anchorReadConcurrency = 16

// fetchAnchors reads every record's anchor version, anchorReadConcurrency at a time, and stops at the first that
// can't be read.
func fetchAnchors(ctx context.Context, opts AnchorOptions, records []*anchorRecord) error {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(anchorReadConcurrency)
	for _, record := range records {
		group.Go(func() error { return fetchAnchor(ctx, opts, record) })
	}
	return group.Wait()
}

// fetchAnchor reads a record's anchor version, unless it's bigger than any anchor (as listed, or as read): then it
// sets tooLarge instead.
func fetchAnchor(ctx context.Context, opts AnchorOptions, record *anchorRecord) error {
	if record.size > jobaudit.MaxAnchorSize {
		record.tooLarge = true
		return nil
	}
	input := &s3.GetObjectInput{Bucket: aws.String(opts.Bucket), Key: aws.String(record.key)}
	if record.versionID != "" {
		input.VersionId = aws.String(record.versionID)
	}
	output, err := opts.Store.GetObject(ctx, input)
	if err == nil {
		defer func() { _ = output.Body.Close() }()
		record.raw, err = jobaudit.ReadAnchorBody(output.Body)
	}
	switch {
	case errors.Is(err, jobaudit.ErrAnchorTooLarge):
		record.tooLarge = true
	case err != nil:
		return fmt.Errorf("failed to read anchor %s (version %s): %w", record.key, record.versionID, err)
	}
	return nil
}
