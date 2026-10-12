package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// expiryEvent is a minimal audit event for an expired request.
func expiryEvent(e gwdb.ExpiredDeployApproval) *gwdb.AuditLog {
	return &gwdb.AuditLog{
		UserEmail:    "system",
		ActionType:   "gateway",
		Action:       "deploy_approval_request.expire",
		ResourceType: "deploy_approval_request",
		Resource:     fmt.Sprintf("%d", e.ID),
		Status:       "success",
		RBACDecision: "allow",
		EventCount:   1,
	}
}

func approvedPastItsWindow(t *testing.T) (*gwdb.Database, *gwdb.DeployApprovalRequest) {
	t.Helper()
	database, userID, tokenID := newApprovalTestFixture(t)
	request, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(request.ID, userID, time.Now().Add(-time.Minute), "")
	require.NoError(t, err)
	return database, request
}

// Each expiry's audit event is appended to the chain, and returned with its stored values.
func TestExpireDeployApprovalRequestsAuditsEachExpiry(t *testing.T) {
	database, request := approvedPastItsWindow(t)

	expired, events, err := database.ExpireDeployApprovalRequests(0, expiryEvent)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Len(t, events, 1)
	require.NotZero(t, events[0].ID)

	logs, err := database.GetAuditLogs("system", time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "deploy_approval_request.expire", logs[0].Action)
	require.Equal(t, fmt.Sprintf("%d", request.ID), logs[0].Resource)
	require.Equal(t, events[0].ID, logs[0].ID)
}

// A request is never expired without its audit event: when the event can't be appended, nothing is expired.
func TestExpireDeployApprovalRequestsExpiresNothingWithoutItsAuditEvent(t *testing.T) {
	database, request := approvedPastItsWindow(t)
	_, err := database.DB().Exec(`ALTER FUNCTION audit.append_audit_event RENAME TO append_audit_event_unavailable`)
	require.NoError(t, err)

	expired, events, err := database.ExpireDeployApprovalRequests(0, expiryEvent)
	require.ErrorIs(t, err, gwdb.ErrAuditAppend)
	require.Empty(t, expired)
	require.Empty(t, events)

	reloaded, err := database.GetDeployApprovalRequest(request.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusApproved, reloaded.Status)
}
