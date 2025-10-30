package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func newProxyForEnvTest(t *testing.T) (*Handler, *db.Database, rbac.RBACManager) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	settingsService := settings.NewService(database)
	h := NewHandler(&config.Config{Racks: map[string]config.RackConfig{
		"default": {Name: "default", URL: "http://mock", Username: "convox", APIKey: "token", Enabled: true},
	}}, mgr, audit.NewLogger(database), database, settingsService, email.NoopSender{}, "testrack", "testrack", nil, nil, nil)
	// Configure extra secret names
	h.secretNames["DATABASE_URL"] = struct{}{}
	h.secretNames["REDIS_URL"] = struct{}{}
	h.secretNames["SECRET_KEY"] = struct{}{}
	return h, database, mgr
}

func TestFilterReleaseEnvForUser(t *testing.T) {
	h, _, mgr := newProxyForEnvTest(t)
	// Users
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	require.NoError(t, mgr.SaveUser("ops@test.com", &rbac.UserConfig{Name: "Ops", Roles: []string{"ops"}}))
	require.NoError(t, mgr.SaveUser("deployer@test.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}))

	// Body with env field
	body := `{"id":"R1","env":"DATABASE_URL=postgres://...\nSECRET_KEY=abc\nREDIS_URL=redis://...\nPORT=3000\n"}`

	// Admin should still see masked secrets (native releases always masked)
	out := h.filterReleaseEnvForUser("admin@test.com", []byte(body), true)
	s := string(out)
	require.Contains(t, s, "SECRET_KEY=********************")
	require.Contains(t, s, "DATABASE_URL=********************")

	// Ops sees masked sensitive values
	out = h.filterReleaseEnvForUser("ops@test.com", []byte(body), false)
	s = string(out)
	require.Contains(t, s, "SECRET_KEY=********************")
	require.Contains(t, s, "DATABASE_URL=********************")
	require.Contains(t, s, "REDIS_URL=********************")
	require.Contains(t, s, "PORT=3000")

	// Deployer same as ops
	out = h.filterReleaseEnvForUser("deployer@test.com", []byte(body), false)
	s = string(out)
	require.Contains(t, s, "SECRET_KEY=*********")
}

func TestFilterReleaseEnv_NoEnvViewMasksAll(t *testing.T) {
	h, _, mgr := newProxyForEnvTest(t)
	require.NoError(t, mgr.SaveUser("viewer@test.com", &rbac.UserConfig{Name: "Viewer", Roles: []string{"viewer"}}))

	body := `{"id":"R1","env":"DATABASE_URL=postgres://...\nSECRET_KEY=abc\nREDIS_URL=redis://...\nPORT=3000\n"}`

	out := h.filterReleaseEnvForUser("viewer@test.com", []byte(body), false)
	s := string(out)
	// Should contain env, but all values masked
	require.Contains(t, s, "DATABASE_URL=********************")
	require.Contains(t, s, "SECRET_KEY=********************")
	require.Contains(t, s, "REDIS_URL=********************")
	require.Contains(t, s, "PORT=********************")
}

func TestAuditLogsForEnvChanges_MultipleRows(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	r := httptest.NewRequest(http.MethodPost, "/apps/app/releases", nil)
	r.Header.Set("X-User-Name", "Admin User")
	// Two diffs
	diffs := []envutil.EnvDiff{
		{Key: "FOO", OldVal: "old", NewVal: "new", Secret: false},
		{Key: "SECRET_KEY", OldVal: "[redacted]", NewVal: "[redacted]", Secret: true},
	}
	h.logEnvDiffs(r, "admin@test.com", "default", diffs)

	logs, err := database.GetAuditLogs("admin@test.com", time.Time{}, 10)
	require.NoError(t, err)
	// Find at least two env-related entries (env.set and secrets.set)
	count := 0
	for _, l := range logs {
		if l.Action == audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String()) || l.Action == audit.BuildAction(rbac.ResourceSecret.String(), rbac.ActionSet.String()) {
			count++
		}
	}
	require.GreaterOrEqual(t, count, 2)
}

func TestEnvSetPermissions(t *testing.T) {
	h, _, mgr := newProxyForEnvTest(t)
	// Users
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	require.NoError(t, mgr.SaveUser("deployer@test.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}))

	// Request with headers Env containing mixed keys
	req := httptest.NewRequest(http.MethodPost, "/apps/app/releases", nil)
	req.Header.Add("Env", strings.Join([]string{
		"PORT=3000",
		"SECRET_KEY=abc",
		"DATABASE_URL=postgres://...",
	}, "\n"))

	// Deployer should be denied due to secret keys
	ok := h.checkEnvSetPermissions(req, "deployer@test.com")
	require.False(t, ok)

	// Admin allowed
	ok = h.checkEnvSetPermissions(req, "admin@test.com")
	require.True(t, ok)

	// Deployer with non-secret only should be allowed
	req2 := httptest.NewRequest(http.MethodPost, "/apps/app/releases", nil)
	req2.Header.Set("Env", "PORT=3000\nNODE_ENV=production")
	ok = h.checkEnvSetPermissions(req2, "deployer@test.com")
	require.True(t, ok)
}

func TestProxyBlocksReleaseCreateWithSecretSetForDeployer(t *testing.T) {
	h, _, mgr := newProxyForEnvTest(t)
	require.NoError(t, mgr.SaveUser("deployer@test.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}))

	// Build request with form-encoded body simulating CLI
	form := url.Values{}
	form.Set("env", "SECRET_KEY=abc\nPORT=3000")
	req := httptest.NewRequest(http.MethodPost, "/apps/app/releases", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	au := &auth.AuthUser{Email: "deployer@test.com", Name: "Deployer"}
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, au))

	rr := httptest.NewRecorder()
	// Will be denied before attempting to forward (since rack URL is dummy)
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)
}

func TestProxyBlocksProtectedEnvChangesAndAudits(t *testing.T) {
	h, database, mgr := newProxyForEnvTest(t)
	// Set protected env var for the app (app-scoped setting)
	appName := "app"
	require.NoError(t, database.UpsertSetting(&appName, "protected_env_vars", []string{"DATABASE_URL"}, nil))
	// Admin user (even admin should be blocked from protected changes)
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	// Attempt to change protected key via releases form
	form := url.Values{}
	form.Set("env", "DATABASE_URL=abc\nPORT=3000")
	req := httptest.NewRequest(http.MethodPost, "/apps/app/releases", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	au := &auth.AuthUser{Email: "admin@test.com", Name: "Admin"}
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, au))
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)

	logs, err := database.GetAuditLogs("admin@test.com", time.Time{}, 50)
	require.NoError(t, err)
	found := false
	for _, l := range logs {
		if l.Action == audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String()) && l.Status == "denied" && strings.Contains(l.Resource, "/DATABASE_URL") {
			found = true
			break
		}
	}
	require.True(t, found, "expected denied env.set audit for protected key change")
}
