package proxy

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func approveForUploadTest(
	t *testing.T, database *db.Database, userID, tokenID int64, commit string,
) *db.DeployApprovalRequest {
	t.Helper()
	req, err := database.CreateDeployApprovalRequest("Deploy", "test-app", commit, "main", "", nil, userID, nil,
		tokenID)
	require.NoError(t, err)
	req, err = database.ApproveDeployApprovalRequest(req.ID, userID, time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	return req
}

// An upload binds to the one approval waiting for an upload. When several are waiting it can't tell which commit
// it's for, so it's refused rather than bound to the newest. When the newest already has its archive, the upload is
// most likely its retry, so it's refused rather than bound to an older approval.
func TestObjectUploadBindsOnlyWhenOneApprovalIsWaiting(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)
	user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)
	token, err := database.CreateAPIToken(strings.Repeat("f", 64), "ci", user.ID, nil, nil, nil)
	require.NoError(t, err)

	_, err = h.findApprovalForObjectCreate(token.ID, "test-app")
	require.ErrorIs(t, err, db.ErrDeployApprovalRequestNotFound, "no approval, so the upload is denied")

	first := approveForUploadTest(t, database, user.ID, token.ID, "aaaaaaa1111111111111111111111111111111aa")
	bound, err := h.findApprovalForObjectCreate(token.ID, "test-app")
	require.NoError(t, err)
	require.Equal(t, first.ID, bound.ID)

	approveForUploadTest(t, database, user.ID, token.ID, "bbbbbbb2222222222222222222222222222222bb")
	_, err = h.findApprovalForObjectCreate(token.ID, "test-app")
	var approvalErr *deployApprovalError
	require.ErrorAs(t, err, &approvalErr)
	require.Equal(t, http.StatusConflict, approvalErr.status)
	require.Contains(t, approvalErr.message, "aaaaaaa")
	require.Contains(t, approvalErr.message, "bbbbbbb")

	// Once the older one has its archive, the newer one is the only one waiting.
	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(first.ID, "object://test-app/a.tgz"))
	bound, err = h.findApprovalForObjectCreate(token.ID, "test-app")
	require.NoError(t, err)
	require.NotEqual(t, first.ID, bound.ID)
}

func TestObjectUploadRefusedWhenTheNewestApprovalHasItsArchive(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)
	user, err := database.CreateUser("deployer@example.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)
	token, err := database.CreateAPIToken(strings.Repeat("f", 64), "ci", user.ID, nil, nil, nil)
	require.NoError(t, err)

	approveForUploadTest(t, database, user.ID, token.ID, "aaaaaaa1111111111111111111111111111111aa")
	newer := approveForUploadTest(t, database, user.ID, token.ID, "bbbbbbb2222222222222222222222222222222bb")
	require.NoError(t, database.UpdateDeployApprovalRequestObjectURL(newer.ID, "object://test-app/b.tgz"))

	_, err = h.findApprovalForObjectCreate(token.ID, "test-app")
	var approvalErr *deployApprovalError
	require.ErrorAs(t, err, &approvalErr)
	require.Equal(t, http.StatusConflict, approvalErr.status)
	require.Contains(t, approvalErr.message, "already been uploaded")
	require.Contains(t, approvalErr.message, "bbbbbbb")
}
