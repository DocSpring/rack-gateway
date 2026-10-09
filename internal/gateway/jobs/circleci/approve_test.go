package circleci

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
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
	worker := NewApproveJobWorker("token")
	require.NotNil(t, worker)
}

func TestPipelineMismatchIsNonRetryable(t *testing.T) {
	assert.True(t, isNonRetryableError(fmt.Errorf("wrapped: %w", circleci.ErrPipelineMismatch)))
	assert.True(t, isNonRetryableError(fmt.Errorf("wrapped: %w", circleci.ErrInvalidWorkflowID)))
	assert.False(t, isNonRetryableError(fmt.Errorf("CircleCI API error (502): bad gateway")))
}
