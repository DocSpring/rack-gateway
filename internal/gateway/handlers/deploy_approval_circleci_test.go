package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/circleci"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// The queued CircleCI approval carries what the worker checks: the deploy approval to re-check, and the
// commit and repository the pipeline must be building.
func TestCircleCIApprovalJobArgsBindTheApproval(t *testing.T) {
	record := &db.DeployApprovalRequest{ID: 42, GitCommitHash: "0123456789abcdef0123456789abcdef01234567"}
	metadata := &circleci.ApprovalMetadata{
		WorkflowID: "11111111-2222-3333-4444-555555555555", PipelineNumber: "100", ApprovalJobName: "approve_deploy_us",
	}

	args := circleCIApprovalJobArgs(record, metadata, "DocSpring/docspring")

	require.Equal(t, int64(42), args.DeployApprovalRequestID)
	require.Equal(t, record.GitCommitHash, args.ExpectedRevision)
	require.Equal(t, "DocSpring/docspring", args.ExpectedRepo)
	require.Equal(t, metadata.WorkflowID, args.WorkflowID)
	require.Equal(t, "approve_deploy_us", args.ApprovalJobName)
}
