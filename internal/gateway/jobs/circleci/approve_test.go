package circleci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test ApproveJobArgs.Kind
func TestApproveJobArgs_Kind(t *testing.T) {
	args := ApproveJobArgs{
		WorkflowID:              "abc123-workflow-id",
		ApprovalJobName:         "hold-for-approval",
		CircleCIToken:           "circle-token-secret",
		DeployApprovalRequestID: 789,
	}
	assert.Equal(t, "circleci:approve_job", args.Kind())
}

// Test NewApproveJobWorker
func TestNewApproveJobWorker(t *testing.T) {
	worker := NewApproveJobWorker()
	require.NotNil(t, worker)
}
