package circleci

import (
	"context"
	"fmt"
	"strings"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
)

// ApproveJobArgs contains parameters for CircleCI job approval
type ApproveJobArgs struct {
	WorkflowID              string `json:"workflow_id"`
	PipelineNumber          string `json:"pipeline_number"`
	ApprovalJobName         string `json:"approval_job_name"`
	CircleCIToken           string `json:"circleci_token"`
	DeployApprovalRequestID int64  `json:"deploy_approval_request_id"`
}

// Kind returns the unique identifier for this job type
func (ApproveJobArgs) Kind() string { return "circleci:approve_job" }

// ApproveJobWorker approves CircleCI jobs
type ApproveJobWorker struct {
	river.WorkerDefaults[ApproveJobArgs]
}

// NewApproveJobWorker creates a new CircleCI approve job worker
func NewApproveJobWorker() *ApproveJobWorker {
	return &ApproveJobWorker{}
}

// Work approves the CircleCI job
func (_ *ApproveJobWorker) Work(_ context.Context, job *river.Job[ApproveJobArgs]) error {
	args := job.Args

	// Create client with token from job args
	client := circleci.NewClient(args.CircleCIToken)

	if err := client.ApproveJob(args.WorkflowID, args.PipelineNumber, args.ApprovalJobName); err != nil {
		// Don't retry if the job is in an invalid state (canceled, already approved, etc.)
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
	errStr := err.Error()
	// "Invalid approval job state" - job was canceled or already processed
	// "approval job not found" - job doesn't exist anymore
	return strings.Contains(errStr, "Invalid approval job state") ||
		strings.Contains(errStr, "approval job") && strings.Contains(errStr, "not found")
}
