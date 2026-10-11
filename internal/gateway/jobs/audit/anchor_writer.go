package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
)

// AnchorWriterArgs contains parameters for anchor writer job
// Empty for periodic jobs - all config comes from environment
type AnchorWriterArgs struct{}

// Kind returns the unique identifier for this job type
func (AnchorWriterArgs) Kind() string { return "audit:anchor_writer" }

// AnchorWriterWorker writes audit log chain anchors to S3 WORM bucket
type AnchorWriterWorker struct {
	river.WorkerDefaults[AnchorWriterArgs]
	db            *db.Database
	s3Client      S3Client
	bucket        string
	chainID       string
	retentionDays int
}

// NewAnchorWriterWorker creates a new anchor writer worker
func NewAnchorWriterWorker(
	database *db.Database,
	s3Client S3Client,
	bucket, chainID string,
	retentionDays int,
) *AnchorWriterWorker {
	return &AnchorWriterWorker{
		db:            database,
		s3Client:      s3Client,
		bucket:        bucket,
		chainID:       chainID,
		retentionDays: retentionDays,
	}
}

// AnchorFormat is the format of the anchors this version writes. A format 2 anchor's prev_anchor_hash always
// names the anchor written just before it (earlier versions only looked at the first 1000 anchors in the
// bucket), and it records where hash v2 starts. Anchors without a format were written by earlier versions.
const AnchorFormat = 2

// AnchorPayload represents the anchor JSON structure
type AnchorPayload struct {
	ChainID        string `json:"chain_id"`
	LastSeq        int64  `json:"last_seq"`
	LastHash       string `json:"last_hash"`
	TimestampUTC   string `json:"ts_utc"`
	DBSnapshotLSN  string `json:"db_snapshot_lsn"`
	PrevAnchorHash string `json:"prev_anchor_hash,omitempty"`
	// Format is AnchorFormat, or 0 (absent) for anchors written by earlier versions.
	Format int `json:"format,omitempty"`
	// HashV2FromSeq is the chain index of the first hash v2 event, once there is one.
	HashV2FromSeq *int64 `json:"hash_v2_from_seq,omitempty"`
}

// AnchorKey returns the S3 key of the anchor for the hour of ts. Retries of a job write the same key.
func AnchorKey(chainID string, ts time.Time) string {
	ts = ts.UTC()
	return fmt.Sprintf("%s/%s/anchor-%s.json", chainID, ts.Format("2006/01/02/15"), ts.Format("20060102T15"))
}

// anchorExists reports whether the anchor for key has been written. Any error other than "not found" fails the
// job: writing again would add a second version of the anchor, which verification reports as a replacement.
func (w *AnchorWriterWorker) anchorExists(ctx context.Context, key string) (bool, error) {
	_, err := w.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check for anchor %s: %w", key, err)
}

// anchorBusySnooze is how long the job waits before trying again when another anchor write holds the lock.
const anchorBusySnooze = time.Minute

// Work writes the anchor for the hour the job was created in. When another anchor write is running, the job is
// snoozed and tries again later.
func (w *AnchorWriterWorker) Work(ctx context.Context, job *river.Job[AnchorWriterArgs]) error {
	// The job's creation time names the anchor, so retries of a job write the same key.
	err := w.WriteAnchor(ctx, job.CreatedAt)
	if errors.Is(err, db.ErrAuditAnchorLockBusy) {
		return river.JobSnooze(anchorBusySnooze)
	}
	return err
}

// WriteAnchor writes the anchor for the hour of ts unless it's already written. It holds the audit anchor lock
// from the existence check through the write, so concurrent writes can't add a second version of a key or link
// two anchors to the same predecessor, which verification would report as failures that Object Lock keeps
// forever. It returns db.ErrAuditAnchorLockBusy when another anchor write holds the lock.
func (w *AnchorWriterWorker) WriteAnchor(ctx context.Context, ts time.Time) error {
	timestamp := ts.UTC()
	return w.db.WithAuditAnchorLock(ctx, func(ctx context.Context, lock *db.AuditAnchorTx) error {
		return w.writeAnchorLocked(ctx, lock, timestamp)
	})
}

