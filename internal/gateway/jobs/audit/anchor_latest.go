package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// recentAnchorWindow is how far back the anchor writer first looks for the previous anchor. Anchors are
// written about hourly, so it is almost always there; otherwise the whole chain prefix is listed.
const recentAnchorWindow = 35 * 24 * time.Hour

// anchorObject is a listed anchor.
type anchorObject struct {
	key          string
	lastModified time.Time
}

// newerThan orders anchors by when they were written, then by key (S3 times are in whole seconds).
func (a anchorObject) newerThan(other anchorObject) bool {
	if !a.lastModified.Equal(other.lastModified) {
		return a.lastModified.After(other.lastModified)
	}
	return a.key > other.key
}

// getLatestAnchor returns the SHA-256 (hex) of the most recently written anchor, or "" if there is none. It
// lists every page, so the answer stays right however many anchors the bucket holds.
func (w *AnchorWriterWorker) getLatestAnchor(ctx context.Context, now time.Time) (string, error) {
	recent := w.chainID + "/" + now.UTC().Add(-recentAnchorWindow).Format("2006/01/02")
	latest, err := w.findLatestAnchor(ctx, recent)
	if err != nil {
		return "", err
	}
	if latest.key == "" {
		if latest, err = w.findLatestAnchor(ctx, ""); err != nil {
			return "", err
		}
	}
	if latest.key == "" {
		return "", nil // No previous anchors
	}

	getOutput, err := w.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(latest.key),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get previous anchor: %w", err)
	}
	defer func() { _ = getOutput.Body.Close() }()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(getOutput.Body); err != nil {
		return "", fmt.Errorf("failed to read previous anchor: %w", err)
	}

	hash := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(hash[:]), nil
}

// findLatestAnchor lists the chain's anchors with keys after startAfter ("" for all of them), page by page,
// and returns the most recently written one (a zero anchorObject if there are none).
func (w *AnchorWriterWorker) findLatestAnchor(ctx context.Context, startAfter string) (anchorObject, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(w.bucket),
		Prefix: aws.String(w.chainID + "/"),
	}
	if startAfter != "" {
		input.StartAfter = aws.String(startAfter)
	}

	var latest anchorObject
	for {
		page, err := w.s3Client.ListObjectsV2(ctx, input)
		if err != nil {
			return anchorObject{}, fmt.Errorf("failed to list anchors: %w", err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			candidate := anchorObject{key: key, lastModified: aws.ToTime(obj.LastModified)}
			if candidate.newerThan(latest) {
				latest = candidate
			}
		}
		if !aws.ToBool(page.IsTruncated) || aws.ToString(page.NextContinuationToken) == "" {
			return latest, nil
		}
		input.ContinuationToken = page.NextContinuationToken
	}
}
