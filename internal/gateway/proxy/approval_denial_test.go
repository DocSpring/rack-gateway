package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

func TestDescribeApprovalState(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	ended := now.Add(-time.Minute)
	open := now.Add(time.Hour)
	cases := []struct {
		status  string
		expires *time.Time
		want    string
	}{
		{db.DeployApprovalRequestStatusPending, nil, "is waiting for approval"},
		{db.DeployApprovalRequestStatusRejected, nil, "was rejected"},
		{db.DeployApprovalRequestStatusDeployed, &open, "has already been deployed"},
		{db.DeployApprovalRequestStatusExpired, nil, "expired before it was approved"},
		{db.DeployApprovalRequestStatusExpired, &ended, "expired at 2026-10-13T11:59:00Z"},
		{db.DeployApprovalRequestStatusApproved, &ended, "approval window ended at 2026-10-13T11:59:00Z"},
		{db.DeployApprovalRequestStatusApproved, &open, "this request isn't part of its deploy"},
	}
	for _, tc := range cases {
		req := &db.DeployApprovalRequest{Status: tc.status, ApprovalExpiresAt: tc.expires}
		require.Contains(t, describeApprovalState(req, now), tc.want, tc.status)
	}
}

// A token refused by its deploy approval is told why: no request at all, or what its newest request is waiting on.
func TestApprovalDenialSaysWhy(t *testing.T) {
	h, database := newProxyForDeployApprovalTest(t)
	deployer := createUserWithRole(t, database, "deployer@example.com", "deployer")
	perms := []string{rbac.Convox(rbac.ResourceDeploy, rbac.ActionDeployWithApproval)}
	token, err := database.CreateAPIToken(strings.Repeat("b", 64), "ci", deployer.ID, perms, nil, nil)
	require.NoError(t, err)

	promote := func() error {
		t.Helper()
		caller := &auth.User{
			Email: deployer.Email, IsAPIToken: true, TokenID: &token.ID, TokenName: "ci",
			Permissions: perms, DBUser: deployer,
		}
		req := httptest.NewRequest(http.MethodPost, "/apps/test-app/releases/RABC/promote", nil)
		allowed, _, err := h.evaluateAPITokenPermission(req, caller, rbac.ResourceRelease, rbac.ActionPromote)
		require.False(t, allowed)
		var denial *deployApprovalError
		require.ErrorAs(t, err, &denial)
		require.Equal(t, http.StatusForbidden, denial.status)
		return err
	}

	require.EqualError(t, promote(), "You don't have permission to promote releases: this API token has no deploy "+
		"approval request for test-app (request one with rack-gateway deploy-approval request)")

	request, err := database.CreateDeployApprovalRequest(
		"Deploy", "test-app", strings.Repeat("ab12", 10), "main", "", []byte("{}"), deployer.ID, nil, token.ID,
	)
	require.NoError(t, err)
	require.ErrorContains(t, promote(),
		fmt.Sprintf("its newest request, %s (commit ab12ab1), is waiting for approval", request.PublicID))

	_, err = database.RejectDeployApprovalRequest(request.ID, deployer.ID, "no")
	require.NoError(t, err)
	require.ErrorContains(t, promote(), "was rejected")
}
