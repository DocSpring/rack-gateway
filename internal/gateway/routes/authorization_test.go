package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth/mfa"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/deps"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/DocSpring/rack-gateway/internal/gateway/token"
)

type authzEnv struct {
	router   *gin.Engine
	database *db.Database
	tokens   *token.Service
	sessions *auth.SessionManager
	issued   []string
}

func newAuthzEnv(t *testing.T) *authzEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database := dbtest.NewDatabase(t)
	for userEmail, role := range map[string]string{
		"admin@example.com":    "admin",
		"deployer@example.com": "deployer",
		"viewer@example.com":   "viewer",
	} {
		_, err := database.CreateUser(userEmail, userEmail, []string{role})
		require.NoError(t, err)
	}

	settingsSvc := settings.NewService(database)
	sessions := auth.NewSessionManager(database, "secret", settingsSvc)
	mfaSettings, err := settingsSvc.GetMFASettings()
	require.NoError(t, err)
	rbacMgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	tokenSvc := token.NewService(database)
	mfaSvc, err := mfa.NewService(database, "Test", 24*time.Hour, 10*time.Minute, []byte("pepper"), "", "", "", "", nil)
	require.NoError(t, err)

	router := gin.New()
	Setup(router, &Config{Gateway: &deps.Gateway{
		Config:          &config.Config{Domain: "gateway.example.com", GoogleAllowedDomain: "example.com"},
		Database:        database,
		RBACManager:     rbacMgr,
		SessionManager:  sessions,
		AuthService:     auth.NewAuthService(tokenSvc, database, sessions),
		TokenService:    tokenSvc,
		MFAService:      mfaSvc,
		MFASettings:     mfaSettings,
		SettingsService: settingsSvc,
		AuditLogger:     audit.NewLogger(database),
		EmailSender:     email.NoopSender{},
	}})
	return &authzEnv{router: router, database: database, tokens: tokenSvc, sessions: sessions}
}

// credentials returns request headers that authenticate as the caller.
type credentials map[string]string

func (e *authzEnv) apiToken(t *testing.T, ownerEmail string, permissions ...string) credentials {
	t.Helper()
	owner, err := e.database.GetUser(ownerEmail)
	require.NoError(t, err)
	resp, err := e.tokens.GenerateAPIToken(&token.APITokenRequest{
		Name: fmt.Sprintf("token-%d", len(e.issued)), UserID: owner.ID, Permissions: permissions,
	})
	require.NoError(t, err)
	e.issued = append(e.issued, resp.Token)
	return credentials{"Authorization": "Bearer " + resp.Token}
}

func (e *authzEnv) webSession(t *testing.T, userEmail string) credentials {
	t.Helper()
	user, err := e.database.GetUser(userEmail)
	require.NoError(t, err)
	sessionToken, _, err := e.sessions.CreateSession(user, auth.SessionMetadata{Channel: "web"})
	require.NoError(t, err)
	csrf, err := e.sessions.DeriveCSRFToken(sessionToken)
	require.NoError(t, err)
	return credentials{"Cookie": "session_token=" + sessionToken, "X-CSRF-Token": csrf}
}

