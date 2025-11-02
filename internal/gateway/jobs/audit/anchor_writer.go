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
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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

// AnchorPayload represents the anchor JSON structure
type AnchorPayload struct {
	ChainID        string `json:"chain_id"`
	LastSeq        int64  `json:"last_seq"`
	LastHash       string `json:"last_hash"`
	TimestampUTC   string `json:"ts_utc"`
	DBSnapshotLSN  string `json:"db_snapshot_lsn"`
	PrevAnchorHash string `json:"prev_anchor_hash,omitempty"`
}

// checkS3FileExists returns true if the file exists in S3
func (w *AnchorWriterWorker) checkS3FileExists(ctx context.Context, key string) bool {
	_, err := w.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(key),
	})
	return err == nil
}

// Work writes an audit anchor to S3
func (w *AnchorWriterWorker) Work(ctx context.Context, job *river.Job[AnchorWriterArgs]) error {
	// Use job creation time as anchor timestamp - this ensures deterministic filenames on retries
	// All retries of the same job will use the same timestamp and try to write the same file
	timestamp := job.CreatedAt.UTC()

	// Generate S3 key using hour-based timestamp (deterministic for retries within same hour)
	key := fmt.Sprintf("%s/%s/anchor-%s.json",
		w.chainID,
		timestamp.Format("2006/01/02/15"),
		timestamp.Format("20060102T15")) // Hour precision only - no minutes/seconds

	// Check if anchor file already exists - skip if already written
	if w.checkS3FileExists(ctx, key) {
		// File exists - job already completed successfully
		return nil
	}

	// Get latest event from database
	latest, err := w.getLatestEvent(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to get latest event: %w", err)
	}

	// Get previous anchor hash if exists
	prevAnchorHash := ""
	prevAnchor, err := w.getLatestAnchor(ctx)
	if err == nil && prevAnchor != "" {
		prevAnchorHash = prevAnchor
	}

	// Create anchor payload
	lastSeq := latest.ChainIndex
	if lastSeq < 0 {
		lastSeq = 0 // Empty chain starts at 0
	}
	lastHash := ""
	if len(latest.EventHash) > 0 {
		lastHash = hex.EncodeToString(latest.EventHash)
	}
	anchor := AnchorPayload{
		ChainID:        w.chainID,
		LastSeq:        lastSeq,
		LastHash:       lastHash,
		TimestampUTC:   timestamp.Format(time.RFC3339),
		DBSnapshotLSN:  "", // TODO: Get current LSN from PostgreSQL if needed
		PrevAnchorHash: prevAnchorHash,
	}

	// Marshal to canonical JSON (sorted keys)
	canonicalJSON, err := json.Marshal(anchor)
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
	useAWS := os.Getenv("AWS_ENDPOINT_URL_S3") == ""
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

// latestEvent represents the latest event in the chain
type latestEvent struct {
	ChainIndex     int64
	EventHash      []byte
	CheckpointID   string
	CheckpointHash []byte
}

func (w *AnchorWriterWorker) getLatestEvent(ctx context.Context, _ interface{}) (latestEvent, error) {
	var latest latestEvent
	query := `SELECT chain_index, event_hash, COALESCE(checkpoint_id, '')::varchar, COALESCE(checkpoint_hash, ''::bytea)
		FROM audit.audit_event
		ORDER BY chain_index DESC
		LIMIT 1`

	err := w.db.Pool().QueryRow(ctx, query).Scan(
		&latest.ChainIndex,
		&latest.EventHash,
		&latest.CheckpointID,
		&latest.CheckpointHash,
	)
	if err != nil {
		// No rows is OK - means chain is empty
		if errors.Is(err, pgx.ErrNoRows) {
			latest.ChainIndex = -1
			return latest, nil
		}
		return latest, fmt.Errorf("failed to scan latest event: %w", err)
	}

	return latest, nil
}

func (w *AnchorWriterWorker) getLatestAnchor(ctx context.Context) (string, error) {
	// List objects in bucket with chain ID prefix
	listInput := &s3.ListObjectsV2Input{
		Bucket: aws.String(w.bucket),
		Prefix: aws.String(w.chainID + "/"),
	}

	listOutput, err := w.s3Client.ListObjectsV2(ctx, listInput)
	if err != nil {
		return "", fmt.Errorf("failed to list anchors: %w", err)
	}

	if len(listOutput.Contents) == 0 {
		return "", nil // No previous anchors
	}

	// Find the latest JSON anchor file (not .sha256)
	var latestKey string
	var latestTime time.Time
	for _, obj := range listOutput.Contents {
		if obj.Key == nil || *obj.Key == "" || !strings.HasSuffix(*obj.Key, ".json") {
			continue
		}
		if obj.LastModified != nil && obj.LastModified.After(latestTime) {
			latestTime = *obj.LastModified
			latestKey = *obj.Key
		}
	}

	if latestKey == "" {
		return "", nil // No previous anchors found
	}

	// Download and hash the previous anchor
	getInput := &s3.GetObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(latestKey),
	}

	getOutput, err := w.s3Client.GetObject(ctx, getInput)
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
