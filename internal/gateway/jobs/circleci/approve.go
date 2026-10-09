package circleci

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
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

// ApproveJobWorker approves CircleCI jobs
type ApproveJobWorker struct {
	river.WorkerDefaults[ApproveJobArgs]
	token string
}

// NewApproveJobWorker creates a new CircleCI approve job worker using the given API token.
func NewApproveJobWorker(token string) *ApproveJobWorker {
	return &ApproveJobWorker{token: token}
}

// Work approves the CircleCI job after verifying its pipeline matches the deploy approval.
func (w *ApproveJobWorker) Work(_ context.Context, job *river.Job[ApproveJobArgs]) error {
	if strings.TrimSpace(w.token) == "" {
		return river.JobCancel(fmt.Errorf("CircleCI token not configured"))
	}
	args := job.Args
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
