package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// approvedForClaims creates an approved request for app.
func approvedForClaims(
	t *testing.T, database *gwdb.Database, userID, tokenID int64, app string,
) *gwdb.DeployApprovalRequest {
	t.Helper()
	approval, err := createApproval(t, database, userID, tokenID, app)
	require.NoError(t, err)
	approval, err = database.ApproveDeployApprovalRequest(approval.ID, userID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	return approval
}

// bindArchiveAndBuild takes an approval through its upload and build, as the proxy does.
func bindArchiveAndBuild(t *testing.T, database *gwdb.Database, id int64, objectURL, buildID string) {
	t.Helper()
	claimed, err := database.ClaimDeployApprovalObjectUpload(id, objectURL)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(id, objectURL))
	claimed, err = database.ClaimDeployApprovalBuild(id)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(id, buildID, ""))
}

// An approval's upload is claimed before it's forwarded: a second upload can't claim it while the first is in
// flight, a failed upload releases it, and a finished one stays bound. Its object can't be bound to another
// approval.
func TestDeployApprovalUploadClaim(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	approval := approvedForClaims(t, database, userID, tokenID, "docspring")
	const first, second = "object://docspring/tmp/a.tgz", "object://docspring/tmp/b.tgz"

	claimed, err := database.ClaimDeployApprovalObjectUpload(approval.ID, first)
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = database.ClaimDeployApprovalObjectUpload(approval.ID, second)
	require.NoError(t, err)
	require.False(t, claimed, "a second upload under the approval must not get the claim")
	require.Error(t, database.UpdateDeployApprovalRequestObjectURL(approval.ID, second),
		"a response can't bind an object that wasn't claimed")

	require.NoError(t, database.ReleaseDeployApprovalObjectUpload(approval.ID, first))
	claimed, err = database.ClaimDeployApprovalObjectUpload(approval.ID, second)
	require.NoError(t, err)
	require.True(t, claimed, "a failed upload's claim is released for a retry")

	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(approval.ID, second))
	require.NoError(t, database.ReleaseDeployApprovalObjectUpload(approval.ID, second))
	reloaded, err := database.GetDeployApprovalRequest(approval.ID)
	require.NoError(t, err)
	require.Equal(t, second, reloaded.ObjectURL, "a finished upload isn't released")

	other := approvedForClaims(t, database, userID, tokenID, "api-proxy")
	_, err = database.ClaimDeployApprovalObjectUpload(other.ID, second)
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalObjectBound)

	lapsed := approvedForClaims(t, database, userID, tokenID, "worker")
	require.NoError(t, database.SetDeployApprovalExpiryForTest(lapsed.ID, time.Now().Add(-time.Minute)))
	claimed, err = database.ClaimDeployApprovalObjectUpload(lapsed.ID, "object://worker/tmp/c.tgz")
	require.NoError(t, err)
	require.False(t, claimed, "an approval whose window has passed can't be claimed")
}

// An approval creates one build, and only from an archive whose upload finished. A failed build releases the claim;
// a recorded one keeps it.
func TestDeployApprovalBuildClaim(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	approval := approvedForClaims(t, database, userID, tokenID, "docspring")
	const objectURL = "object://docspring/tmp/a.tgz"

	claimed, err := database.ClaimDeployApprovalObjectUpload(approval.ID, objectURL)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = database.ClaimDeployApprovalBuild(approval.ID)
	require.NoError(t, err)
	require.False(t, claimed, "the archive is still uploading")

	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(approval.ID, objectURL))
	claimed, err = database.ClaimDeployApprovalBuild(approval.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = database.ClaimDeployApprovalBuild(approval.ID)
	require.NoError(t, err)
	require.False(t, claimed, "a second concurrent build must not get the claim")

	require.NoError(t, database.ReleaseDeployApprovalBuild(approval.ID))
	claimed, err = database.ClaimDeployApprovalBuild(approval.ID)
	require.NoError(t, err)
	require.True(t, claimed, "a failed build's claim is released for a retry")

	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "B1", ""))
	require.NoError(t, database.ReleaseDeployApprovalBuild(approval.ID))
	claimed, err = database.ClaimDeployApprovalBuild(approval.ID)
	require.NoError(t, err)
	require.False(t, claimed, "a recorded build keeps the approval's one build")
	require.Error(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "B2", ""),
		"a second build can't be recorded")
}

// A build's release is recorded once, from the approval's own build, and never replaced.
func TestDeployApprovalReleaseIsNeverReplaced(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)
	approval := approvedForClaims(t, database, userID, tokenID, "docspring")
	bindArchiveAndBuild(t, database, approval.ID, "object://docspring/tmp/a.tgz", "B1")

	require.Error(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "B2", "R2"),
		"another build's release must not bind to the approval")
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "B1", "R1"))
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "B1", "R1"), "a re-read is a no-op")
	require.Error(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "B1", "R9"))

	reloaded, err := database.GetDeployApprovalRequest(approval.ID)
	require.NoError(t, err)
	require.Equal(t, "B1", reloaded.BuildID)
	require.Equal(t, "R1", reloaded.ReleaseID)
}
