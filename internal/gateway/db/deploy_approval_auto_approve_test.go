package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// autoApprovalEvent is a minimal audit event for an auto-approved request.
func autoApprovalEvent(approved *gwdb.DeployApprovalRequest) *gwdb.AuditLog {
	return &gwdb.AuditLog{
		UserEmail:    "system",
		ActionType:   "gateway",
		Action:       "deploy_approval_request.approve",
		ResourceType: "deploy_approval_request",
		Resource:     fmt.Sprintf("%d", approved.ID),
		Details:      fmt.Sprintf(`{"public_id": %q, "app": %q}`, approved.PublicID, approved.App),
		Status:       "success",
		RBACDecision: "allow",
		EventCount:   1,
	}
}

// The approval and its audit event are written together: the event is built from the approved request, with no
// approving user, and returned as stored.
func TestAutoApproveDeployApprovalRequestAuditsTheApproval(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	request, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	expiresAt := time.Now().Add(15 * time.Minute)

	approved, event, err := database.AutoApproveDeployApprovalRequest(request.ID, expiresAt, "auto", autoApprovalEvent)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusApproved, approved.Status)
	require.Nil(t, approved.ApprovedByUserID)
	require.Equal(t, "auto", approved.ApprovalNotes)
	require.WithinDuration(t, expiresAt, *approved.ApprovalExpiresAt, time.Second)
	require.NotZero(t, event.ID)
	require.Contains(t, event.Details, request.PublicID)

	logs, err := database.GetAuditLogs("system", time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, event.ID, logs[0].ID)

	_, _, err = database.AutoApproveDeployApprovalRequest(request.ID, expiresAt, "auto", autoApprovalEvent)
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound, "only a pending request is approved")
}

// Without its audit event there's no approval: the request stays pending.
func TestAutoApproveDeployApprovalRequestApprovesNothingWithoutItsAuditEvent(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	request, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.DB().Exec(`ALTER FUNCTION audit.append_audit_event RENAME TO append_audit_event_unavailable`)
	require.NoError(t, err)

	approved, event, err := database.AutoApproveDeployApprovalRequest(
		request.ID, time.Now().Add(time.Hour), "auto", autoApprovalEvent)
	require.ErrorIs(t, err, gwdb.ErrAuditAppend)
	require.Nil(t, approved)
	require.Nil(t, event)

	reloaded, err := database.GetDeployApprovalRequest(request.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusPending, reloaded.Status)
	require.Nil(t, reloaded.ApprovedAt)
}
