// Package deployapprovals holds background jobs for deploy approval requests.
package deployapprovals

import (
	"context"
	"errors"
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

// Expirer expires deploy approval requests.
type Expirer interface {
	ExpireDeployApprovalRequests(pendingMaxAge time.Duration) ([]db.ExpiredDeployApproval, error)
}

// AuditFunc writes an audit event.
type AuditFunc func(*db.AuditLog) error

// ExpireWorker marks approved deploy requests whose window has passed, and pending requests older than the
// pending max age, as expired, so they stop counting as open requests. Each expiry is audit-logged.
type ExpireWorker struct {
	river.WorkerDefaults[ExpireArgs]
	store         Expirer
	pendingMaxAge time.Duration
	audit         AuditFunc
}

// NewExpireWorker creates the expiry worker.
func NewExpireWorker(store Expirer, pendingMaxAge time.Duration, auditFn AuditFunc) *ExpireWorker {
	return &ExpireWorker{store: store, pendingMaxAge: pendingMaxAge, audit: auditFn}
}

// Work runs one sweep.
func (w *ExpireWorker) Work(_ context.Context, _ *river.Job[ExpireArgs]) error {
	return Expire(w.store, w.pendingMaxAge, w.audit)
}

// Expire runs one expiry sweep and audits each request it expires. It also runs before a deploy approval request
// is created, so a lapsed request can't block it.
func Expire(store Expirer, pendingMaxAge time.Duration, auditFn AuditFunc) error {
	expired, err := store.ExpireDeployApprovalRequests(pendingMaxAge)
	if err != nil {
		return fmt.Errorf("expire deploy approvals: %w", err)
	}
	if len(expired) > 0 {
		log.Printf("deploy approvals: expired %d request(s)", len(expired))
	}
	// The requests are already expired, so a retry wouldn't audit them: write every event, then report failures.
	var auditErrs []error
	for _, e := range expired {
		if err := auditFn(audit.DeployApprovalExpiredEntry(e)); err != nil {
			auditErrs = append(auditErrs, fmt.Errorf("audit expired deploy approval %d: %w", e.ID, err))
		}
	}
	return errors.Join(auditErrs...)
}
