// Package deployapprovals holds background jobs for deploy approval requests.
package deployapprovals

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/riverqueue/river"
)

// ExpireInterval is how often approvals past their window are marked expired.
const ExpireInterval = 5 * time.Minute

// ExpireArgs schedules a sweep that marks approvals past their window as expired.
type ExpireArgs struct{}

// Kind returns the unique identifier for this job type.
func (ExpireArgs) Kind() string { return "deploy_approvals:expire" }

// Expirer marks approvals past their window as expired.
type Expirer interface {
	ExpireDeployApprovalRequests() (int64, error)
}

// ExpireWorker marks approved deploy requests whose window has passed as expired, so their status matches
// what lookups already enforce and they stop counting as open requests.
type ExpireWorker struct {
	river.WorkerDefaults[ExpireArgs]
	store Expirer
}

// NewExpireWorker creates the expiry worker.
func NewExpireWorker(store Expirer) *ExpireWorker {
	return &ExpireWorker{store: store}
}

// Work runs one sweep.
func (w *ExpireWorker) Work(_ context.Context, _ *river.Job[ExpireArgs]) error {
	expired, err := w.store.ExpireDeployApprovalRequests()
	if err != nil {
		return fmt.Errorf("expire deploy approvals: %w", err)
	}
	if expired > 0 {
		log.Printf("deploy approvals: marked %d approval(s) past their window as expired", expired)
	}
	return nil
}
