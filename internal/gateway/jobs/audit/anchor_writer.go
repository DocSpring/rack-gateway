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

// AnchorWriterWorker writes audit log chain anchors to the S3 Object Lock bucket. The bucket's default COMPLIANCE
// retention locks each anchor (the gateway checks it at startup and in every verification); the writer sets none.
type AnchorWriterWorker struct {
	river.WorkerDefaults[AnchorWriterArgs]
	db       *db.Database
	s3Client S3Client
	bucket   string
	chainID  string
}

// NewAnchorWriterWorker creates a new anchor writer worker
func NewAnchorWriterWorker(database *db.Database, s3Client S3Client, bucket, chainID string) *AnchorWriterWorker {
	return &AnchorWriterWorker{db: database, s3Client: s3Client, bucket: bucket, chainID: chainID}
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

// anchorWrittenAt reports whether the anchor for key has been written, and when S3 stored it. Any error other
// than "not found" fails the job: writing again would add a second version of the anchor, which verification
// reports as a replacement.
func (w *AnchorWriterWorker) anchorWrittenAt(ctx context.Context, key string) (time.Time, bool, error) {
	head, err := w.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return aws.ToTime(head.LastModified), true, nil
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return time.Time{}, false, nil
	}
	return time.Time{}, false, fmt.Errorf("failed to check for anchor %s: %w", key, err)
}

// anchorBusySnooze is how long the job waits before trying again when another anchor write holds the lock.
const anchorBusySnooze = time.Minute

// Work writes the anchor for the hour the job was created in. When another anchor write is running, the job is
// snoozed and tries again later. An attempt outside the hour's write window gives up, and so does a job that finds
// its hour's key holding an object the anchor writer didn't write; both are canceled, since retrying can't help.
func (w *AnchorWriterWorker) Work(ctx context.Context, job *river.Job[AnchorWriterArgs]) error {
	if err := checkAttemptTime(AnchorKey(w.chainID, job.CreatedAt), job.JobRow); err != nil {
		return cancelAnchorJob(err)
	}
	// The job's creation time names the anchor, so retries of a job write the same key.
	err := w.WriteAnchor(ctx, job.CreatedAt)
	switch {
	case errors.Is(err, db.ErrAuditAnchorLockBusy):
		return river.JobSnooze(anchorBusySnooze)
	case errors.Is(err, ErrUnexpectedAnchor):
		return cancelAnchorJob(err)
	}
	return err
}

// WriteAnchor writes the anchor for the hour of ts unless it's already written. It holds the audit anchor lock
// from the existence check through the write, so concurrent writes can't add a second version of a key or link
// two anchors to the same predecessor, which verification would report as failures that Object Lock keeps
// forever. It returns db.ErrAuditAnchorLockBusy when another anchor write holds the lock, and
// ErrUnexpectedAnchor when the hour's key holds an object the anchor writer didn't write.
//
// The lock is a transaction-scoped Postgres advisory lock, so the write holds one pooled database connection
// across its S3 calls: releasing it before the PutObject would let a second writer find the key missing and write
// it too. In the gateway the job writes at most one anchor an hour and River's job timeout (a minute) bounds it,
// so it holds the connection for well under a second normally and never longer than that. `write-anchor` runs in
// its own process, with its own pool.
func (w *AnchorWriterWorker) WriteAnchor(ctx context.Context, ts time.Time) error {
	timestamp := ts.UTC()
	return w.db.WithAuditAnchorLock(ctx, func(ctx context.Context, lock *db.AuditAnchorTx) error {
		return w.writeAnchorLocked(ctx, lock, timestamp)
	})
}

// WriteCurrentAnchor writes the anchor for the current hour by the database's clock, for `write-anchor`. The
// clock of the host running it could be wrong, and an anchor named by it would fail verification for as long as
// Object Lock keeps it. Otherwise it behaves like WriteAnchor.
func (w *AnchorWriterWorker) WriteCurrentAnchor(ctx context.Context) error {
	return w.db.WithAuditAnchorLock(ctx, func(ctx context.Context, lock *db.AuditAnchorTx) error {
		now, err := lock.Now(ctx)
		if err != nil {
			return err
		}
		return w.writeAnchorLocked(ctx, lock, now.UTC())
	})
}

func (w *AnchorWriterWorker) writeAnchorLocked(ctx context.Context, lock *db.AuditAnchorTx, timestamp time.Time) error {
	key := AnchorKey(w.chainID, timestamp) // Hour precision only - no minutes/seconds

	written, exists, err := w.anchorWrittenAt(ctx, key)
	if err != nil {
		return err
	}
	if exists {
		// An earlier run wrote this hour's anchor, unless someone else put an object there first.
		return w.checkExistingAnchor(ctx, lock, key, timestamp, written)
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

	// Write JSON to S3 with its checksum. The SHA256 checksum is stored in S3 metadata, so no separate .sha256 file
	// is needed.
	if err := w.putAnchorObject(ctx, key, canonicalJSON, &hashBase64); err != nil {
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

// putAnchorObject writes an anchor to S3. On AWS it uses KMS encryption. Both AWS and MinIO lock it with the
// bucket's default Object Lock retention.
func (w *AnchorWriterWorker) putAnchorObject(
	ctx context.Context,
	key string,
	data []byte,
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

	_, err := w.s3Client.PutObject(ctx, putInput, putAnchorOnce)
	if err != nil {
		// Log detailed error information for debugging 400 errors
		return fmt.Errorf(
			"S3 PutObject failed (bucket=%s, key=%s, size=%d, useAWS=%t, hasChecksum=%t): %w",
			w.bucket, key, len(data), useAWS, checksumSHA256 != nil, err,
		)
	}
	return nil
}

// putAnchorOnce stops the SDK from retrying the anchor's PutObject. After a 5xx or a dropped connection the SDK
// resends the object, but S3 may have stored the first attempt and only the response was lost: the resend then
// adds a second version of the key, which verification reports as an overwritten anchor and Object Lock keeps for
// the whole retention period. A failed attempt fails the job instead, and River's retry checks whether the anchor
// exists (under the anchor lock) before writing it.
func putAnchorOnce(o *s3.Options) { o.RetryMaxAttempts = 1 }
