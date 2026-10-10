package deployapprovals

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

type fakeExpirer struct {
	expired int64
	err     error
	calls   int
}

func (f *fakeExpirer) ExpireDeployApprovalRequests() (int64, error) {
	f.calls++
	return f.expired, f.err
}

func TestExpireWorkerSweeps(t *testing.T) {
	store := &fakeExpirer{expired: 2}
	require.NoError(t, NewExpireWorker(store).Work(context.Background(), &river.Job[ExpireArgs]{}))
	require.Equal(t, 1, store.calls)
}

func TestExpireWorkerReturnsErrorsForRetry(t *testing.T) {
	store := &fakeExpirer{err: errors.New("db down")}
	err := NewExpireWorker(store).Work(context.Background(), &river.Job[ExpireArgs]{})
	require.ErrorContains(t, err, "db down")
}
