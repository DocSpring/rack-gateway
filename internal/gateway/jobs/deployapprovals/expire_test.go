package deployapprovals

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// fakeExpirer expires its requests, building each one's audit event as the database does.
type fakeExpirer struct {
	expired       []db.ExpiredDeployApproval
	err           error
	pendingMaxAge time.Duration
}

func (f *fakeExpirer) ExpireDeployApprovalRequests(
	pendingMaxAge time.Duration,
	entry func(db.ExpiredDeployApproval) *db.AuditLog,
) ([]db.ExpiredDeployApproval, []*db.AuditLog, error) {
	f.pendingMaxAge = pendingMaxAge
	if f.err != nil {
		return nil, nil, f.err
	}
	events := make([]*db.AuditLog, 0, len(f.expired))
	for _, e := range f.expired {
		events = append(events, entry(e))
	}
	return f.expired, events, nil
}

// Each expired request is audit-logged as a system action.
func TestExpireWorkerAuditsEachExpiry(t *testing.T) {
	store := &fakeExpirer{expired: []db.ExpiredDeployApproval{
		{ID: 1, PublicID: "a", App: "web", GitCommitHash: "abc", PreviousStatus: "approved"},
		{ID: 2, PublicID: "b", App: "api", GitCommitHash: "def", PreviousStatus: "pending"},
	}}
	var audited []*db.AuditLog
	recording := &recordingExpirer{fakeExpirer: store, events: &audited}

	require.NoError(t, NewExpireWorker(recording, 7*24*time.Hour).Work(context.Background(), &river.Job[ExpireArgs]{}))
	require.Equal(t, 7*24*time.Hour, store.pendingMaxAge)
	require.Len(t, audited, 2)
	require.Equal(t, "system", audited[0].UserEmail)
	require.Equal(t, "deploy_approval_request.expire", audited[0].Action)
	require.Equal(t, "1", audited[0].Resource)
	require.JSONEq(t,
		`{"public_id":"b","app":"api","git_commit_hash":"def","previous_status":"pending"}`, audited[1].Details)
}

// A sweep whose audit events can't be appended expires nothing (the database rolls it back), fails, and makes
// the gateway unhealthy.
func TestExpireWorkerFailsWhenTheAuditLogCantBeWritten(t *testing.T) {
	store := &fakeExpirer{err: fmt.Errorf("%w for expired deploy approval 1: audit down", db.ErrAuditAppend)}
	err := NewExpireWorker(store, 0).Work(context.Background(), &river.Job[ExpireArgs]{})
	require.ErrorIs(t, err, db.ErrAuditAppend)
	require.Error(t, audit.WriteHealth(time.Now()))
}

func TestExpireWorkerReturnsErrorsForRetry(t *testing.T) {
	store := &fakeExpirer{err: errors.New("db down")}
	err := NewExpireWorker(store, 0).Work(context.Background(), &river.Job[ExpireArgs]{})
	require.ErrorContains(t, err, "db down")
}

// batchingExpirer hands out a backlog a batch at a time, as the database does.
type batchingExpirer struct {
	backlog int
	calls   int
}

func (b *batchingExpirer) ExpireDeployApprovalRequests(
	_ time.Duration,
	entry func(db.ExpiredDeployApproval) *db.AuditLog,
) ([]db.ExpiredDeployApproval, []*db.AuditLog, error) {
	b.calls++
	n := min(b.backlog, db.DeployApprovalExpiryBatchSize)
	b.backlog -= n
	expired := make([]db.ExpiredDeployApproval, n)
	events := make([]*db.AuditLog, 0, n)
	for i := range expired {
		expired[i] = db.ExpiredDeployApproval{ID: int64(i + 1), PreviousStatus: "pending"}
		events = append(events, entry(expired[i]))
	}
	return expired, events, nil
}

// A backlog larger than a batch is worked through in several transactions, each of which makes progress.
func TestExpireWorksThroughABacklogInBatches(t *testing.T) {
	store := &batchingExpirer{backlog: 2*db.DeployApprovalExpiryBatchSize + 3}
	require.NoError(t, Expire(store, time.Hour))
	require.Equal(t, 3, store.calls)
	require.Zero(t, store.backlog)

	// A backlog of exactly one batch takes one more (empty) call to see it's done.
	store = &batchingExpirer{backlog: db.DeployApprovalExpiryBatchSize}
	require.NoError(t, Expire(store, time.Hour))
	require.Equal(t, 2, store.calls)
}

// recordingExpirer keeps the events its expirer returned.
type recordingExpirer struct {
	*fakeExpirer
	events *[]*db.AuditLog
}

func (r *recordingExpirer) ExpireDeployApprovalRequests(
	pendingMaxAge time.Duration,
	entry func(db.ExpiredDeployApproval) *db.AuditLog,
) ([]db.ExpiredDeployApproval, []*db.AuditLog, error) {
	expired, events, err := r.fakeExpirer.ExpireDeployApprovalRequests(pendingMaxAge, entry)
	*r.events = events
	return expired, events, err
}
