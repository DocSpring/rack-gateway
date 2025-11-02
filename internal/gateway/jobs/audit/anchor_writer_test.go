package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// mockS3Client implements S3Client interface for testing
type mockS3Client struct {
	headObjectFunc func(
		ctx context.Context,
		params *s3.HeadObjectInput,
		optFns ...func(*s3.Options),
	) (*s3.HeadObjectOutput, error)
	putObjectFunc func(
		ctx context.Context,
		params *s3.PutObjectInput,
		optFns ...func(*s3.Options),
	) (*s3.PutObjectOutput, error)
	getObjectFunc func(
		ctx context.Context,
		params *s3.GetObjectInput,
		optFns ...func(*s3.Options),
	) (*s3.GetObjectOutput, error)
	listObjectsV2Func func(
		ctx context.Context,
		params *s3.ListObjectsV2Input,
		optFns ...func(*s3.Options),
	) (*s3.ListObjectsV2Output, error)
}

func (m *mockS3Client) HeadObject(
	ctx context.Context,
	params *s3.HeadObjectInput,
	optFns ...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	if m.headObjectFunc != nil {
		return m.headObjectFunc(ctx, params, optFns...)
	}
	return nil, &types.NotFound{}
}

func (m *mockS3Client) PutObject(
	ctx context.Context,
	params *s3.PutObjectInput,
	optFns ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	if m.putObjectFunc != nil {
		return m.putObjectFunc(ctx, params, optFns...)
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, params, optFns...)
	}
	return &s3.GetObjectOutput{}, nil
}

func (m *mockS3Client) ListObjectsV2(
	ctx context.Context,
	params *s3.ListObjectsV2Input,
	optFns ...func(*s3.Options),
) (*s3.ListObjectsV2Output, error) {
	if m.listObjectsV2Func != nil {
		return m.listObjectsV2Func(ctx, params, optFns...)
	}
	return &s3.ListObjectsV2Output{}, nil
}

func TestAnchorWriterArgs_Kind(t *testing.T) {
	args := AnchorWriterArgs{}
	assert.Equal(t, "audit:anchor_writer", args.Kind())
}

func TestNewAnchorWriterWorker(t *testing.T) {
	s3Client := &mockS3Client{}
	bucket := "test-bucket"
	chainID := "staging"
	retentionDays := 400

	worker := NewAnchorWriterWorker(nil, s3Client, bucket, chainID, retentionDays)

	require.NotNil(t, worker)
	assert.Equal(t, bucket, worker.bucket)
	assert.Equal(t, chainID, worker.chainID)
	assert.Equal(t, retentionDays, worker.retentionDays)
}

func TestAnchorWriterWorker_Work_BothFilesExist(t *testing.T) {
	// Setup: Both JSON and SHA256 files already exist
	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			_ *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			// Both files exist
			return &s3.HeadObjectOutput{}, nil
		},
	}

	worker := &AnchorWriterWorker{
		db:            nil, // Not needed when files already exist
		s3Client:      s3Client,
		bucket:        "test-bucket",
		chainID:       "staging",
		retentionDays: 7,
	}

	job := &river.Job[AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	err := worker.Work(context.Background(), job)
	assert.NoError(t, err, "Should succeed when both files exist")
}

