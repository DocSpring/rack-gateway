package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

const testChainID = "staging"

var firstAnchorHour = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// putHourlyAnchors stores count anchors, one per hour from firstAnchorHour, each written in its hour. It returns
// the body of the last one.
func putHourlyAnchors(bucket *fakes3.Bucket, count int) []byte {
	var body []byte
	for i := 0; i < count; i++ {
		ts := firstAnchorHour.Add(time.Duration(i) * time.Hour)
		body = []byte(fmt.Sprintf(`{"chain_id":"staging","last_seq":%d,"ts_utc":%q}`, i, ts.Format(time.RFC3339)))
		bucket.PutAt(AnchorKey(testChainID, ts), body, ts.Add(time.Minute))
	}
	return body
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newTestWorker(database *db.Database, s3Client S3Client) *AnchorWriterWorker {
	return NewAnchorWriterWorker(database, s3Client, "test-bucket", testChainID)
}

func anchorJob(at time.Time) *river.Job[AnchorWriterArgs] {
	return &river.Job[AnchorWriterArgs]{JobRow: &rivertype.JobRow{CreatedAt: at}}
}

func TestGetLatestAnchorLooksPastTheFirstThousand(t *testing.T) {
	bucket := fakes3.New(firstAnchorHour)
	bucket.PageSize = 100
	last := putHourlyAnchors(bucket, 1500)
	now := firstAnchorHour.Add(1500 * time.Hour)

	hash, err := newTestWorker(nil, bucket).getLatestAnchor(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, sha256Hex(last), hash, "prev_anchor_hash must name the latest anchor, not the 1000th")
}

func TestGetLatestAnchorListsEverythingWhenNothingIsRecent(t *testing.T) {
	bucket := fakes3.New(firstAnchorHour)
	bucket.PageSize = 100
	last := putHourlyAnchors(bucket, 1200)
	now := firstAnchorHour.Add(1200*time.Hour + 60*24*time.Hour) // 60 days after the last anchor

	hash, err := newTestWorker(nil, bucket).getLatestAnchor(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, sha256Hex(last), hash)
}

func TestGetLatestAnchorUsesWriteOrder(t *testing.T) {
	bucket := fakes3.New(firstAnchorHour)
	hour10 := firstAnchorHour.Add(10 * time.Hour)
	hour11 := firstAnchorHour.Add(11 * time.Hour)
	bucket.PutAt(AnchorKey(testChainID, hour11), []byte(`{"hour":11}`), hour11)
	// A retried job for the earlier hour, written after the later hour's anchor.
	lateRetry := []byte(`{"hour":10}`)
	bucket.PutAt(AnchorKey(testChainID, hour10), lateRetry, hour11.Add(30*time.Minute))

	hash, err := newTestWorker(nil, bucket).getLatestAnchor(context.Background(), hour11.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, sha256Hex(lateRetry), hash)
}

func TestAnchorWriterRecordsFormatAndHashV2Start(t *testing.T) {
	database := dbtest.NewDatabase(t)
	var last *db.AuditLog
	for i := 0; i < 2; i++ {
		last = &db.AuditLog{UserEmail: "test@example.com", ActionType: "test", Action: "test.read", Status: "success"}
		require.NoError(t, database.CreateAuditLog(last))
	}
	bucket := fakes3.New(firstAnchorHour)

	require.NoError(t, newTestWorker(database, bucket).Work(context.Background(), anchorJob(firstAnchorHour)))

	versions := bucket.Versions(AnchorKey(testChainID, firstAnchorHour))
	require.Len(t, versions, 1)
	var anchor AnchorPayload
	require.NoError(t, json.Unmarshal(versions[0], &anchor))
	assert.Equal(t, AnchorFormat, anchor.Format)
	require.NotNil(t, anchor.HashV2FromSeq)
	assert.Equal(t, int64(0), *anchor.HashV2FromSeq)
	assert.Equal(t, last.ChainIndex, anchor.LastSeq)
	assert.Equal(t, hex.EncodeToString(last.EventHash), anchor.LastHash)
	assert.Empty(t, anchor.PrevAnchorHash)
}

func TestAnchorWriterChainsToThePreviousAnchor(t *testing.T) {
	database := dbtest.NewDatabase(t)
	bucket := fakes3.New(firstAnchorHour)
	worker := newTestWorker(database, bucket)

	require.NoError(t, worker.Work(context.Background(), anchorJob(firstAnchorHour)))
	nextHour := firstAnchorHour.Add(time.Hour)
	bucket.AdvanceTo(nextHour)
	require.NoError(t, worker.Work(context.Background(), anchorJob(nextHour)))
	// A retry of the same hour leaves the anchor alone.
	require.NoError(t, worker.Work(context.Background(), anchorJob(nextHour.Add(10*time.Minute))))

	first := bucket.Versions(AnchorKey(testChainID, firstAnchorHour))
	second := bucket.Versions(AnchorKey(testChainID, nextHour))
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	var anchor AnchorPayload
	require.NoError(t, json.Unmarshal(second[0], &anchor))
	assert.Equal(t, sha256Hex(first[0]), anchor.PrevAnchorHash)
}

func TestAnchorWriterFailsWhenItCannotTellIfTheAnchorExists(t *testing.T) {
	putCalled := false
	s3Client := &mockS3Client{
		headObjectFunc: func(
			_ context.Context,
			_ *s3.HeadObjectInput,
			_ ...func(*s3.Options),
		) (*s3.HeadObjectOutput, error) {
			return nil, errors.New("access denied")
		},
		putObjectFunc: func(
			_ context.Context,
			_ *s3.PutObjectInput,
			_ ...func(*s3.Options),
		) (*s3.PutObjectOutput, error) {
			putCalled = true
			return &s3.PutObjectOutput{}, nil
		},
	}

	err := newTestWorker(dbtest.NewDatabase(t), s3Client).Work(context.Background(), anchorJob(firstAnchorHour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
	assert.False(t, putCalled, "writing again could add a second version of an existing anchor")
}

func TestAnchorWriterFailsWhenThePreviousAnchorCannotBeListed(t *testing.T) {
	database := dbtest.NewDatabase(t)
	s3Client := &mockS3Client{
		listObjectsV2Func: func(
			_ context.Context,
			_ *s3.ListObjectsV2Input,
			_ ...func(*s3.Options),
		) (*s3.ListObjectsV2Output, error) {
			return nil, errors.New("throttled")
		},
	}

	err := newTestWorker(database, s3Client).Work(context.Background(), anchorJob(firstAnchorHour))
	require.Error(t, err, "an anchor without its prev_anchor_hash would break the anchor chain")
	assert.Contains(t, err.Error(), "failed to list anchors")
}

// An anchor is a few hundred bytes: a bigger body is refused after reading no more than MaxAnchorSize.
func TestReadAnchorBodyRefusesOversizedBodies(t *testing.T) {
	body, err := ReadAnchorBody(strings.NewReader(strings.Repeat("x", MaxAnchorSize)))
	require.NoError(t, err)
	require.Len(t, body, MaxAnchorSize)

	reader := strings.NewReader(strings.Repeat("x", 10*MaxAnchorSize))
	_, err = ReadAnchorBody(reader)
	require.ErrorIs(t, err, ErrAnchorTooLarge)
	require.Equal(t, 9*MaxAnchorSize-1, reader.Len(), "it read no more than the limit")
}

// The writer chains only to objects at keys it writes, and refuses an oversized one there instead of reading it.
func TestLatestAnchorIsAlwaysAnAnchor(t *testing.T) {
	bucket := fakes3.New(firstAnchorHour)
	last := putHourlyAnchors(bucket, 3)
	now := firstAnchorHour.Add(3 * time.Hour)
	// Both stored after every anchor, at keys the writer never writes
	bucket.PutAt(testChainID+"/notes.json", []byte(`{}`), now)
	bucket.PutAt(testChainID+"/2026/01/01/02/anchor-20260101T03.json", []byte(`{}`), now)
	worker := newTestWorker(nil, bucket)

	hash, err := worker.getLatestAnchor(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, sha256Hex(last), hash)

	bucket.PutAt(AnchorKey(testChainID, now), make([]byte, MaxAnchorSize+1), now.Add(time.Minute))
	_, err = worker.getLatestAnchor(context.Background(), now.Add(time.Hour))
	require.ErrorIs(t, err, ErrAnchorTooLarge)
}
