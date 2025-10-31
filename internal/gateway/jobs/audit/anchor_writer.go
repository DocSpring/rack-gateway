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
	s3Client      *s3.Client
	bucket        string
	chainID       string
	retentionDays int
}

// NewAnchorWriterWorker creates a new anchor writer worker
func NewAnchorWriterWorker(database *db.Database, s3Client *s3.Client, bucket, chainID string, retentionDays int) *AnchorWriterWorker {
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

// Work writes an audit anchor to S3
func (w *AnchorWriterWorker) Work(ctx context.Context, job *river.Job[AnchorWriterArgs]) error {
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
	timestamp := time.Now().UTC()
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

	// Compute SHA-256 hash
	hash := sha256.Sum256(canonicalJSON)
	hashHex := hex.EncodeToString(hash[:])
	hashBase64 := base64.StdEncoding.EncodeToString(hash[:])

	// Generate S3 key with timestamp
	key := fmt.Sprintf("%s/%s/anchor-%s.json",
		w.chainID,
		timestamp.Format("2006/01/02/15"),
		timestamp.Format("20060102T150405Z"))

	// Calculate retention date
	retainUntil := timestamp.Add(time.Duration(w.retentionDays) * 24 * time.Hour)

	// Write JSON to S3 with Object Lock
	putInput := &s3.PutObjectInput{
		Bucket:                    aws.String(w.bucket),
		Key:                       aws.String(key),
		Body:                      bytes.NewReader(canonicalJSON),
		ServerSideEncryption:      types.ServerSideEncryptionAwsKms,
		ObjectLockMode:            types.ObjectLockModeCompliance,
		ObjectLockRetainUntilDate: aws.Time(retainUntil),
		ChecksumSHA256:            aws.String(hashBase64),
		IfNoneMatch:               aws.String("*"), // Prevent accidental overwrite
	}
	_, err = w.s3Client.PutObject(ctx, putInput)
	if err != nil {
		return fmt.Errorf("failed to write anchor JSON: %w", err)
	}

	// Write .sha256 file
	sha256Key := key + ".sha256"
	sha256PutInput := &s3.PutObjectInput{
		Bucket:                    aws.String(w.bucket),
		Key:                       aws.String(sha256Key),
		Body:                      bytes.NewReader([]byte(hashHex)),
		ServerSideEncryption:      types.ServerSideEncryptionAwsKms,
		ObjectLockMode:            types.ObjectLockModeCompliance,
		ObjectLockRetainUntilDate: aws.Time(retainUntil),
		IfNoneMatch:               aws.String("*"),
	}
	_, err = w.s3Client.PutObject(ctx, sha256PutInput)
	if err != nil {
		return fmt.Errorf("failed to write anchor SHA256: %w", err)
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