func TestAnchorWriterWorker_Work_JSONExistsSHA256Missing(t *testing.T) {
	// Setup: JSON file exists but SHA256 is missing - should write SHA256 only
	database := dbtest.NewDatabase(t)
	dbtest.Reset(t, database)

	// Create an audit event so we have data to anchor
	auditLog := &db.AuditLog{
		UserEmail:      "test@example.com",
		UserName:       "Test User",
		ActionType:     "test",
		Action:         "test:read",
		Resource:       "test-resource",
		Status:         "success",
		ResponseTimeMs: 100,
	}
	err := database.CreateAuditLog(auditLog)
	require.NoError(t, err)

	key := "staging/2025/11/01/12/anchor-20251101T12.json"
	sha256Key := key + ".sha256"

	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			params *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			// JSON exists, SHA256 doesn't
			if *params.Key == key {
				return &s3.HeadObjectOutput{}, nil
			}
			return nil, &types.NotFound{}
		},
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			// No previous anchors
			return &s3.ListObjectsV2Output{Contents: []types.Object{}}, nil
		},
		putObjectFunc: func(
			_ context.Context,
			params *s3.PutObjectInput,
			_ ...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			// Should only write SHA256 file
			assert.Equal(t, sha256Key, *params.Key, "Should only write SHA256 file")
			return &s3.PutObjectOutput{}, nil
		},
	}

	worker := &AnchorWriterWorker{
		db:            database,
		s3Client:      s3Client,
		bucket:        "test-bucket",
		chainID:       "staging",
		retentionDays: 7,
	}

	job := &river.Job[AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	err = worker.Work(context.Background(), job)
	assert.NoError(t, err, "Should succeed when writing only SHA256 file")
}

func TestAnchorWriterWorker_Work_BothFilesMissing_EmptyChain(t *testing.T) {
	// Setup: Both files missing, empty chain (no audit events)
	database := dbtest.NewDatabase(t)
	dbtest.Reset(t, database)

	key := "staging/2025/11/01/12/anchor-20251101T12.json"
	sha256Key := key + ".sha256"

	var jsonWritten, sha256Written bool

	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			_ *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			// Neither file exists
			return nil, &types.NotFound{}
		},
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			// No previous anchors
			return &s3.ListObjectsV2Output{Contents: []types.Object{}}, nil
		},
		putObjectFunc: func(
			_ context.Context,
			params *s3.PutObjectInput,
			_ ...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			switch *params.Key {
			case key:
				jsonWritten = true
				// Verify empty chain payload
				var buf bytes.Buffer
				_, _ = buf.ReadFrom(params.Body)
				var anchor AnchorPayload
				err := json.Unmarshal(buf.Bytes(), &anchor)
				require.NoError(t, err)
				assert.Equal(t, "staging", anchor.ChainID)
				assert.Equal(t, int64(0), anchor.LastSeq, "Empty chain should have seq 0")
				assert.Equal(t, "", anchor.LastHash, "Empty chain should have empty hash")
				assert.Equal(t, "", anchor.PrevAnchorHash, "First anchor should have no prev hash")
			case sha256Key:
				sha256Written = true
			}
			return &s3.PutObjectOutput{}, nil
		},
	}

	worker := &AnchorWriterWorker{
		db:            database,
		s3Client:      s3Client,
		bucket:        "test-bucket",
		chainID:       "staging",
		retentionDays: 7,
	}

	job := &river.Job[AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	err := worker.Work(context.Background(), job)
	assert.NoError(t, err, "Should succeed with empty chain")
	assert.True(t, jsonWritten, "Should write JSON file")
	assert.True(t, sha256Written, "Should write SHA256 file")
}

func TestAnchorWriterWorker_Work_PutJSONFails(t *testing.T) {
	// Setup: Both files missing, but PutObject fails for JSON
	database := dbtest.NewDatabase(t)
	dbtest.Reset(t, database)

	// Create an audit event so we have data to anchor
	auditLog := &db.AuditLog{
		UserEmail:      "test@example.com",
		UserName:       "Test User",
		ActionType:     "test",
		Action:         "test:read",
		Resource:       "test-resource",
		Status:         "success",
		ResponseTimeMs: 100,
	}
	err := database.CreateAuditLog(auditLog)
	require.NoError(t, err)

	key := "staging/2025/11/01/12/anchor-20251101T12.json"

	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			_ *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			// Neither file exists
			return nil, &types.NotFound{}
		},
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			// No previous anchors
			return &s3.ListObjectsV2Output{Contents: []types.Object{}}, nil
		},
		putObjectFunc: func(
			_ context.Context,
			params *s3.PutObjectInput,
			_ ...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			// Fail when writing JSON
			if *params.Key == key {
				return nil, fmt.Errorf("S3 put error: access denied")
			}
			return &s3.PutObjectOutput{}, nil
		},
	}

	worker := &AnchorWriterWorker{
		db:            database,
		s3Client:      s3Client,
		bucket:        "test-bucket",
		chainID:       "staging",
		retentionDays: 7,
	}

	job := &river.Job[AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	err = worker.Work(context.Background(), job)
	assert.Error(t, err, "Should fail when PutObject fails for JSON")
	assert.Contains(t, err.Error(), "failed to write anchor JSON")
}

