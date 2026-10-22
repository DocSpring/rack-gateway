package db_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// A deploy approval request's audit history pages through every entry about it, including its lifecycle entries
// from before they set deploy_approval_request_id, which name it only as their resource.
func TestDeployApprovalAuditLogsPageThroughEverythingAboutTheRequest(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	approval, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	other, err := createApproval(t, database, userID, tokenID, "api-proxy")
	require.NoError(t, err)
	id := strconv.FormatInt(approval.ID, 10)

	entry := func(action, resourceType, resource string, approvalID *int64) {
		t.Helper()
		require.NoError(t, database.CreateAuditLog(&gwdb.AuditLog{
			UserEmail: "deployer@example.com", ActionType: "gateway", Action: action,
			ResourceType: resourceType, Resource: resource, Status: "success",
			DeployApprovalRequestID: approvalID,
		}))
	}
	entry("deploy_approval_request.create", "deploy_approval_request", id, nil)
	entry("deploy_approval_request.approve", "deploy-approval-request", id, nil)
	entry("build.create", "build", "B1", &approval.ID)
	entry("deploy_approval_request.reject", "deploy_approval_request", id, &approval.ID)
	// Not about it: another request's entries, and another resource that happens to have the same name.
	entry("build.create", "build", "B2", &other.ID)
	entry("app.read", "app", id, nil)

	var actions []string
	for page := range 3 {
		logs, total, err := database.GetDeployApprovalAuditLogsPaged(approval.ID, 2, page*2)
		require.NoError(t, err)
		require.Equal(t, 4, total)
		for _, log := range logs {
			actions = append(actions, log.Action)
		}
	}
	require.ElementsMatch(t, []string{
		"deploy_approval_request.create", "deploy_approval_request.approve", "build.create",
		"deploy_approval_request.reject",
	}, actions)
}
