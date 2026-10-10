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

// An approval past its window is expired and no longer blocks a new request for the same app.
func TestExpiredApprovalDoesNotBlockNewRequest(t *testing.T) {
	database, userID, tokenID := newApprovalTestFixture(t)

	stale, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(stale.ID, userID, time.Now().Add(-time.Minute), "")
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

	// Create both first: creating a request also runs the expiry sweep.
	stale, err := createApproval(t, database, userID, tokenID, "docspring")
	require.NoError(t, err)
	live, err := createApproval(t, database, userID, tokenID, "api-proxy")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(stale.ID, userID, time.Now().Add(-time.Minute), "")
	require.NoError(t, err)
	_, err = database.ApproveDeployApprovalRequest(live.ID, userID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)

	expired, err := database.ExpireDeployApprovalRequests()
	require.NoError(t, err)
	require.EqualValues(t, 1, expired)

	for id, want := range map[int64]string{
		stale.ID: gwdb.DeployApprovalRequestStatusExpired,
		live.ID:  gwdb.DeployApprovalRequestStatusApproved,
	} {
		reloaded, err := database.GetDeployApprovalRequest(id)
		require.NoError(t, err)
		require.Equal(t, want, reloaded.Status)
	}
}