func (e *authzEnv) do(
	t *testing.T,
	creds credentials,
	method, path string,
	body interface{},
) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Host = "gateway.example.com"
	req.Header.Set("Content-Type", "application/json")
	for k, v := range creds {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

type routeCall struct {
	method string
	path   string
	body   interface{}
}

// adminOnlyCalls are admin endpoints that must reject every non-admin user and every API token.
var adminOnlyCalls = []routeCall{
	{http.MethodGet, "/api/v1/users", nil},
	{http.MethodPost, "/api/v1/users", map[string]interface{}{
		"email": "attacker@example.com", "name": "x", "roles": []string{"admin"},
	}},
	{http.MethodPut, "/api/v1/users/admin@example.com", map[string]interface{}{"roles": []string{"viewer"}}},
	{http.MethodDelete, "/api/v1/users/admin@example.com", nil},
	{http.MethodPost, "/api/v1/users/admin@example.com/lock", map[string]interface{}{"reason": "x"}},
	{http.MethodGet, "/api/v1/users/admin@example.com/sessions", nil},
	{http.MethodGet, "/api/v1/audit-logs", nil},
	{http.MethodGet, "/api/v1/audit-logs/export", nil},
	{http.MethodGet, "/api/v1/api-tokens", nil},
	{http.MethodPost, "/api/v1/api-tokens", map[string]interface{}{
		"name": "pwn", "user_email": "admin@example.com", "permissions": []string{"convox:*:*"},
	}},
	{http.MethodPut, "/api/v1/settings/deploy-approvals", map[string]interface{}{"deploy_approvals_enabled": false}},
	{http.MethodPut, "/api/v1/settings/mfa-configuration", map[string]interface{}{"mfa_require_all_users": false}},
	{http.MethodPut, "/api/v1/apps/web/settings/protected-env-vars", []string{}},
	{http.MethodGet, "/api/v1/jobs", nil},
	{http.MethodGet, "/api/v1/deploy-approval-requests", nil},
	{http.MethodPost, "/api/v1/deploy-approval-requests/00000000-0000-0000-0000-000000000000/approve", nil},
}

func isAuthzDenial(w *httptest.ResponseRecorder) bool {
	if w.Code != http.StatusForbidden {
		return false
	}
	body := w.Body.String()
	return strings.Contains(body, "insufficient permissions") ||
		strings.Contains(body, "API tokens cannot use this endpoint")
}

func TestAdminEndpointsRejectNonAdmins(t *testing.T) {
	e := newAuthzEnv(t)
	callers := map[string]credentials{
		"viewer session":   e.webSession(t, "viewer@example.com"),
		"deployer session": e.webSession(t, "deployer@example.com"),
		"viewer token":     e.apiToken(t, "viewer@example.com", "convox:app:list"),
		// Mirrors production: the CircleCI token is owned by an admin.
		"admin-owned cicd token": e.apiToken(t, "admin@example.com", rbacRolePermissions(t, "cicd")...),
		"admin-owned wildcard token": e.apiToken(
			t, "admin@example.com", "convox:*:*", "gateway:*:*", "security:*:*",
		),
	}
	for name, creds := range callers {
		for _, call := range adminOnlyCalls {
			w := e.do(t, creds, call.method, call.path, call.body)
			require.Truef(t, isAuthzDenial(w), "%s: %s %s => %d %s",
				name, call.method, call.path, w.Code, w.Body.String())
		}
	}

	admin, err := e.database.GetUser("admin@example.com")
	require.NoError(t, err)
	require.Equal(t, []string{"admin"}, admin.Roles, "admin must not have been demoted")
	attacker, err := e.database.GetUser("attacker@example.com")
	require.NoError(t, err)
	require.Nil(t, attacker, "no user may have been created")
}

func TestAdminSessionPassesAuthorization(t *testing.T) {
	e := newAuthzEnv(t)
	creds := e.webSession(t, "admin@example.com")
	for _, call := range []routeCall{
		{http.MethodGet, "/api/v1/users", nil},
		{http.MethodGet, "/api/v1/audit-logs", nil},
		{http.MethodGet, "/api/v1/api-tokens", nil},
	} {
		w := e.do(t, creds, call.method, call.path, call.body)
		require.Falsef(t, isAuthzDenial(w), "%s %s => %d %s", call.method, call.path, w.Code, w.Body.String())
	}
}

func TestCICDTokenKeepsDeployApprovalAccess(t *testing.T) {
	e := newAuthzEnv(t)
	creds := e.apiToken(t, "admin@example.com", rbacRolePermissions(t, "cicd")...)
	for _, call := range []routeCall{
		{http.MethodGet, "/api/v1/info", nil},
		{http.MethodPost, "/api/v1/deploy-approval-requests", map[string]interface{}{}},
		{http.MethodGet, "/api/v1/deploy-approval-requests/00000000-0000-0000-0000-000000000000", nil},
	} {
		w := e.do(t, creds, call.method, call.path, call.body)
		require.Falsef(t, isAuthzDenial(w), "%s %s => %d %s", call.method, call.path, w.Code, w.Body.String())
	}
}

func TestEveryGatewayRouteDeclaresAnAccessPolicy(t *testing.T) {
	var tokenRoutes []string
	for _, spec := range rbac.HTTPRouteSpecs() {
		if spec.Access == rbac.AccessPermissions {
			require.NotEmptyf(t, spec.Permissions, "%s %s requires permissions but declares none",
				spec.Method, spec.Pattern)
		}
		if spec.AllowAPIToken {
			tokenRoutes = append(tokenRoutes, spec.Method+" "+spec.Pattern)
		}
	}
	sort.Strings(tokenRoutes)
	require.Equal(t, []string{
		"GET /api/v1/deploy-approval-requests/:id",
		"GET /api/v1/info",
		"GET /api/v1/rack",
		"POST /api/v1/deploy-approval-requests",
	}, tokenRoutes, "API tokens may only reach the routes the CLI uses in CI")
}

func rbacRolePermissions(t *testing.T, role string) []string {
	t.Helper()
	perms, ok := rbac.DefaultRolePermissions()[role]
	require.Truef(t, ok, "unknown role %s", role)
	return perms
}