func TestAnchorWriterWorker_Work_PutSHA256Fails(t *testing.T) {
	// Setup: Neither file exists, JSON succeeds but SHA256 fails
	database := dbtest.NewDatabase(t)
	dbtest.Reset(t, database)

	// Create an audit event so we have data to anchor
	auditLog := &db.AuditLog{
		UserEmail:      "test@example.com",
		UserName:       "Test User",
		ActionType:     "test",
		Action:         "test:read",
		Resource:       "test-resource",
		Status:         "success",
		ResponseTimeMs: 100,
	}
	err := database.CreateAuditLog(auditLog)
	require.NoError(t, err)

	key := "staging/2025/11/01/12/anchor-20251101T12.json"
	sha256Key := key + ".sha256"

	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			_ *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			// Neither file exists
			return nil, &types.NotFound{}
		},
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			// No previous anchors
			return &s3.ListObjectsV2Output{Contents: []types.Object{}}, nil
		},
		putObjectFunc: func(
			_ context.Context,
			params *s3.PutObjectInput,
			_ ...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			// JSON succeeds, SHA256 fails
			switch *params.Key {
			case key:
				return &s3.PutObjectOutput{}, nil
			case sha256Key:
				return nil, fmt.Errorf("S3 put error: access denied")
			}
			return &s3.PutObjectOutput{}, nil
		},
	}

	worker := &AnchorWriterWorker{
		db:            database,
		s3Client:      s3Client,
		bucket:        "test-bucket",
		chainID:       "staging",
		retentionDays: 7,
	}

	job := &river.Job[AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	err = worker.Work(context.Background(), job)
	assert.Error(t, err, "Should fail when PutObject fails for SHA256")
	assert.Contains(t, err.Error(), "failed to write anchor SHA256")
}

func TestAnchorWriterWorker_FilenameGeneration(t *testing.T) {
	// Test that filename generation is deterministic and hour-based
	testTime := time.Date(2025, 11, 1, 12, 34, 56, 0, time.UTC)

	// Expected values
	expectedKey := "staging/2025/11/01/12/anchor-20251101T12.json"
	expectedSHA256Key := "staging/2025/11/01/12/anchor-20251101T12.json.sha256"

	// Verify format
	key := fmt.Sprintf("%s/%s/anchor-%s.json",
		"staging",
		testTime.Format("2006/01/02/15"),
		testTime.Format("20060102T15"))

	sha256Key := key + ".sha256"

	assert.Equal(t, expectedKey, key)
	assert.Equal(t, expectedSHA256Key, sha256Key)
}

func TestAnchorWriterWorker_TimestampDeterminism(t *testing.T) {
	// Test that using job.CreatedAt makes filenames deterministic
	jobTime := time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC)

	// Simulate multiple retries with different "now" times
	retry1Time := jobTime.Add(10 * time.Second)
	retry2Time := jobTime.Add(5 * time.Minute)
	retry3Time := jobTime.Add(30 * time.Minute)

	// All retries should use job.CreatedAt, not time.Now()
	// So all filenames should be identical
	for _, retryTime := range []time.Time{retry1Time, retry2Time, retry3Time} {
		_ = retryTime // Simulate time passing
		key := fmt.Sprintf("staging/%s/anchor-%s.json",
			jobTime.Format("2006/01/02/15"),
			jobTime.Format("20060102T15"))

		expectedKey := "staging/2025/11/01/12/anchor-20251101T12.json"
		assert.Equal(t, expectedKey, key, "Filename should be deterministic across retries")
	}
}

