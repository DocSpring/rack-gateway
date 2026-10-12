// Package deployapprovals holds background jobs for deploy approval requests.
package deployapprovals

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// ExpireInterval is how often the expiry sweep runs.
const ExpireInterval = 5 * time.Minute

// ExpireArgs schedules a sweep that expires approvals past their window and old pending requests.
type ExpireArgs struct{}

// Kind returns the unique identifier for this job type.
func (ExpireArgs) Kind() string { return "deploy_approvals:expire" }

// Expirer expires deploy approval requests, appending each expiry's audit event in the same transaction
// (*db.Database implements it).
type Expirer interface {
	ExpireDeployApprovalRequests(
		pendingMaxAge time.Duration,
		entry func(db.ExpiredDeployApproval) *db.AuditLog,
	) ([]db.ExpiredDeployApproval, []*db.AuditLog, error)
}

// ExpireWorker marks approved deploy requests whose window has passed, and pending requests older than the
// pending max age, as expired, so they stop counting as open requests. Each expiry is audit-logged.
type ExpireWorker struct {
	river.WorkerDefaults[ExpireArgs]
	store         Expirer
	pendingMaxAge time.Duration
}

// NewExpireWorker creates the expiry worker.
func NewExpireWorker(store Expirer, pendingMaxAge time.Duration) *ExpireWorker {
	return &ExpireWorker{store: store, pendingMaxAge: pendingMaxAge}
}

// Work runs one sweep.
func (w *ExpireWorker) Work(_ context.Context, _ *river.Job[ExpireArgs]) error {
	return Expire(w.store, w.pendingMaxAge)
}

// Expire runs one expiry sweep. Each request it expires is audit-logged in the same transaction, so a sweep that
// can't write the audit log expires nothing and fails, loudly. It also runs before a deploy approval request is
// created, so a lapsed request can't block it.
func Expire(store Expirer, pendingMaxAge time.Duration) error {
	expired, events, err := store.ExpireDeployApprovalRequests(pendingMaxAge, audit.DeployApprovalExpiredEntry)
	audit.RecordedInTransaction(events, err)
	if err != nil {
		return fmt.Errorf("expire deploy approvals: %w", err)
	}
	if len(expired) > 0 {
		log.Printf("deploy approvals: expired %d request(s)", len(expired))
	}
	return nil
}
