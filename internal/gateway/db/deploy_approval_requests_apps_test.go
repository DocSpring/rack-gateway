package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const appsTestCommit = "0123456789abcdef0123456789abcdef01234567"

func newApprovalTestFixture(t *testing.T) (*gwdb.Database, int64, int64) {
	t.Helper()
	database := dbtest.NewDatabase(t)
	user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)
	token, err := database.CreateAPIToken(strings.Repeat("f", 64), "ci-token", user.ID, nil, nil, nil)
	require.NoError(t, err)
	return database, user.ID, token.ID
}

func createApproval(t *testing.T, database *gwdb.Database, userID, tokenID int64, app string) (
	*gwdb.DeployApprovalRequest, error,
) {
	t.Helper()
	return database.CreateDeployApprovalRequest(
		"Deploy "+app, app, appsTestCommit, "feature", "", nil, userID, nil, tokenID,
	)
}

// CI deploys several apps for one commit with the same token; each app gets its own open request.
func TestOpenDeployApprovalRequestsAreUniquePerApp(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	docspring, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	apiProxy, err := createApproval(t, database, userID, tokenID, "api-proxy")
	require.NoError(t, err, "a second app for the same commit and token must get its own request")
	require.NotEqual(t, docspring.ID, apiProxy.ID)
	require.Equal(t, "api-proxy", apiProxy.App)

	// A duplicate for the same app still returns that app's existing request.
	_, err = createApproval(t, database, userID, tokenID, "api-proxy")
	var conflict *gwdb.DeployApprovalRequestConflictError
	require.True(t, errors.As(err, &conflict), "expected a conflict, got %v", err)
	require.Equal(t, apiProxy.ID, conflict.Request.ID)

	// Also once the existing request is approved.
	_, err = database.ApproveDeployApprovalRequest(docspring.ID, userID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	_, err = createApproval(t, database, userID, tokenID, "docspring")
	require.True(t, errors.As(err, &conflict), "expected a conflict, got %v", err)
	require.Equal(t, docspring.ID, conflict.Request.ID)
}

// Once the sweep expires an approval past its window, it no longer blocks a new request for the same app. (The
// create handler runs the sweep first.)
func TestExpiredApprovalDoesNotBlockNewRequest(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	stale, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(stale.ID, userID, time.Now().Add(-time.Minute), "")
	require.NoError(t, err)
	_, err = database.ExpireDeployApprovalRequests(0)
	require.NoError(t, err)

	fresh, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	require.NotEqual(t, stale.ID, fresh.ID)

	reloaded, err := database.GetDeployApprovalRequest(stale.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusExpired, reloaded.Status)
}

func TestExpireDeployApprovalRequestsOnlyExpiresPastWindows(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	stale, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	live, err := createApproval(t, database, userID, tokenID, "api-proxy")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(stale.ID, userID, time.Now().Add(-time.Minute), "")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(live.ID, userID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)

	expired, err := database.ExpireDeployApprovalRequests(0)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, gwdb.ExpiredDeployApproval{
		ID: stale.ID, PublicID: stale.PublicID, App: "docspring", GitCommitHash: stale.GitCommitHash,
		PreviousStatus: gwdb.DeployApprovalRequestStatusApproved,
	}, expired[0])

	for id, want := range map[int64]string{
		stale.ID: gwdb.DeployApprovalRequestStatusExpired,
		live.ID:  gwdb.DeployApprovalRequestStatusApproved,
	} {
		reloaded, err := database.GetDeployApprovalRequest(id)
		require.NoError(t, err)
		require.Equal(t, want, reloaded.Status)
	}
}

// Extending a lapsed (expired) approval approves it again, unless another request for the app has opened since.
func TestExtendRevivesExpiredApprovalUnlessAnotherIsOpen(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	lapsed, err := createApproval(t, database, userID, tokenID, "api-proxy")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(lapsed.ID, userID, time.Now().Add(-time.Minute), "")
	require.NoError(t, err)
	_, err = database.ExpireDeployApprovalRequests(0)
	require.NoError(t, err)
	extended, err := database.ExtendDeployApprovalRequestExpiry(lapsed.PublicID, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusApproved, extended.Status)

	// A lapsed approval with a newer open request for the same app can't be revived: that would make two.
	old, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(old.ID, userID, time.Now().Add(-time.Minute), "")
	require.NoError(t, err)
	_, err = database.ExpireDeployApprovalRequests(0)
	require.NoError(t, err)
	_, err = createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ExtendDeployApprovalRequestExpiry(old.PublicID, time.Now().Add(time.Hour))
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestActive)
}

// A promote authorized while the approval was open can finish after the window lapsed (and the sweep marked it
// expired); the approval still records that it was deployed.
func TestApprovalExpiredDuringPromoteIsMarkedDeployed(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	approval, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(approval.ID, userID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	bindArchiveAndBuild(t, database, approval.ID, "object://docspring/tmp/a.tgz", "B1")
	require.NoError(t, database.MarkDeployApprovalRequestBuildStarted(approval.ID, "B1", "R1"))

	// The window lapses while the promote runs.
	require.NoError(t, database.SetDeployApprovalExpiryForTest(approval.ID, time.Now().Add(-time.Second)))
	_, err = database.ExpireDeployApprovalRequests(0)
	require.NoError(t, err)

	require.NoError(t, database.MarkDeployApprovalAsDeployed(approval.ID))
	reloaded, err := database.GetDeployApprovalRequest(approval.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusDeployed, reloaded.Status)
}

// Pending requests older than the max age expire; newer ones and approvals inside their window don't.
func TestExpireDeployApprovalRequestsExpiresOldPendingRequests(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	old, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	fresh, err := createApproval(t, database, userID, tokenID, "api-proxy")
	require.NoError(t, err)
	require.NoError(t, database.SetDeployApprovalCreatedAtForTest(old.ID, time.Now().Add(-8*24*time.Hour)))

	none, err := database.ExpireDeployApprovalRequests(0)
	require.NoError(t, err)
	require.Empty(t, none, "pending requests never expire without a max age")

	expired, err := database.ExpireDeployApprovalRequests(7 * 24 * time.Hour)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, old.ID, expired[0].ID)
	require.Equal(t, gwdb.DeployApprovalRequestStatusPending, expired[0].PreviousStatus)

	reloaded, err := database.GetDeployApprovalRequest(fresh.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusPending, reloaded.Status)
}

// Extend only revives approvals: a request that expired while pending was never approved.
func TestExtendRefusesExpiredPendingRequest(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	pending, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	require.NoError(t, database.SetDeployApprovalCreatedAtForTest(pending.ID, time.Now().Add(-8*24*time.Hour)))
	_, err = database.ExpireDeployApprovalRequests(7 * 24 * time.Hour)
	require.NoError(t, err)

	_, err = database.ExtendDeployApprovalRequestExpiry(pending.PublicID, time.Now().Add(time.Hour))
	require.ErrorIs(t, err, gwdb.ErrDeployApprovalRequestNotFound)
	reloaded, err := database.GetDeployApprovalRequest(pending.ID)
	require.NoError(t, err)
	require.Equal(t, gwdb.DeployApprovalRequestStatusExpired, reloaded.Status)
}
