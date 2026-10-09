package circleci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Test ApproveJobArgs.Kind
func TestApproveJobArgs_Kind(t *testing.T) {
	args := ApproveJobArgs{
		WorkflowID:              "11111111-2222-3333-4444-555555555555",
		PipelineNumber:          "6445",
		ApprovalJobName:         "hold-for-approval",
		DeployApprovalRequestID: 789,
	}
	assert.Equal(t, "circleci:approve_job", args.Kind())
}

// The CircleCI token must never be persisted in job arguments (they are stored in river_job.args
// and returned by the jobs API).
func TestApproveJobArgs_DoNotContainToken(t *testing.T) {
	encoded, err := json.Marshal(ApproveJobArgs{WorkflowID: "w", ExpectedRevision: "r"})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "token")
}

// Test NewApproveJobWorker
func TestNewApproveJobWorker(t *testing.T) {
	worker := NewApproveJobWorker(nil, "token")
	require.NotNil(t, worker)
}

func TestPipelineMismatchIsNonRetryable(t *testing.T) {
	assert.True(t, isNonRetryableError(fmt.Errorf("wrapped: %w", circleci.ErrPipelineMismatch)))
	assert.True(t, isNonRetryableError(fmt.Errorf("wrapped: %w", circleci.ErrInvalidWorkflowID)))
	assert.False(t, isNonRetryableError(fmt.Errorf("CircleCI API error (502): bad gateway")))
}

type fakeApprovals map[int64]*db.DeployApprovalRequest

func (f fakeApprovals) GetDeployApprovalRequest(id int64) (*db.DeployApprovalRequest, error) {
	if approval, ok := f[id]; ok {
		return approval, nil
	}
	return nil, db.ErrDeployApprovalRequestNotFound
}

// A retry can run hours after the job was queued; if the deploy approval was rejected or has expired by
// then, the job is canceled before CircleCI is called.
func TestApproveJobCancelledWhenApprovalNoLongerActive(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)
	approvals := fakeApprovals{
		1: {PublicID: "rejected", Status: db.DeployApprovalRequestStatusRejected, ApprovalExpiresAt: &future},
		2: {PublicID: "expired", Status: db.DeployApprovalRequestStatusApproved, ApprovalExpiresAt: &past},
		3: {PublicID: "deployed", Status: db.DeployApprovalRequestStatusDeployed, ApprovalExpiresAt: &future},
	}
	worker := NewApproveJobWorker(approvals, "token")

	for _, id := range []int64{1, 2, 3, 404, 0} {
		job := &river.Job[ApproveJobArgs]{
			JobRow: &rivertype.JobRow{},
			Args:   ApproveJobArgs{WorkflowID: "not-a-uuid", DeployApprovalRequestID: id},
		}
		err := worker.Work(context.Background(), job)
		var cancel *river.JobCancelError
		require.Truef(t, errors.As(err, &cancel), "approval %d: want cancel, got %v", id, err)
		require.ErrorIsf(t, err, errApprovalNotActive, "approval %d", id)
	}
}

// Database errors are retried rather than canceling the job.
func TestApproveJobRetriesWhenApprovalLookupFails(t *testing.T) {
	worker := NewApproveJobWorker(failingApprovals{}, "token")
	job := &river.Job[ApproveJobArgs]{JobRow: &rivertype.JobRow{}, Args: ApproveJobArgs{DeployApprovalRequestID: 1}}
	err := worker.Work(context.Background(), job)
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel))
}

type failingApprovals struct{}

func (failingApprovals) GetDeployApprovalRequest(int64) (*db.DeployApprovalRequest, error) {
	return nil, errors.New("connection refused")
}