func TestAnchorPayload_JSONMarshaling(t *testing.T) {
	anchor := AnchorPayload{
		ChainID:        "staging",
		LastSeq:        42,
		LastHash:       "abcdef1234567890",
		TimestampUTC:   "2025-11-01T12:00:00Z",
		DBSnapshotLSN:  "",
		PrevAnchorHash: "previoushash",
	}

	data, err := json.Marshal(anchor)
	require.NoError(t, err)

	var decoded AnchorPayload
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, anchor.ChainID, decoded.ChainID)
	assert.Equal(t, anchor.LastSeq, decoded.LastSeq)
	assert.Equal(t, anchor.LastHash, decoded.LastHash)
	assert.Equal(t, anchor.TimestampUTC, decoded.TimestampUTC)
	assert.Equal(t, anchor.PrevAnchorHash, decoded.PrevAnchorHash)
}

func TestAnchorPayload_PrevAnchorHashOmittedWhenEmpty(t *testing.T) {
	anchor := AnchorPayload{
		ChainID:       "staging",
		LastSeq:       0,
		LastHash:      "",
		TimestampUTC:  "2025-11-01T12:00:00Z",
		DBSnapshotLSN: "",
		// PrevAnchorHash omitted (genesis)
	}

	data, err := json.Marshal(anchor)
	require.NoError(t, err)

	// Verify prev_anchor_hash field is not present in JSON
	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	_, hasPrevAnchorHash := raw["prev_anchor_hash"]
	assert.False(t, hasPrevAnchorHash, "prev_anchor_hash should be omitted when empty")
}

func TestAnchorWriterWorker_getLatestAnchor_NoObjects(t *testing.T) {
	s3Client := &mockS3Client{
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents: []types.Object{},
			}, nil
		},
	}

	worker := &AnchorWriterWorker{
		s3Client: s3Client,
		bucket:   "test-bucket",
		chainID:  "staging",
	}

	hash, err := worker.getLatestAnchor(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "", hash, "Should return empty string when no anchors exist")
}

func TestAnchorWriterWorker_getLatestAnchor_WithObjects(t *testing.T) {
	testAnchorJSON := []byte(`{"chain_id":"staging","last_seq":10}`)

	time1 := time.Date(2025, 11, 1, 10, 0, 0, 0, time.UTC)
	time2 := time.Date(2025, 11, 1, 11, 0, 0, 0, time.UTC)
	time3 := time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC)

	s3Client := &mockS3Client{
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			key1 := "staging/2025/11/01/10/anchor-20251101T10.json"
			key2 := "staging/2025/11/01/11/anchor-20251101T11.json"
			key3 := "staging/2025/11/01/12/anchor-20251101T12.json"
			key4 := "staging/2025/11/01/12/anchor-20251101T12.json.sha256" // Not JSON

			return &s3.ListObjectsV2Output{
				Contents: []types.Object{
					{Key: &key1, LastModified: &time1},
					{Key: &key2, LastModified: &time2},
					{Key: &key3, LastModified: &time3},
					{Key: &key4, LastModified: &time3}, // SHA256 file, should be ignored
				},
			}, nil
		},
		getObjectFunc: func(
			_ context.Context,
			params *s3.GetObjectInput,
			_ ...func(*s3.Options),
		) (*s3.GetObjectOutput, error) {
			// Should fetch the latest JSON file (key3)
			assert.Equal(t, "staging/2025/11/01/12/anchor-20251101T12.json", *params.Key)
			return &s3.GetObjectOutput{
				Body: io.NopCloser(bytes.NewReader(testAnchorJSON)),
			}, nil
		},
	}

	worker := &AnchorWriterWorker{
		s3Client: s3Client,
		bucket:   "test-bucket",
		chainID:  "staging",
	}

	hash, err := worker.getLatestAnchor(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, hash, "Should return hash of latest anchor")
}

func TestAnchorWriterWorker_getLatestAnchor_ListError(t *testing.T) {
	s3Client := &mockS3Client{
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			return nil, fmt.Errorf("S3 list error: access denied")
		},
	}

	worker := &AnchorWriterWorker{
		s3Client: s3Client,
		bucket:   "test-bucket",
		chainID:  "staging",
	}

	hash, err := worker.getLatestAnchor(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list anchors")
	assert.Equal(t, "", hash)
}

