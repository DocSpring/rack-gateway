package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func createAPITokenHelper(t *testing.T, database *db.Database, userID int64) *db.APIToken {
	t.Helper()
	hash := strings.Repeat("a", 64)
	perms := []string{"convox:build:create", "convox:object:create", "convox:release:create", "convox:release:promote-with-approval"}
	token, err := database.CreateAPIToken(hash, "ci-token", userID, perms, nil, nil)
	require.NoError(t, err)
	return token
}

func TestDeployApprovalRequestLifecycle(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	approver, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	token := createAPITokenHelper(t, database, user.ID)
	app := "my-app"
	releaseID := "R123"

	// Create approval request for specific release
	req, err := database.CreateDeployApprovalRequest("Deploy release R123", app, releaseID, user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusPending, req.Status)
	require.Equal(t, app, req.App)
	require.Equal(t, releaseID, req.ReleaseID)

	// Approve the request
	approved, err := database.ApproveDeployApprovalRequest(req.ID, approver.ID, time.Now().Add(15*time.Minute), "approved")
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusApproved, approved.Status)

	// Check approval exists for (app, token, release) triple
	active, err := database.ActiveDeployApprovalRequestByTokenAndRelease(token.ID, app, releaseID)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)
	require.Equal(t, app, active.App)
	require.Equal(t, releaseID, active.ReleaseID)

	// Mark as promoted
	require.NoError(t, database.MarkDeployApprovalRequestPromoted(req.ID, app, releaseID, token.ID, time.Now()))

	// Verify final status
	final, err := database.GetDeployApprovalRequest(req.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusConsumed, final.Status)
	require.NotNil(t, final.ReleasePromotedAt)
	require.Equal(t, app, final.App)
	require.Equal(t, releaseID, final.ReleaseID)
}

func TestDeployApprovalRequestDuplicateGuard(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)
	app := "my-app"
	releaseID := "R123"

	// First request should succeed
	_, err = database.CreateDeployApprovalRequest("Deploy release", app, releaseID, user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	// Duplicate request for same (app, token, release) should fail
	_, err = database.CreateDeployApprovalRequest("Deploy release again", app, releaseID, user.ID, nil, token.ID, &user.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, db.ErrDeployApprovalRequestActive)

	// Request for different release should succeed
	_, err = database.CreateDeployApprovalRequest("Deploy different release", app, "R456", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	// Request for different app should succeed
	_, err = database.CreateDeployApprovalRequest("Deploy to different app", "other-app", releaseID, user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)
}

func TestDeployApprovalRequestExpiration(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)
	app := "my-app"
	releaseID := "R123"

	req, err := database.CreateDeployApprovalRequest("Deploy release", app, releaseID, user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	// Approve with expiration in the past
	_, err = database.ApproveDeployApprovalRequest(req.ID, user.ID, time.Now().Add(-1*time.Minute), "expired")
	require.NoError(t, err)

	// Should not find active approval (expired)
	_, err = database.ActiveDeployApprovalRequestByTokenAndRelease(token.ID, app, releaseID)
	require.Error(t, err)
	require.ErrorIs(t, err, db.ErrDeployApprovalRequestNotFound)
}

func TestDeployApprovalRequestReject(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("user@example.com", "User", []string{"deployer"})
	require.NoError(t, err)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)

	req, err := database.CreateDeployApprovalRequest("Deploy", "my-app", "R123", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	rejected, err := database.RejectDeployApprovalRequest(req.ID, admin.ID, "not today")
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusRejected, rejected.Status)
	require.Equal(t, "not today", strings.TrimSpace(rejected.ApprovalNotes))
}
