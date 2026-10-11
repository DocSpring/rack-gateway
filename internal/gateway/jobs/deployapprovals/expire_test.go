package deployapprovals

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

type fakeExpirer struct {
	expired       []db.ExpiredDeployApproval
	err           error
	pendingMaxAge time.Duration
}

func (f *fakeExpirer) ExpireDeployApprovalRequests(pendingMaxAge time.Duration) ([]db.ExpiredDeployApproval, error) {
	f.pendingMaxAge = pendingMaxAge
	return f.expired, f.err
}

// Each expired request is audit-logged as a system action.
func TestExpireWorkerAuditsEachExpiry(t *testing.T) {
	store := &fakeExpirer{expired: []db.ExpiredDeployApproval{
		{ID: 1, PublicID: "a", App: "web", GitCommitHash: "abc", PreviousStatus: "approved"},
		{ID: 2, PublicID: "b", App: "api", GitCommitHash: "def", PreviousStatus: "pending"},
	}}
	var audited []*db.AuditLog
	worker := NewExpireWorker(store, 7*24*time.Hour, func(al *db.AuditLog) error {
		audited = append(audited, al)
		return nil
	})

	require.NoError(t, worker.Work(context.Background(), &river.Job[ExpireArgs]{}))
	require.Equal(t, 7*24*time.Hour, store.pendingMaxAge)
	require.Len(t, audited, 2)
	require.Equal(t, "system", audited[0].UserEmail)
	require.Equal(t, "deploy_approval_request.expire", audited[0].Action)
	require.Equal(t, "1", audited[0].Resource)
	require.JSONEq(t,
		`{"public_id":"b","app":"api","git_commit_hash":"def","previous_status":"pending"}`, audited[1].Details)
}

// A failed audit write doesn't stop the others, and fails the job so it's reported.
func TestExpireWorkerReportsAuditFailures(t *testing.T) {
	store := &fakeExpirer{expired: []db.ExpiredDeployApproval{{ID: 1}, {ID: 2}}}
	calls := 0
	worker := NewExpireWorker(store, 0, func(*db.AuditLog) error {
		calls++
		return errors.New("audit down")
	})
	err := worker.Work(context.Background(), &river.Job[ExpireArgs]{})
	require.ErrorContains(t, err, "audit down")
	require.Equal(t, 2, calls)
}

func TestExpireWorkerReturnsErrorsForRetry(t *testing.T) {
	store := &fakeExpirer{err: errors.New("db down")}
	err := NewExpireWorker(store, 0, func(*db.AuditLog) error { return nil }).
		Work(context.Background(), &river.Job[ExpireArgs]{})
	require.ErrorContains(t, err, "db down")
}
