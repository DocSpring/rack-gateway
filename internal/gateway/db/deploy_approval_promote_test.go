package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Only one promote can hold an approval's claim. A failed promote releases it; any other claim never lapses.
func TestDeployApprovalPromoteClaim(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	approval, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)

	claimed, err := database.ClaimDeployApprovalPromote(approval.ID)
	require.NoError(t, err)
	require.False(t, claimed, "a pending request can't be promoted")

	_, err = database.ApproveDeployApprovalRequest(approval.ID, userID, time.Now().Add(time.Hour), "", 0)
	require.NoError(t, err)

	claimed, err = database.ClaimDeployApprovalPromote(approval.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = database.ClaimDeployApprovalPromote(approval.ID)
	require.NoError(t, err)
	require.False(t, claimed, "a second concurrent promote must not get the claim")

	require.NoError(t, database.ReleaseDeployApprovalPromote(approval.ID))
	claimed, err = database.ClaimDeployApprovalPromote(approval.ID)
	require.NoError(t, err)
	require.True(t, claimed, "a failed promote's claim is released for a retry")

	_, err = database.DB().Exec("UPDATE deploy_approval_requests SET promote_started_at = $1 WHERE id = $2",
		time.Now().Add(-24*time.Hour), approval.ID)
	require.NoError(t, err)
	claimed, err = database.ClaimDeployApprovalPromote(approval.ID)
	require.NoError(t, err)
	require.False(t, claimed, "a promote whose outcome is unknown keeps the claim, however old")

	lapsed, err := createApproval(t, database, userID, tokenID, "other-app")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(lapsed.ID, userID, time.Now().Add(-time.Minute), "", 0)
	require.NoError(t, err)
	claimed, err = database.ClaimDeployApprovalPromote(lapsed.ID)
	require.NoError(t, err)
	require.False(t, claimed, "an approval whose window has passed can't be claimed, even before the expiry sweep")
}