func (w *AnchorWriterWorker) writeAnchorLocked(ctx context.Context, lock *db.AuditAnchorTx, timestamp time.Time) error {
	key := AnchorKey(w.chainID, timestamp) // Hour precision only - no minutes/seconds

	exists, err := w.anchorExists(ctx, key)
	if err != nil {
		return err
	}
	if exists {
		// File exists - job already completed successfully
		return nil
	}

	head, err := lock.AuditChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest event: %w", err)
	}

	// Without the previous anchor's hash this anchor would break the anchor chain, so fail and retry.
	prevAnchorHash, err := w.getLatestAnchor(ctx, timestamp)
	if err != nil {
		return err
	}

	// Marshal to canonical JSON (sorted keys)
	canonicalJSON, err := json.Marshal(newAnchorPayload(w.chainID, timestamp, head, prevAnchorHash))
	if err != nil {
		return fmt.Errorf("failed to marshal anchor: %w", err)
	}

	// Compute SHA-256 hash of the JSON
	hash := sha256.Sum256(canonicalJSON)
	hashBase64 := base64.StdEncoding.EncodeToString(hash[:])

	// Calculate retention date
	retainUntil := timestamp.Add(time.Duration(w.retentionDays) * 24 * time.Hour)

	// Write JSON to S3 with Object Lock and checksum
	// The SHA256 checksum is stored in S3 metadata, so no separate .sha256 file is needed
	if err := w.putObjectWithRetention(ctx, key, canonicalJSON, retainUntil, &hashBase64); err != nil {
		return fmt.Errorf("failed to write anchor JSON: %w", err)
	}

	return nil
}

// newAnchorPayload describes the chain head. An empty chain is anchored with last_seq 0 and no hash.
func newAnchorPayload(chainID string, ts time.Time, head db.AuditChainHead, prevAnchorHash string) AnchorPayload {
	anchor := AnchorPayload{
		ChainID:        chainID,
		TimestampUTC:   ts.Format(time.RFC3339),
		PrevAnchorHash: prevAnchorHash,
		Format:         AnchorFormat,
	}
	if !head.Empty {
		anchor.LastSeq = head.LastIndex
		anchor.LastHash = hex.EncodeToString(head.LastHash)
	}
	if head.HashV2From.Valid {
		hashV2From := head.HashV2From.Int64
		anchor.HashV2FromSeq = &hashV2From
	}
	return anchor
}

// putObjectWithRetention writes an object to S3 with appropriate retention and encryption settings.
// For AWS S3: uses KMS encryption. Object Lock retention is applied via bucket default retention.
// For MinIO: relies on bucket-level default retention.
func (w *AnchorWriterWorker) putObjectWithRetention(
	ctx context.Context,
	key string,
	data []byte,
	retainUntil time.Time,
	checksumSHA256 *string,
) error {
	putInput := &s3.PutObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	}

	// AWS S3 specific features (not supported by MinIO)
	// Note: We do NOT set ObjectLockMode or ObjectLockRetainUntilDate here because the bucket
	// has default Object Lock retention configured via Terraform. AWS S3 returns a 400 error
	// if you try to set per-object retention on a bucket with default retention.
	useAWS := !testbypass.S3EndpointOverride()
	if useAWS {
		putInput.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		if checksumSHA256 != nil {
			putInput.ChecksumSHA256 = checksumSHA256
		}
	}

	_, err := w.s3Client.PutObject(ctx, putInput)
	if err != nil {
		// Log detailed error information for debugging 400 errors
		return fmt.Errorf(
			"S3 PutObject failed (bucket=%s, key=%s, size=%d, useAWS=%t, hasChecksum=%t, retainUntil=%s): %w",
			w.bucket, key, len(data), useAWS, checksumSHA256 != nil, retainUntil.Format(time.RFC3339), err,
		)
	}
	return nil
}
