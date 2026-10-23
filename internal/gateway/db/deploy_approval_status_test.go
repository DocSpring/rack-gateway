package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// A request pending longer than DEPLOY_APPROVAL_PENDING_MAX_AGE has expired, so it can't be approved even before
// the expiry sweep marks it.
func TestApprovingARequestPendingTooLongIsRefused(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	request, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.DB().Exec(
		`UPDATE deploy_approval_requests SET created_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, request.ID,
	)
	require.NoError(t, err)

	_, err = database.ApproveDeployApprovalRequest(request.ID, userID, time.Now().Add(time.Hour), "", time.Hour)
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestTooOld)
	stored, err := database.GetDeployApprovalRequest(request.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusPending, stored.Status)

	// Without a limit (or within it) it can be approved.
	approved, err := database.ApproveDeployApprovalRequest(
		request.ID, userID, time.Now().Add(time.Hour), "", 3*time.Hour,
	)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusApproved, approved.Status)
}

// Once a promote has been claimed, the rack may be applying it, and the approval must still say approved when the
// promote's outcome is recorded.
func TestAnApprovalCantBeRejectedOnceItsPromoteStarted(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	request, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(request.ID, userID, time.Now().Add(time.Hour), "", 0)
	require.NoError(t, err)
	claimed, err := database.ClaimDeployApprovalPromote(request.ID)
	require.NoError(t, err)
	require.True(t, claimed)

	_, err = database.RejectDeployApprovalRequest(request.ID, userID, "too late")
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalPromoteStarted)
	stored, err := database.GetDeployApprovalRequest(request.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusApproved, stored.Status)

	// A request that doesn't exist is still not found.
	_, err = database.RejectDeployApprovalRequest(request.ID+1000, userID, "")
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound)
}
