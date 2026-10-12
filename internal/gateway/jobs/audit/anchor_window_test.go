package audit

import (
	"bytes"
	"context"
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

	err := newTestWorker(nil, client).putObjectWithRetention(
		context.Background(), AnchorKey(testChainID, firstAnchorHour), []byte(`{}`), firstAnchorHour, nil,
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

func TestCheckExistingAnchor(t *testing.T) {
	ts := firstAnchorHour.Add(10*time.Hour + 20*time.Minute) // the job, 20 minutes into the hour
	key := AnchorKey(testChainID, ts)
	for _, tc := range []struct {
		name    string
		written time.Time
		ok      bool
	}{
		{"written earlier in the hour", firstAnchorHour.Add(10*time.Hour + time.Minute), true},
		{"written just before the hour by S3's clock", firstAnchorHour.Add(10*time.Hour - time.Minute), true},
		{"written by a late retry", ts.Add(5 * time.Hour), true},
		{"created ahead of time", firstAnchorHour.Add(10*time.Hour - AnchorClockSkew - time.Second), false},
		{"created a day ahead", firstAnchorHour.Add(-14 * time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkExistingAnchor(key, ts, tc.written)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, ErrUnexpectedAnchor)
			}
		})
	}
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
