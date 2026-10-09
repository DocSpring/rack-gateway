package routes

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func (e *authzEnv) listTokenNames(t *testing.T, creds credentials) []string {
	t.Helper()
	w := e.do(t, creds, http.MethodGet, "/api/v1/api-tokens", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tokens []db.APIToken
	decodeJSON(t, w.Body.Bytes(), &tokens)
	names := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		names = append(names, tok.Name)
	}
	sort.Strings(names)
	return names
}

func TestDeployerCreatesCITokenForThemself(t *testing.T) {
	e := newAuthzEnv(t)
	adminToken := e.ownedToken(t, "admin@example.com", "admin-ci", "convox:app:list")
	deployer := e.enrolledSession(t, "deployer@example.com")

	w := e.do(t, deployer.withMFA(t), http.MethodPost, "/api/v1/api-tokens",
		map[string]interface{}{"name": "deployer-ci", "role": "cicd"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var created struct {
		APIToken db.APIToken `json:"api_token"`
	}
	decodeJSON(t, w.Body.Bytes(), &created)
	require.Equal(t, mustUserID(t, e, "deployer@example.com"), created.APIToken.UserID)

	require.Equal(t, []string{"deployer-ci"}, e.listTokenNames(t, deployer.creds))

	w = e.do(t, deployer.creds, http.MethodGet, "/api/v1/api-tokens/"+adminToken.PublicID, nil)
	require.Equal(t, http.StatusNotFound, w.Code, "another user's token must look nonexistent")
	w = e.do(t, deployer.creds, http.MethodGet, "/api/v1/api-tokens/"+created.APIToken.PublicID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestDeployerCannotIssueTokensForOthersOrBeyondTheirRole(t *testing.T) {
	e := newAuthzEnv(t)
	deployer := e.enrolledSession(t, "deployer@example.com")

	w := e.do(t, deployer.withMFA(t), http.MethodPost, "/api/v1/api-tokens", map[string]interface{}{
		"name": "for-admin", "user_email": "admin@example.com", "permissions": []string{"convox:app:list"},
	})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "requires gateway:api_token:manage")

	w = e.do(t, deployer.withMFA(t), http.MethodPost, "/api/v1/api-tokens", map[string]interface{}{
		"name": "too-much", "permissions": []string{"convox:app:list", "convox:app:delete"},
	})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "cannot exceed the owner's role: convox:app:delete")

	tokens, err := e.database.ListAllAPITokens()
	require.NoError(t, err)
	require.Empty(t, tokens)
}

func TestDeployerCannotDeleteOthersTokens(t *testing.T) {
	e := newAuthzEnv(t)
	adminToken := e.ownedToken(t, "admin@example.com", "admin-ci", "convox:app:list")
	ownToken := e.ownedToken(t, "deployer@example.com", "deployer-ci", "convox:app:list")
	deployer := e.enrolledSession(t, "deployer@example.com")

	w := e.do(t, deployer.withMFA(t), http.MethodDelete, "/api/v1/api-tokens/"+adminToken.PublicID, nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	w = e.do(t, deployer.withMFA(t), http.MethodDelete, "/api/v1/api-tokens/"+ownToken.PublicID, nil)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	remaining, err := e.database.ListAllAPITokens()
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, adminToken.ID, remaining[0].ID)
}

func TestViewerSeesOnlyTheirOwnTokens(t *testing.T) {
	e := newAuthzEnv(t)
	e.ownedToken(t, "admin@example.com", "admin-ci", "convox:app:list")
	e.ownedToken(t, "viewer@example.com", "viewer-script", "convox:app:list")
	viewer := e.enrolledSession(t, "viewer@example.com")

	require.Equal(t, []string{"viewer-script"}, e.listTokenNames(t, viewer.creds))

	w := e.do(t, viewer.creds, http.MethodPost, "/api/v1/api-tokens",
		map[string]interface{}{"name": "mine", "permissions": []string{"convox:app:list"}})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "requires gateway:api_token:create")
}

func TestAdminManagesEveryTokenWithinOwnerRoles(t *testing.T) {
	e := newAuthzEnv(t)
	deployerToken := e.ownedToken(t, "deployer@example.com", "deployer-ci", "convox:app:list")
	e.ownedToken(t, "viewer@example.com", "viewer-script", "convox:app:list")
	admin := e.enrolledSession(t, "admin@example.com")

	require.Equal(t, []string{"deployer-ci", "viewer-script"}, e.listTokenNames(t, admin.creds))
	w := e.do(t, admin.creds, http.MethodGet, "/api/v1/api-tokens/"+deployerToken.PublicID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Even an admin can't give a deployer's token more than the deployer holds.
	w = e.do(t, admin.withMFA(t), http.MethodPut, "/api/v1/api-tokens/"+deployerToken.PublicID,
		map[string]interface{}{"permissions": []string{"convox:app:delete"}})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "cannot exceed the owner's role")

	w = e.do(t, admin.withMFA(t), http.MethodPost, "/api/v1/api-tokens", map[string]interface{}{
		"name": "viewer-read", "user_email": "viewer@example.com", "permissions": []string{"convox:app:list"},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// The expiry a token is created with is stored and enforced.
func TestCreatedTokenKeepsItsExpiry(t *testing.T) {
	e := newAuthzEnv(t)
	deployer := e.enrolledSession(t, "deployer@example.com")
	expiresAt := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)

	w := e.do(t, deployer.withMFA(t), http.MethodPost, "/api/v1/api-tokens", map[string]interface{}{
		"name": "expiring-ci", "role": "cicd", "expires_at": expiresAt.Format(time.RFC3339),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var created struct {
		APIToken db.APIToken `json:"api_token"`
	}
	decodeJSON(t, w.Body.Bytes(), &created)

	stored, err := e.database.GetAPITokenByPublicID(created.APIToken.PublicID)
	require.NoError(t, err)
	require.NotNil(t, stored.ExpiresAt, "expires_at must be stored")
	require.WithinDuration(t, expiresAt, *stored.ExpiresAt, time.Second)
}

// Naming yourself as the owner in different letter case still issues the token to you.
func TestDeployerNamingThemselfInAnyCaseOwnsTheToken(t *testing.T) {
	e := newAuthzEnv(t)
	deployer := e.enrolledSession(t, "deployer@example.com")

	w := e.do(t, deployer.withMFA(t), http.MethodPost, "/api/v1/api-tokens", map[string]interface{}{
		"name": "mixed-case-ci", "role": "cicd", "user_email": "Deployer@Example.com",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var created struct {
		APIToken db.APIToken `json:"api_token"`
	}
	decodeJSON(t, w.Body.Bytes(), &created)
	require.Equal(t, mustUserID(t, e, "deployer@example.com"), created.APIToken.UserID)
}
