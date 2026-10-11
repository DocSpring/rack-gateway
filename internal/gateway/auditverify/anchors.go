package auditverify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
)

// anchorRecord is one version of an anchor object.
type anchorRecord struct {
	key          string
	versionID    string
	lastModified time.Time
	raw          []byte
	payload      jobaudit.AnchorPayload
	// invalid is set when raw isn't anchor JSON (already reported).
	invalid bool
}

// writtenBefore orders anchors the way the writer chains them: by when they were written (S3 times are whole
// seconds), then by key.
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
	for _, record := range records {
		if err := fetchAnchor(ctx, opts, record); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(record.raw, &record.payload); err != nil {
			record.invalid = true
			report.fail("anchor %s (version %s) is not valid anchor JSON: %v", record.key, record.versionID, err)
		}
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].writtenBefore(records[j]) })
	return records, nil
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

func fetchAnchor(ctx context.Context, opts AnchorOptions, record *anchorRecord) error {
	input := &s3.GetObjectInput{Bucket: aws.String(opts.Bucket), Key: aws.String(record.key)}
	if record.versionID != "" {
		input.VersionId = aws.String(record.versionID)
	}
	output, err := opts.Store.GetObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to read anchor %s (version %s): %w", record.key, record.versionID, err)
	}
	defer func() { _ = output.Body.Close() }()
	if record.raw, err = io.ReadAll(output.Body); err != nil {
		return fmt.Errorf("failed to read anchor %s (version %s): %w", record.key, record.versionID, err)
	}
	return nil
}
