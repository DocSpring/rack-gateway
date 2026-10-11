package audit

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

// blockingHeadBucket is a fake bucket whose first HeadObject call waits until release is called, so a test can
// hold an anchor write in the middle of its locked section.
type blockingHeadBucket struct {
	*fakes3.Bucket
	entered     chan struct{}
	released    chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func newBlockingHeadBucket(bucket *fakes3.Bucket) *blockingHeadBucket {
	return &blockingHeadBucket{Bucket: bucket, entered: make(chan struct{}), released: make(chan struct{})}
}

func (b *blockingHeadBucket) HeadObject(
	ctx context.Context,
	params *s3.HeadObjectInput,
	optFns ...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	b.enterOnce.Do(func() { close(b.entered) })
	select {
	case <-b.released:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.Bucket.HeadObject(ctx, params, optFns...)
}

func (b *blockingHeadBucket) release() {
	b.releaseOnce.Do(func() { close(b.released) })
}

// startBlockedAnchorWrite starts writing the anchor for at and returns once the write holds the anchor lock and
// is waiting in HeadObject. The returned channel receives the write's result after release.
func startBlockedAnchorWrite(t *testing.T, database *db.Database, bucket *fakes3.Bucket, at time.Time) (
	*blockingHeadBucket, <-chan error,
) {
	t.Helper()
	blocking := newBlockingHeadBucket(bucket)
	t.Cleanup(blocking.release)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() { result <- newTestWorker(database, blocking).WriteAnchor(ctx, at) }()
	select {
	case <-blocking.entered:
	case <-ctx.Done():
		t.Fatal("the first anchor write never reached HeadObject")
	}
	return blocking, result
}

func TestAnchorWritesForTheSameHourDoNotOverlap(t *testing.T) {
	database := dbtest.NewDatabase(t)
	bucket := fakes3.New(firstAnchorHour)
	first, result := startBlockedAnchorWrite(t, database, bucket, firstAnchorHour)

	// While the first write holds the lock, a second one for the same hour (a job on another instance, or
	// write-anchor) neither checks nor writes the key.
	other := newTestWorker(database, bucket)
	err := other.WriteAnchor(context.Background(), firstAnchorHour.Add(5*time.Minute))
	require.ErrorIs(t, err, db.ErrAuditAnchorLockBusy)
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, other.Work(context.Background(), anchorJob(firstAnchorHour)), &snooze,
		"a job that finds the lock taken is snoozed, not failed")

	first.release()
	require.NoError(t, <-result)
	require.NoError(t, other.WriteAnchor(context.Background(), firstAnchorHour.Add(5*time.Minute)))
	assert.Len(t, bucket.Versions(AnchorKey(testChainID, firstAnchorHour)), 1, "the hour's anchor is written once")
}

func TestAnchorWriteWaitsForThePreviousAnchor(t *testing.T) {
	database := dbtest.NewDatabase(t)
	require.NoError(t, database.CreateAuditLog(&db.AuditLog{
		UserEmail: "test@example.com", ActionType: "test", Action: "test.read", Status: "success",
	}))
	bucket := fakes3.New(firstAnchorHour)
	nextHour := firstAnchorHour.Add(time.Hour)
	first, result := startBlockedAnchorWrite(t, database, bucket, firstAnchorHour)

	// Without the lock, the next hour's anchor would read the latest anchor before the first one is written and
	// link to the one before it (here: none), forking the anchor chain.
	next := newTestWorker(database, bucket)
	require.ErrorIs(t, next.WriteAnchor(context.Background(), nextHour), db.ErrAuditAnchorLockBusy)
	assert.Empty(t, bucket.Versions(AnchorKey(testChainID, nextHour)))

	first.release()
	require.NoError(t, <-result)
	require.NoError(t, next.WriteAnchor(context.Background(), nextHour))

	firstBody := bucket.Versions(AnchorKey(testChainID, firstAnchorHour))
	nextBody := bucket.Versions(AnchorKey(testChainID, nextHour))
	require.Len(t, firstBody, 1)
	require.Len(t, nextBody, 1)
	var anchor AnchorPayload
	require.NoError(t, json.Unmarshal(nextBody[0], &anchor))
	assert.Equal(t, sha256Hex(firstBody[0]), anchor.PrevAnchorHash)
}
