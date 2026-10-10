package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// tokenRequest returns a request authenticated as an API token owned by owner.
func tokenRequest(owner *db.User, permissions []string, method, path, body string) *http.Request {
	tokenID := int64(99)
	tokenUser := &auth.User{
		Email: owner.Email, IsAPIToken: true, TokenID: &tokenID, TokenName: "ci",
		Permissions: permissions, DBUser: owner,
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, tokenUser))
}

func createUserWithRole(t *testing.T, database *db.Database, email, role string) *db.User {
	t.Helper()
	user, err := database.CreateUser(email, role, []string{role})
	require.NoError(t, err)
	return user
}

// Env masking uses the token's own permissions, not its admin owner's.
func TestTokenWithoutEnvReadSeesMaskedEnv(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	admin := createUserWithRole(t, database, "admin@test.com", "admin")
	req := tokenRequest(admin, []string{"convox:release:read"}, http.MethodGet, "/apps/testapp/releases/R1", "")

	out := string(h.filterReleaseEnvForUser(req, []byte(`{"id":"R1","env":"PORT=3000\n"}`), "testapp"))
	require.Contains(t, out, "PORT=********************")
}

// A token that can create releases but not set secrets cannot change a secret, even with an admin owner.
func TestTokenWithoutSecretSetCannotChangeSecrets(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	admin := createUserWithRole(t, database, "admin@test.com", "admin")
	form := url.Values{"env": {"SECRET_KEY=abc\nPORT=3000"}}
	req := tokenRequest(
		admin, []string{"convox:release:create", "convox:env:set"},
		http.MethodPost, "/apps/app/releases", form.Encode(),
	)

	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)
	require.Contains(t, rr.Body.String(), "You don't have permission to modify secrets: SECRET_KEY")
}

// deploy_with_approval lets a token act without holding the permission itself, but never beyond its owner.
func TestDeployWithApprovalNeverExceedsTokenOwner(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	require.NoError(t, database.UpsertSetting(nil, settings.KeyDeployApprovalsEnabled, false, nil))
	cicd := rbac.DefaultPermissionsForRole("cicd")

	viewer := createUserWithRole(t, database, "viewer@test.com", "viewer")
	req := tokenRequest(viewer, cicd, http.MethodPost, "/apps/app1/builds", "")
	allowed, _, err := h.evaluateAPITokenPermission(req, req.Context().Value(auth.UserContextKey).(*auth.User),
		rbac.ResourceBuild, rbac.ActionCreate)
	require.NoError(t, err)
	require.False(t, allowed, "a viewer's token must not create builds")

	deployer := createUserWithRole(t, database, "deployer@test.com", "deployer")
	req = tokenRequest(deployer, cicd, http.MethodPost, "/apps/app1/builds", "")
	allowed, _, err = h.evaluateAPITokenPermission(req, req.Context().Value(auth.UserContextKey).(*auth.User),
		rbac.ResourceBuild, rbac.ActionCreate)
	require.NoError(t, err)
	require.True(t, allowed, "a deployer's CI token keeps working when approvals are disabled")
}
