package circleci

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
)

// ApproveJobArgs contains parameters for CircleCI job approval
type ApproveJobArgs struct {
	WorkflowID              string `json:"workflow_id"`
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
func (w *ApproveJobWorker) Work(_ context.Context, job *river.Job[ApproveJobArgs]) error {
	args := job.Args

	// Create client with token from job args
	client := circleci.NewClient(args.CircleCIToken)

	if err := client.ApproveJob(args.WorkflowID, args.ApprovalJobName); err != nil {
		return fmt.Errorf("failed to approve CircleCI job: %w", err)
	}

	return nil
}
