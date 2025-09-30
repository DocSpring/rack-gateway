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

func TestDeployApprovalRequestLifecycle(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	approver, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	token := createAPITokenHelper(t, database, user.ID)

	req, err := database.CreateDeployApprovalRequest("production", "Deploy release", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusPending, req.Status)

	approved, err := database.ApproveDeployApprovalRequest(req.ID, approver.ID, time.Now().Add(15*time.Minute), "approved")
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusApproved, approved.Status)

	active, err := database.ActiveDeployApprovalRequestForStage(token.ID, "production", db.DeployApprovalRequestStageObject, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployApprovalRequestObjectUsed(req.ID, "obj-key", time.Now()))

	active, err = database.ActiveDeployApprovalRequestForStage(token.ID, "production", db.DeployApprovalRequestStageBuild, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployApprovalRequestBuildUsed(req.ID, "B123", time.Now()))

	active, err = database.ActiveDeployApprovalRequestForStage(token.ID, "production", db.DeployApprovalRequestStageRelease, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployApprovalRequestReleaseCreated(req.ID, "R123", time.Now()))

	active, err = database.ActiveDeployApprovalRequestForStage(token.ID, "production", db.DeployApprovalRequestStagePromote, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, req.ID, active.ID)

	require.NoError(t, database.MarkDeployApprovalRequestPromoted(req.ID, "R123", token.ID, time.Now()))

	final, err := database.GetDeployApprovalRequest(req.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusConsumed, final.Status)
	require.NotNil(t, final.ReleasePromotedAt)
}

func TestDeployApprovalRequestDuplicateGuard(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)

	_, err = database.CreateDeployApprovalRequest("production", "Deploy branch", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	_, err = database.CreateDeployApprovalRequest("production", "Deploy branch", user.ID, nil, token.ID, &user.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, db.ErrDeployApprovalRequestActive)

	_, err = database.CreateDeployApprovalRequest("production", "Deploy another branch", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)
}

func TestDeployApprovalRequestExpiration(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("deployer@example.com", "Deploy", []string{"deployer"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)

	req, err := database.CreateDeployApprovalRequest("production", "Deploy release", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	_, err = database.ApproveDeployApprovalRequest(req.ID, user.ID, time.Now().Add(-1*time.Minute), "expired")
	require.NoError(t, err)

	_, err = database.ActiveDeployApprovalRequestForStage(token.ID, "production", db.DeployApprovalRequestStageBuild, 15*time.Minute)
	require.Error(t, err)
	require.ErrorIs(t, err, db.ErrDeployApprovalRequestExpired)

	updated, err := database.GetDeployApprovalRequest(req.ID)
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusConsumed, updated.Status)
}

func TestDeployApprovalRequestReject(t *testing.T) {
	database := dbtest.NewDatabase(t)

	user, err := database.CreateUser("user@example.com", "User", []string{"deployer"})
	require.NoError(t, err)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	token := createAPITokenHelper(t, database, user.ID)

	req, err := database.CreateDeployApprovalRequest("production", "Deploy", user.ID, nil, token.ID, &user.ID)
	require.NoError(t, err)

	rejected, err := database.RejectDeployApprovalRequest(req.ID, admin.ID, "not today")
	require.NoError(t, err)
	require.Equal(t, db.DeployApprovalRequestStatusRejected, rejected.Status)
	require.Equal(t, "not today", strings.TrimSpace(rejected.ApprovalNotes))
}
