package circleci

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// ApproveJobArgs contains parameters for CircleCI job approval.
// The CircleCI API token is held by the worker and never stored in job arguments.
type ApproveJobArgs struct {
	WorkflowID string `json:"workflow_id"`
	// PipelineNumber is the pipeline the requester named and the approver was shown; the workflow must be in it.
	PipelineNumber          string `json:"pipeline_number"`
	ApprovalJobName         string `json:"approval_job_name"`
	DeployApprovalRequestID int64  `json:"deploy_approval_request_id"`
	// ExpectedRevision is the approved commit; the workflow's pipeline must be building it.
	ExpectedRevision string `json:"expected_revision"`
	// ExpectedRepo is the app's repository as "host/owner/repo" (circleci.RepoIdentity); the pipeline must be for
	// this repository. A job without one approves nothing.
	ExpectedRepo string `json:"expected_repo"`
}

// Kind returns the unique identifier for this job type
func (ApproveJobArgs) Kind() string { return "circleci:approve_job" }

// ApprovalStore loads deploy approval requests.
type ApprovalStore interface {
	GetDeployApprovalRequest(id int64) (*db.DeployApprovalRequest, error)
}

// AuditLog records the worker's approvals in the hash-chained audit log (audit.Logger).
type AuditLog interface {
	RecordSystemAttempt(entry *db.AuditLog) error
	LogDBEntry(entry *db.AuditLog) error
}

// errApprovalNotActive means the deploy approval was rejected, expired or used since the job was queued.
var errApprovalNotActive = errors.New("deploy approval is no longer active")

// ApproveJobWorker approves CircleCI jobs
type ApproveJobWorker struct {
	river.WorkerDefaults[ApproveJobArgs]
	approvals ApprovalStore
	auditLog  AuditLog
	token     string
	now       func() time.Time
	// baseURL overrides the CircleCI API root (tests).
	baseURL string
}

// NewApproveJobWorker creates a new CircleCI approve job worker using the given API token.
func NewApproveJobWorker(approvals ApprovalStore, token string, auditLog AuditLog) *ApproveJobWorker {
	return &ApproveJobWorker{approvals: approvals, auditLog: auditLog, token: token, now: time.Now}
}

// Work approves the CircleCI job after checking the deploy approval is still approved and the workflow's pipeline
// matches it. Retries can run hours after the job was queued, so the approval is re-checked on every attempt, and
// again just before the hold job is approved: checking CircleCI takes several requests, during which the approval
// can be rejected or expire. The approval is written to the audit log before it's sent, with the hold job,
// workflow and pipeline it approves, and nothing is approved when that entry can't be written.
func (w *ApproveJobWorker) Work(_ context.Context, job *river.Job[ApproveJobArgs]) error {
	if strings.TrimSpace(w.token) == "" {
		return river.JobCancel(fmt.Errorf("CircleCI token not configured"))
	}
	args := job.Args
	if _, err := w.activeApproval(args.DeployApprovalRequestID); err != nil {
		return approvalLookupError(err)
	}
	client := circleci.NewClient(w.token)
	if w.baseURL != "" {
		client.BaseURL = w.baseURL
	}
	target, err := client.PrepareApproval(args.WorkflowID, args.ApprovalJobName, circleci.ApprovalExpectation{
		Revision: args.ExpectedRevision, Repo: args.ExpectedRepo, PipelineNumber: args.PipelineNumber,
	})
	if err != nil {
		return approvalError(err)
	}

	approval, err := w.activeApproval(args.DeployApprovalRequestID)
	if err != nil {
		return approvalLookupError(err)
	}
	attempt := w.approvalEntry(approval, args, target)
	if err := w.auditLog.RecordSystemAttempt(attempt); err != nil {
		return fmt.Errorf("not approving the CircleCI job without an audit entry: %w", err)
	}
	approveErr := client.Approve(target)
	result := w.approvalEntry(approval, args, target)
	result.RequestID = attempt.RequestID
	if approveErr != nil {
		result.Status = audit.StatusFailed
		result.Details = audit.WithDetails(result.Details, map[string]interface{}{"error": approveErr.Error()})
	}
	// The approval has been sent either way; a failed write is reported by the logger, and the attempt records it.
	_ = w.auditLog.LogDBEntry(result)
	if approveErr != nil {
		return approvalError(approveErr)
	}
	return nil
}

func (_ *ApproveJobWorker) approvalEntry(
	approval *db.DeployApprovalRequest,
	args ApproveJobArgs,
	target *circleci.ApprovalTarget,
) *db.AuditLog {
	return audit.CircleCIApprovalEntry(approval.ID, approval.PublicID, map[string]interface{}{
		"app":                approval.App,
		"git_commit_hash":    approval.GitCommitHash,
		"requested_workflow": args.WorkflowID,
		"workflow_id":        target.WorkflowID,
		"job_id":             target.JobID,
		"job_name":           args.ApprovalJobName,
		"pipeline_id":        target.PipelineID,
		"pipeline_number":    target.PipelineNumber,
		"repository":         args.ExpectedRepo,
	})
}

// approvalLookupError cancels the job when the deploy approval is no longer active, and retries it on any other
// error (e.g. the database being unavailable).
func approvalLookupError(err error) error {
	if errors.Is(err, errApprovalNotActive) {
		return river.JobCancel(err)
	}
	return err
}

// approvalError cancels the job when CircleCI's answer won't change on a retry: the job is in an invalid state
// (canceled, already approved) or the workflow doesn't belong to the approved commit, pipeline and repository (left
// for a manual approval). Any other error is retried.
func approvalError(err error) error {
	if isNonRetryableError(err) {
		return river.JobCancel(fmt.Errorf("CircleCI job approval failed (non-retryable): %w", err))
	}
	return fmt.Errorf("failed to approve CircleCI job: %w", err)
}

// activeApproval returns the deploy approval, or an errApprovalNotActive error unless it's approved and unexpired.
// Other errors (e.g. the database being unavailable) are returned as-is so the job retries.
func (w *ApproveJobWorker) activeApproval(id int64) (*db.DeployApprovalRequest, error) {
	if w.approvals == nil {
		return nil, fmt.Errorf("deploy approval store not configured")
	}
	if id == 0 {
		return nil, fmt.Errorf("%w: job has no deploy approval request", errApprovalNotActive)
	}
	approval, err := w.approvals.GetDeployApprovalRequest(id)
	if approval == nil && (err == nil || errors.Is(err, db.ErrDeployApprovalRequestNotFound)) {
		return nil, fmt.Errorf("%w: deploy approval request %d not found", errApprovalNotActive, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load deploy approval request %d: %w", id, err)
	}
	if approval.Status != db.DeployApprovalRequestStatusApproved {
		return nil, fmt.Errorf("%w: deploy approval %s is %s", errApprovalNotActive, approval.PublicID, approval.Status)
	}
	if approval.ApprovalExpiresAt != nil && w.now().After(*approval.ApprovalExpiresAt) {
		return nil, fmt.Errorf("%w: deploy approval %s has expired", errApprovalNotActive, approval.PublicID)
	}
	return approval, nil
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