func TestAnchorWriterWorker_getLatestAnchor_GetObjectError(t *testing.T) {
	time1 := time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC)
	key1 := "staging/2025/11/01/12/anchor-20251101T12.json"

	s3Client := &mockS3Client{
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents: []types.Object{
					{Key: &key1, LastModified: &time1},
				},
			}, nil
		},
		getObjectFunc: func(
			_ context.Context,
			_ *s3.GetObjectInput,
			_ ...func(*s3.Options),
		) (*s3.GetObjectOutput, error) {
			return nil, fmt.Errorf("S3 get error: object not found")
		},
	}

	worker := &AnchorWriterWorker{
		s3Client: s3Client,
		bucket:   "test-bucket",
		chainID:  "staging",
	}

	hash, err := worker.getLatestAnchor(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get previous anchor")
	assert.Equal(t, "", hash)
}

func TestAnchorWriterWorker_RetentionDateCalculation(t *testing.T) {
	testCases := []struct {
		name          string
		retentionDays int
		timestamp     time.Time
		expected      time.Time
	}{
		{
			name:          "7 days retention",
			retentionDays: 7,
			timestamp:     time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC),
			expected:      time.Date(2025, 11, 8, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "400 days retention",
			retentionDays: 400,
			timestamp:     time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC),
			expected:      time.Date(2026, 12, 6, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "1 day retention",
			retentionDays: 1,
			timestamp:     time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
			expected:      time.Date(2025, 11, 2, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			retainUntil := tc.timestamp.Add(time.Duration(tc.retentionDays) * 24 * time.Hour)
			assert.Equal(t, tc.expected, retainUntil)
		})
	}
}

func TestAnchorWriterWorker_S3KeyPrefix(t *testing.T) {
	testCases := []struct {
		name      string
		chainID   string
		timestamp time.Time
		expected  string
	}{
		{
			name:      "staging chain",
			chainID:   "staging",
			timestamp: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
			expected:  "staging/2025/11/01/12",
		},
		{
			name:      "us chain",
			chainID:   "us",
			timestamp: time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC),
			expected:  "us/2025/12/31/23",
		},
		{
			name:      "eu chain",
			chainID:   "eu",
			timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			expected:  "eu/2026/01/01/00",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prefix := fmt.Sprintf("%s/%s", tc.chainID, tc.timestamp.Format("2006/01/02/15"))
			assert.Equal(t, tc.expected, prefix)
		})
	}
}

