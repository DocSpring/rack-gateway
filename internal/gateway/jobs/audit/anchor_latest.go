package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// recentAnchorWindow is how far back the anchor writer first looks for the previous anchor. Anchors are
// written about hourly, so it is almost always there; otherwise the whole chain prefix is listed.
const recentAnchorWindow = 35 * 24 * time.Hour

// MaxAnchorSize is the most an anchor's body can hold. An anchor is a few hundred bytes of JSON, so a bigger object
// under the chain's prefix isn't one the writer wrote, and it's refused without reading it all.
const MaxAnchorSize = 16 << 10

// ErrAnchorTooLarge means an object stored as an anchor is bigger than MaxAnchorSize.
var ErrAnchorTooLarge = errors.New("larger than an anchor can be (16 KiB)")

// ReadAnchorBody reads an anchor's body, and refuses one bigger than MaxAnchorSize (ErrAnchorTooLarge) after
// reading no more than that.
func ReadAnchorBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, MaxAnchorSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxAnchorSize {
		return nil, ErrAnchorTooLarge
	}
	return data, nil
}

// isAnchorKey reports whether key is one AnchorKey writes for the chain. Nothing else under the prefix is an
// anchor, so the writer never chains to it.
func isAnchorKey(chainID, key string) bool {
	ts, err := time.Parse("anchor-20060102T15.json", path.Base(key))
	return err == nil && AnchorKey(chainID, ts) == key
}

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

	body, err := w.readAnchor(ctx, latest.key)
	if err != nil {
		return "", fmt.Errorf("failed to get previous anchor: %w", err)
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

// readAnchor returns the body of the anchor at key.
func (w *AnchorWriterWorker) readAnchor(ctx context.Context, key string) ([]byte, error) {
	output, err := w.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get anchor %s: %w", key, err)
	}
	defer func() { _ = output.Body.Close() }()
	body, err := ReadAnchorBody(output.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read anchor %s: %w", key, err)
	}
	return body, nil
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
			if !isAnchorKey(w.chainID, key) {
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
