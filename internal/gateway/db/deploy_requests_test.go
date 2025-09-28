package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func createAPITokenHelper(t *testing.T, database *db.Database, userID int64) *db.APIToken {
	t.Helper()
	hash := strings.Repeat("a", 64)
	perms := []string{"convox:build:create-with-approval", "convox:object:create-with-approval", "convox:release:create-with-approval", "convox:release:promote-with-approval"}
	token, err := database.CreateAPIToken(hash, "ci-token", userID, perms, nil, nil)
	require.NoError(t, err)
	return token
}

func TestDeployRequestLifecycle(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	approver, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	token := createAPITokenHelper(t, database, user.ID)

	req, err := database.CreateDeployRequest("production", "Deploy release", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployRequestStatusPending, req.Status)

	approved, err := database.ApproveDeployRequest(req.ID, approver.ID, time.Now().Add(15*time.Minute), "approved")
	require.NoError(t, err)
	require.Equal(t, db.DeployRequestStatusApproved, approved.Status)

	active, err := database.ActiveDeployRequestForStage(token.ID, "production", db.DeployRequestStageObject, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployRequestObjectUsed(req.ID, "obj-key", time.Now()))

	active, err = database.ActiveDeployRequestForStage(token.ID, "production", db.DeployRequestStageBuild, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployRequestBuildUsed(req.ID, "B123", time.Now()))

	active, err = database.ActiveDeployRequestForStage(token.ID, "production", db.DeployRequestStageRelease, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployRequestReleaseCreated(req.ID, "R123", time.Now()))

	active, err = database.ActiveDeployRequestForStage(token.ID, "production", db.DeployRequestStagePromote, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployRequestPromoted(req.ID, "R123", token.ID, time.Now()))

	final, err := database.GetDeployRequest(req.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployRequestStatusConsumed, final.Status)
	require.NotNil(t, final.ReleasePromotedAt)
}

func TestDeployRequestDuplicateGuard(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)

	_, err = database.CreateDeployRequest("production", "Deploy branch", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	_, err = database.CreateDeployRequest("production", "Deploy branch", user.ID, nil, token.ID, &user.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, db.ErrDeployRequestActive)

	_, err = database.CreateDeployRequest("production", "Deploy another branch", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)
}

func TestDeployRequestExpiration(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)

	req, err := database.CreateDeployRequest("production", "Deploy release", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	_, err = database.ApproveDeployRequest(req.ID, user.ID, time.Now().Add(-1*time.Minute), "expired")
	require.NoError(t, err)

	_, err = database.ActiveDeployRequestForStage(token.ID, "production", db.DeployRequestStageBuild, 15*time.Minute)
	require.Error(t, err)
	require.ErrorIs(t, err, db.ErrDeployRequestExpired)

	updated, err := database.GetDeployRequest(req.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployRequestStatusConsumed, updated.Status)
}

func TestDeployRequestReject(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("user@example.com", "User", []string{"deployer"})
	require.NoError(t, err)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)

	req, err := database.CreateDeployRequest("production", "Deploy", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	rejected, err := database.RejectDeployRequest(req.ID, admin.ID, "not today")
	require.NoError(t, err)
	require.Equal(t, db.DeployRequestStatusRejected, rejected.Status)
	require.Equal(t, "not today", strings.TrimSpace(rejected.ApprovalNotes))
}
