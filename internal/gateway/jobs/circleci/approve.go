package circleci

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// ApproveJobArgs contains parameters for CircleCI job approval.
// The CircleCI API token is held by the worker and never stored in job arguments.
type ApproveJobArgs struct {
	WorkflowID              string `json:"workflow_id"`
	PipelineNumber          string `json:"pipeline_number"`
	ApprovalJobName         string `json:"approval_job_name"`
	DeployApprovalRequestID int64  `json:"deploy_approval_request_id"`
	// ExpectedRevision is the approved commit; the workflow's pipeline must be building it.
	ExpectedRevision string `json:"expected_revision"`
	// ExpectedRepo is the app's "owner/repo"; when set, the pipeline must be for this repository.
	ExpectedRepo string `json:"expected_repo,omitempty"`
}

// Kind returns the unique identifier for this job type
func (ApproveJobArgs) Kind() string { return "circleci:approve_job" }

// ApprovalStore loads deploy approval requests.
type ApprovalStore interface {
	GetDeployApprovalRequest(id int64) (*db.DeployApprovalRequest, error)
}

// errApprovalNotActive means the deploy approval was rejected, expired or used since the job was queued.
var errApprovalNotActive = errors.New("deploy approval is no longer active")

// ApproveJobWorker approves CircleCI jobs
type ApproveJobWorker struct {
	river.WorkerDefaults[ApproveJobArgs]
	approvals ApprovalStore
	token     string
	now       func() time.Time
}

// NewApproveJobWorker creates a new CircleCI approve job worker using the given API token.
func NewApproveJobWorker(approvals ApprovalStore, token string) *ApproveJobWorker {
	return &ApproveJobWorker{approvals: approvals, token: token, now: time.Now}
}

// Work approves the CircleCI job after checking the deploy approval is still approved and the
// workflow's pipeline matches it. Retries can run hours after the job was queued, so the approval is
// re-checked on every attempt.
func (w *ApproveJobWorker) Work(_ context.Context, job *river.Job[ApproveJobArgs]) error {
	if strings.TrimSpace(w.token) == "" {
		return river.JobCancel(fmt.Errorf("CircleCI token not configured"))
	}
	args := job.Args
	if err := w.ensureApprovalActive(args.DeployApprovalRequestID); err != nil {
		if errors.Is(err, errApprovalNotActive) {
			return river.JobCancel(err)
		}
		return err
	}
	client := circleci.NewClient(w.token)
	expect := circleci.ApprovalExpectation{Revision: args.ExpectedRevision, Repo: args.ExpectedRepo}

	if err := client.ApproveJob(args.WorkflowID, args.PipelineNumber, args.ApprovalJobName, expect); err != nil {
		// Don't retry if the job is in an invalid state (canceled, already approved, etc.)
		// or the workflow doesn't belong to the approved commit (leave it for a manual approval).
		if isNonRetryableError(err) {
			return river.JobCancel(fmt.Errorf("CircleCI job approval failed (non-retryable): %w", err))
		}
		return fmt.Errorf("failed to approve CircleCI job: %w", err)
	}

	return nil
}

// ensureApprovalActive returns an errApprovalNotActive error unless the deploy approval is approved and
// unexpired. Other errors (e.g. the database being unavailable) are returned as-is so the job retries.
func (w *ApproveJobWorker) ensureApprovalActive(id int64) error {
	if w.approvals == nil {
		return fmt.Errorf("deploy approval store not configured")
	}
	if id == 0 {
		return fmt.Errorf("%w: job has no deploy approval request", errApprovalNotActive)
	}
	approval, err := w.approvals.GetDeployApprovalRequest(id)
	if approval == nil && (err == nil || errors.Is(err, db.ErrDeployApprovalRequestNotFound)) {
		return fmt.Errorf("%w: deploy approval request %d not found", errApprovalNotActive, id)
	}
	if err != nil {
		return fmt.Errorf("failed to load deploy approval request %d: %w", id, err)
	}
	if approval.Status != db.DeployApprovalRequestStatusApproved {
		return fmt.Errorf("%w: deploy approval %s is %s", errApprovalNotActive, approval.PublicID, approval.Status)
	}
	if approval.ApprovalExpiresAt != nil && w.now().After(*approval.ApprovalExpiresAt) {
		return fmt.Errorf("%w: deploy approval %s has expired", errApprovalNotActive, approval.PublicID)
	}
	return nil
}

// isNonRetryableError checks if the error indicates a permanent failure that shouldn't be retried
func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, circleci.ErrPipelineMismatch) || errors.Is(err, circleci.ErrInvalidWorkflowID) {
		return true
	}
	errStr := err.Error()
	// "Invalid approval job state" - job was canceled or already processed
	// "approval job not found" - job doesn't exist anymore
	return strings.Contains(errStr, "Invalid approval job state") ||
		strings.Contains(errStr, "approval job") && strings.Contains(errStr, "not found")
}