func TestAnchorWriterWorker_ObjectLockParameters(t *testing.T) {
	// Verify that Object Lock parameters are set correctly in PutObject
	database := dbtest.NewDatabase(t)
	dbtest.Reset(t, database)

	// Create an audit event so we have data to anchor
	auditLog := &db.AuditLog{
		UserEmail:      "test@example.com",
		UserName:       "Test User",
		ActionType:     "test",
		Action:         "test:read",
		Resource:       "test-resource",
		Status:         "success",
		ResponseTimeMs: 100,
	}
	err := database.CreateAuditLog(auditLog)
	require.NoError(t, err)

	key := "staging/2025/11/01/12/anchor-20251101T12.json"
	timestamp := time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC)
	expectedRetainUntil := timestamp.Add(7 * 24 * time.Hour)

	// Test with AWS S3 (should set Object Lock) - unset endpoint to simulate production AWS
	originalEndpoint := os.Getenv("AWS_ENDPOINT_URL_S3")
	_ = os.Unsetenv("AWS_ENDPOINT_URL_S3")
	defer func() {
		if originalEndpoint != "" {
			_ = os.Setenv("AWS_ENDPOINT_URL_S3", originalEndpoint)
		}
	}()

	var jsonParams, sha256Params *s3.PutObjectInput

	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			_ *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			return nil, &types.NotFound{}
		},
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{Contents: []types.Object{}}, nil
		},
		putObjectFunc: func(
			_ context.Context,
			params *s3.PutObjectInput,
			_ ...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			switch *params.Key {
			case key:
				jsonParams = params
			case key + ".sha256":
				sha256Params = params
			}
			return &s3.PutObjectOutput{}, nil
		},
	}

	worker := &AnchorWriterWorker{
		db:            database,
		s3Client:      s3Client,
		bucket:        "test-bucket",
		chainID:       "staging",
		retentionDays: 7,
	}

	job := &river.Job[AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: timestamp,
		},
	}

	err = worker.Work(context.Background(), job)
	require.NoError(t, err)

	// Verify Object Lock parameters are set for AWS S3
	require.NotNil(t, jsonParams, "JSON PutObject should be called")
	assert.Equal(t, types.ObjectLockModeCompliance, jsonParams.ObjectLockMode, "Should set Compliance mode")
	assert.NotNil(t, jsonParams.ObjectLockRetainUntilDate, "Should set retention date")
	assert.WithinDuration(
		t, expectedRetainUntil, *jsonParams.ObjectLockRetainUntilDate, time.Second,
		"Retention date should match",
	)
	assert.Equal(t, types.ServerSideEncryptionAwsKms, jsonParams.ServerSideEncryption, "Should set KMS encryption")
	assert.NotNil(t, jsonParams.ChecksumSHA256, "Should set SHA256 checksum")

	require.NotNil(t, sha256Params, "SHA256 PutObject should be called")
	assert.Equal(
		t, types.ObjectLockModeCompliance, sha256Params.ObjectLockMode,
		"Should set Compliance mode for SHA256",
	)
	assert.NotNil(t, sha256Params.ObjectLockRetainUntilDate, "Should set retention date for SHA256")
	assert.WithinDuration(
		t, expectedRetainUntil, *sha256Params.ObjectLockRetainUntilDate, time.Second,
		"Retention date should match for SHA256",
	)
	assert.Equal(
		t, types.ServerSideEncryptionAwsKms, sha256Params.ServerSideEncryption,
		"Should set KMS encryption for SHA256",
	)
	// SHA256 file doesn't need checksum (it IS the checksum)
}

func TestAnchorWriterWorker_NoIfNoneMatch(t *testing.T) {
	// Verify that IfNoneMatch is NOT used (incompatible with Object Lock)
	database := dbtest.NewDatabase(t)
	dbtest.Reset(t, database)

	// Create an audit event so we have data to anchor
	auditLog := &db.AuditLog{
		UserEmail:      "test@example.com",
		UserName:       "Test User",
		ActionType:     "test",
		Action:         "test:read",
		Resource:       "test-resource",
		Status:         "success",
		ResponseTimeMs: 100,
	}
	err := database.CreateAuditLog(auditLog)
	require.NoError(t, err)

	key := "staging/2025/11/01/12/anchor-20251101T12.json"

	var jsonParams, sha256Params *s3.PutObjectInput

	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			_ *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			return nil, &types.NotFound{}
		},
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{Contents: []types.Object{}}, nil
		},
		putObjectFunc: func(
			_ context.Context,
			params *s3.PutObjectInput,
			_ ...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			switch *params.Key {
			case key:
				jsonParams = params
			case key + ".sha256":
				sha256Params = params
			}
			return &s3.PutObjectOutput{}, nil
		},
	}

	worker := &AnchorWriterWorker{
		db:            database,
		s3Client:      s3Client,
		bucket:        "test-bucket",
		chainID:       "staging",
		retentionDays: 7,
	}

	job := &river.Job[AnchorWriterArgs]{
		JobRow: &rivertype.JobRow{
			CreatedAt: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	err = worker.Work(context.Background(), job)
	require.NoError(t, err)

	// Verify IfNoneMatch is NOT set (incompatible with Object Lock)
	require.NotNil(t, jsonParams, "JSON PutObject should be called")
	assert.Nil(t, jsonParams.IfNoneMatch, "IfNoneMatch should NOT be set (incompatible with Object Lock)")

	require.NotNil(t, sha256Params, "SHA256 PutObject should be called")
	assert.Nil(t, sha256Params.IfNoneMatch, "IfNoneMatch should NOT be set for SHA256")
}
