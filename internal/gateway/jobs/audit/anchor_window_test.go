package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

// captureLogs sends the standard logger (which gtwlog writes to) to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// S3 can store an object and still fail the request (a 5xx, or a connection dropped before the response). The
// SDK's retryer would then send the anchor again and add a second version of its key, which Object Lock keeps.
func TestAnchorPutIsSentOnce(t *testing.T) {
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<Error><Code>InternalError</Code><Message>try again</Message></Error>`))
	}))
	t.Cleanup(server.Close)
	// A real client, so the SDK's standard retryer (3 attempts, retrying 500s) is in play.
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
		Credentials:  aws.AnonymousCredentials{},
	})

	err := newTestWorker(nil, client).putAnchorObject(
		context.Background(), AnchorKey(testChainID, firstAnchorHour), []byte(`{}`), nil,
	)
	require.Error(t, err)
	assert.Equal(t, int32(1), puts.Load(), "the SDK must not resend the anchor; the job's retry checks for it first")
}

func TestAnchorWriterRefusesAKeyCreatedBeforeItsHour(t *testing.T) {
	database := dbtest.NewDatabase(t)
	bucket := fakes3.New(firstAnchorHour)
	hour := firstAnchorHour.Add(5 * time.Hour)
	key := AnchorKey(testChainID, hour)
	// Someone with write access to the bucket created a later hour's anchor ahead of time. If the writer took the
	// existing key as its own, nothing would anchor that hour for as long as Object Lock keeps the object.
	bucket.PutAt(key, []byte(`{"chain_id":"staging"}`), firstAnchorHour.Add(time.Minute))
	logs := captureLogs(t)
	worker := newTestWorker(database, bucket)

	err := worker.Work(context.Background(), anchorJob(hour.Add(time.Minute)))
	var canceled *river.JobCancelError
	require.ErrorAs(t, err, &canceled, "retrying can't help while Object Lock keeps the object")
	require.ErrorIs(t, err, ErrUnexpectedAnchor)
	assert.Contains(t, logs.String(), "[ERROR] audit anchor: "+ErrUnexpectedAnchor.Error()+": "+key)
	assert.Contains(t, logs.String(), "stored at 2026-01-01T00:01:00Z")

	// write-anchor fails the same way.
	require.ErrorIs(t, worker.WriteAnchor(context.Background(), hour.Add(2*time.Minute)), ErrUnexpectedAnchor)
	assert.Len(t, bucket.Versions(key), 1, "the writer doesn't add a version of its own")
}

// anchorBody is an anchor for the hour of ts recording the chain up to lastSeq.
func anchorBody(t *testing.T, chainID string, ts time.Time, lastSeq int64, lastHash string) []byte {
	t.Helper()
	body, err := json.Marshal(AnchorPayload{
		ChainID: chainID, LastSeq: lastSeq, LastHash: lastHash, TimestampUTC: ts.UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	return body
}

func TestAnchorWriterChecksTheExistingAnchor(t *testing.T) {
	database := dbtest.NewDatabase(t)
	hour := firstAnchorHour.Add(10 * time.Hour)
	for i := 0; i < 2; i++ {
		event := &db.AuditLog{
			UserEmail: "test@example.com", ActionType: "test", Action: "test.read", Status: "success",
			Timestamp: hour.Add(time.Minute),
		}
		require.NoError(t, database.CreateAuditLog(event))
	}
	ts := hour.Add(20 * time.Minute) // the job, 20 minutes into the hour
	stored := hour.Add(10 * time.Minute)
	emptyChain := anchorBody(t, testChainID, hour, 0, "")
	for _, tc := range []struct {
		name    string
		written time.Time
		body    []byte
		ok      bool
	}{
		{"written earlier in the hour", stored, anchorBody(t, testChainID, hour, 1, "ab"), true},
		{"written just before the hour by S3's clock", hour.Add(-time.Minute), emptyChain, true},
		{"written by a late retry", ts.Add(5 * time.Hour), anchorBody(t, testChainID, hour, 1, "ab"), true},
		{"created ahead of time", hour.Add(-AnchorClockSkew - time.Second), emptyChain, false},
		{"created a day ahead", hour.Add(-24 * time.Hour), emptyChain, false},
		{"not an anchor", stored, []byte(`not json`), false},
		{"another chain's anchor", stored, anchorBody(t, "other", hour, 1, "ab"), false},
		{"another hour's anchor", stored, anchorBody(t, testChainID, hour.Add(-time.Hour), 1, "ab"), false},
		{"a stale copy of an earlier anchor", stored, anchorBody(t, testChainID, hour, 0, "ab"), false},
		{"an empty chain's anchor after events were written", stored, emptyChain, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := fakes3.New(firstAnchorHour)
			key := AnchorKey(testChainID, ts)
			bucket.PutAt(key, tc.body, tc.written)

			err := newTestWorker(database, bucket).WriteAnchor(context.Background(), ts)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrUnexpectedAnchor)
			}
			assert.Len(t, bucket.Versions(key), 1, "the writer never adds a version of an existing anchor")
		})
	}
}

func TestAnchorJobRefusesACreationTimeAfterItsAttempt(t *testing.T) {
	bucket := fakes3.New(firstAnchorHour)
	logs := captureLogs(t)
	// Someone moved the job's creation time forward in the database to name a later hour.
	job := anchorJob(firstAnchorHour.Add(5 * time.Hour))
	attempted := firstAnchorHour.Add(time.Minute)
	job.AttemptedAt = &attempted
	key := AnchorKey(testChainID, job.CreatedAt)

	err := newTestWorker(nil, bucket).Work(context.Background(), job)
	var canceled *river.JobCancelError
	require.ErrorAs(t, err, &canceled)
	assert.Contains(t, err.Error(), "refused to write anchor "+key+": the job was created at 2026-01-01T05:00:00Z")
	assert.Contains(t, logs.String(), "[ERROR] audit anchor: refused to write anchor "+key)
	assert.Empty(t, bucket.Versions(key))

	// A creation time within the clock skew of the attempt is fine.
	job = anchorJob(firstAnchorHour.Add(AnchorClockSkew))
	job.AttemptedAt = &attempted
	require.NoError(t, newTestWorker(dbtest.NewDatabase(t), bucket).Work(context.Background(), job))
}

func TestWriteCurrentAnchorUsesTheDatabaseClock(t *testing.T) {
	database := dbtest.NewDatabase(t)
	dbNow := func() time.Time {
		var now time.Time
		require.NoError(t, database.DB().QueryRow("SELECT now()").Scan(&now))
		return now
	}
	before := dbNow()
	bucket := fakes3.New(before)
	require.NoError(t, newTestWorker(database, bucket).WriteCurrentAnchor(context.Background()))
	after := dbNow()

	versions := bucket.Versions(AnchorKey(testChainID, before))
	if !before.UTC().Truncate(time.Hour).Equal(after.UTC().Truncate(time.Hour)) {
		versions = append(versions, bucket.Versions(AnchorKey(testChainID, after))...)
	}
	assert.Len(t, versions, 1, "the anchor is named by the database's hour")
}

func TestAnchorJobGivesUpAfterTheWriteWindow(t *testing.T) {
	bucket := fakes3.New(firstAnchorHour)
	logs := captureLogs(t)
	key := AnchorKey(testChainID, firstAnchorHour)

	// A retry that starts after the window would write an anchor verify-audit rejects as backfilled. It gives up
	// before touching the database or the bucket.
	late := anchorJob(firstAnchorHour)
	attempted := firstAnchorHour.Add(AnchorWriteWindow + time.Minute)
	late.AttemptedAt = &attempted
	err := newTestWorker(nil, bucket).Work(context.Background(), late)
	var canceled *river.JobCancelError
	require.ErrorAs(t, err, &canceled)
	assert.Contains(t, err.Error(), "gave up on anchor "+key+": this attempt started 6h1m0s after the job was created")
	assert.Contains(t, logs.String(), "[ERROR] audit anchor: gave up on anchor "+key)
	assert.Empty(t, bucket.Versions(key))

	// A retry within the window writes the anchor.
	inTime := anchorJob(firstAnchorHour)
	attempted = firstAnchorHour.Add(AnchorWriteWindow - time.Minute)
	inTime.AttemptedAt = &attempted
	bucket.AdvanceTo(attempted)
	require.NoError(t, newTestWorker(dbtest.NewDatabase(t), bucket).Work(context.Background(), inTime))
	assert.Len(t, bucket.Versions(key), 1)
}
