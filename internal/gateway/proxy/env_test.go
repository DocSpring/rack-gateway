package proxy

import (
	"context"
	"encoding/json"
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

func newProxyForEnvTest(t *testing.T) (*Handler, *db.Database, rbac.Manager) {
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	settingsService := settings.NewService(database)
	rackConfig := map[string]config.RackConfig{
		"default": {Name: "default", URL: "http://mock", Username: "convox", APIKey: "token", Enabled: true},
	}
	h := NewHandler(
		&config.Config{Racks: rackConfig},
		mgr,
		audit.NewLogger(database),
		database,
		settingsService,
		email.NoopSender{},
		"testrack",
		"testrack",
		nil,
		nil,
		nil,
	)
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
	require.NoError(
		t,
		mgr.SaveUser("deployer@test.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}),
	)

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
		if l.Action == audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String()) ||
			l.Action == audit.BuildAction(rbac.ResourceSecret.String(), rbac.ActionSet.String()) {
			count++
		}
	}
	require.GreaterOrEqual(t, count, 2)
}

func TestEnvSetPermissions(t *testing.T) {
	h, _, mgr := newProxyForEnvTest(t)
	// Users
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	require.NoError(
		t,
		mgr.SaveUser("deployer@test.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}),
	)

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
	require.NoError(
		t,
		mgr.SaveUser("deployer@test.com", &rbac.UserConfig{Name: "Deployer", Roles: []string{"deployer"}}),
	)

	// Build request with form-encoded body simulating CLI
	form := url.Values{}
	form.Set("env", "SECRET_KEY=abc\nPORT=3000")
	req := httptest.NewRequest(http.MethodPost, "/apps/app/releases", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	au := &auth.User{Email: "deployer@test.com", Name: "Deployer"}
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, au))

	rr := httptest.NewRecorder()
	// Will be denied before attempting to forward (since rack URL is dummy)
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)
}

// TestValidateProtectedKeysAllowsMaskedValues reproduces a bug where running
// `env set LOGSTRUCT_ENABLED=false` would fail with "protected key change denied"
// for unrelated protected keys like ADMIN_PASSWORD.
//
// The bug: When the CLI posts the full env, protected keys appear in the posted
// map with their masked value (e.g., ADMIN_PASSWORD=********************).
// validateProtectedKeys was rejecting ANY protected key in the posted map,
// even when the value was just the masked placeholder (not an actual change).
func TestValidateProtectedKeysAllowsMaskedValues(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)

	// Set ADMIN_PASSWORD as protected for the app
	appName := "docspring"
	require.NoError(t, database.UpsertSetting(&appName, "protected_env_vars", []string{"ADMIN_PASSWORD"}, nil))

	// Simulate what the CLI posts: full env with protected key masked
	// User is trying to change LOGSTRUCT_ENABLED, but the posted map
	// includes ALL keys (with secrets/protected keys masked)
	posted := map[string]string{
		"ADMIN_PASSWORD":    envutil.MaskedSecret, // masked - not a real change
		"LOGSTRUCT_ENABLED": "false",              // actual change
		"OTHER_VAR":         "value",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/docspring/releases", nil)
	req.Header.Set("X-User-Name", "Admin")

	// This should succeed - ADMIN_PASSWORD is masked, so it's not being changed
	err := h.validateProtectedKeys(req, "admin@test.com", appName, posted)
	require.NoError(t, err, "validateProtectedKeys should allow masked protected keys")
}

// TestValidateProtectedKeysBlocksActualChanges ensures that trying to actually
// change a protected key's value is still blocked.
func TestValidateProtectedKeysBlocksActualChanges(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)

	appName := "docspring"
	require.NoError(t, database.UpsertSetting(&appName, "protected_env_vars", []string{"ADMIN_PASSWORD"}, nil))

	// User tries to actually change the protected key
	posted := map[string]string{
		"ADMIN_PASSWORD":    "new_password", // actual change - should be blocked
		"LOGSTRUCT_ENABLED": "false",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/docspring/releases", nil)
	req.Header.Set("X-User-Name", "Admin")

	err := h.validateProtectedKeys(req, "admin@test.com", appName, posted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "protected key change denied")
}

// TestMergeEnvPreservesProtectedKeysNotInPosted reproduces a bug where running
// `env unset LOGSTRUCT_DEBUG` would fail with "protected key change denied"
// for unrelated protected keys like DATABASE_URL_DIRECT that aren't in the posted env.
//
// The bug: When the CLI posts the env, it may not include protected keys at all
// (or they may be filtered). The code was treating missing keys as deletions and
// validateProtectedDiffs was rejecting any "deletion" of a protected key.
func TestMergeEnvPreservesProtectedKeysNotInPosted(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)

	appName := "docspring"
	require.NoError(t, database.UpsertSetting(&appName, "protected_env_vars", []string{"DATABASE_URL_DIRECT"}, nil))

	// Posted env does NOT include DATABASE_URL_DIRECT at all
	// User is just unsetting LOGSTRUCT_DEBUG
	posted := map[string]string{
		"LOGSTRUCT_DEBUG": "", // Being unset
		"OTHER_VAR":       "value",
	}
	order := []string{"LOGSTRUCT_DEBUG", "OTHER_VAR"}

	// Base env has DATABASE_URL_DIRECT
	baseEnv := map[string]string{
		"DATABASE_URL_DIRECT": "postgres://localhost/db",
		"LOGSTRUCT_DEBUG":     "true",
		"OTHER_VAR":           "value",
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/docspring/releases", nil)
	req.Header.Set("X-User-Name", "Admin")

	// This should succeed - DATABASE_URL_DIRECT is not being changed
	merged, diffs, err := h.mergeEnvAndComputeDiffs(
		req, "admin@test.com", appName, posted, order, baseEnv, true,
	)
	require.NoError(t, err, "mergeEnvAndComputeDiffs should allow unchanged protected keys")

	// DATABASE_URL_DIRECT should be preserved in merged
	require.Equal(t, "postgres://localhost/db", merged["DATABASE_URL_DIRECT"])

	// Only LOGSTRUCT_DEBUG should have a diff (being unset)
	// DATABASE_URL_DIRECT should NOT appear in diffs since it's protected and unchanged
	var logstructDiff *envutil.EnvDiff
	for i := range diffs {
		if diffs[i].Key == "LOGSTRUCT_DEBUG" {
			logstructDiff = &diffs[i]
		}
		if diffs[i].Key == "DATABASE_URL_DIRECT" {
			t.Errorf("DATABASE_URL_DIRECT should not appear in diffs - it's protected and not being changed")
		}
	}
	require.NotNil(t, logstructDiff, "LOGSTRUCT_DEBUG should have a diff")
	require.Equal(t, "true", logstructDiff.OldVal)
	require.Equal(t, "", logstructDiff.NewVal)
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
	au := &auth.User{Email: "admin@test.com", Name: "Admin"}
	req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, au))
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)

	logs, err := database.GetAuditLogs("admin@test.com", time.Time{}, 50)
	require.NoError(t, err)
	found := false
	for _, l := range logs {
		if l.Action == audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionSet.String()) && l.Status == "denied" &&
			strings.Contains(l.Resource, "/DATABASE_URL") {
			found = true
			break
		}
	}
	require.True(t, found, "expected denied env.set audit for protected key change")
}

// TestEnvUnsetWithProtectedKeysFullFlow tests the complete flow of `cx env unset`:
// 1. CLI does GET /apps/{app}/environment - gateway should mask protected keys
// 2. CLI modifies env (removes target key)
// 3. CLI does POST /apps/{app}/environment - gateway should accept masked protected keys
//
// This test verifies the fix for the bug where env unset fails with "protected key change denied"
// because the GET response wasn't masking protected keys.
func TestEnvUnsetWithProtectedKeysFullFlow(t *testing.T) {
	h, database, mgr := newProxyForEnvTest(t)

	// Set up a protected key for the app
	appName := "docspring"
	protectedVars := []string{"ADMIN_DATABASE_URL_DIRECT"}
	require.NoError(t, database.UpsertSetting(&appName, "protected_env_vars", protectedVars, nil))

	// Admin user should be able to env unset without triggering protected key errors
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	// Step 1: Test that GET /apps/{app}/environment response masks protected keys
	// using filterEnvironmentMapResponse (the fix)
	envGetResponse := map[string]interface{}{
		"ADMIN_DATABASE_URL_DIRECT": "postgres://admin:secret@localhost/db",
		"LOGSTRUCT_DEBUG":           "true",
		"OTHER_VAR":                 "value",
	}
	envGetBody, err := json.Marshal(envGetResponse)
	require.NoError(t, err)

	// Use the new filterEnvironmentMapResponse which handles the /environment format
	filteredBody := h.filterEnvironmentMapResponse("admin@test.com", envGetBody, appName)

	// Parse the filtered response
	var filteredEnv map[string]string
	require.NoError(t, json.Unmarshal(filteredBody, &filteredEnv))

	// Verify protected key is masked
	require.Equal(t, envutil.MaskedSecret, filteredEnv["ADMIN_DATABASE_URL_DIRECT"],
		"Protected key should be masked in GET /environment response")

	// Verify non-protected keys are NOT masked
	require.Equal(t, "true", filteredEnv["LOGSTRUCT_DEBUG"],
		"Non-protected key should NOT be masked")
	require.Equal(t, "value", filteredEnv["OTHER_VAR"],
		"Non-protected key should NOT be masked")

	// Step 2: Test validateProtectedKeys with masked value (what happens after the fix)
	postedWithMasked := map[string]string{
		"ADMIN_DATABASE_URL_DIRECT": envutil.MaskedSecret, // Properly masked
		"OTHER_VAR":                 "value",
		// LOGSTRUCT_DEBUG removed (being unset)
	}

	req := httptest.NewRequest(http.MethodPost, "/apps/docspring/environment", nil)
	req.Header.Set("X-User-Name", "Admin")

	err = h.validateProtectedKeys(req, "admin@test.com", appName, postedWithMasked)
	require.NoError(t, err, "validateProtectedKeys should allow masked protected keys")

	// Step 3: Test validateProtectedKeys with real value (should still be blocked for direct attempts)
	postedWithReal := map[string]string{
		"ADMIN_DATABASE_URL_DIRECT": "postgres://admin:secret@localhost/db", // Real value - should be blocked!
		"OTHER_VAR":                 "value",
	}

	err = h.validateProtectedKeys(req, "admin@test.com", appName, postedWithReal)
	require.Error(t, err, "validateProtectedKeys should block real protected key values")
	require.Contains(t, err.Error(), "protected key change denied")
}

// TestFilterEnvironmentEndpointResponse tests that the /apps/{app}/environment
// endpoint response has protected keys masked via filterEnvironmentMapResponse.
func TestFilterEnvironmentEndpointResponse(t *testing.T) {
	h, database, mgr := newProxyForEnvTest(t)

	// Set up admin user with env:read permission
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	appName := "docspring"
	require.NoError(t, database.UpsertSetting(&appName, "protected_env_vars", []string{"ADMIN_PASSWORD"}, nil))

	// /environment returns a simple JSON map
	envResponse := `{"ADMIN_PASSWORD":"real_secret_password","PORT":"3000","NODE_ENV":"production"}`

	// Use filterEnvironmentMapResponse (the fix for the /environment endpoint)
	filtered := h.filterEnvironmentMapResponse("admin@test.com", []byte(envResponse), appName)

	var result map[string]string
	require.NoError(t, json.Unmarshal(filtered, &result))

	// Verify protected key is masked
	require.Equal(t, envutil.MaskedSecret, result["ADMIN_PASSWORD"],
		"Protected key ADMIN_PASSWORD should be masked")

	// Verify non-protected keys are NOT masked
	require.Equal(t, "3000", result["PORT"],
		"Non-protected key PORT should NOT be masked")
	require.Equal(t, "production", result["NODE_ENV"],
		"Non-protected key NODE_ENV should NOT be masked")
}

// TestFilterEnvironmentMasksSecretKeys tests that secret keys (DATABASE_URL, etc.)
// are also masked even if not explicitly protected.
func TestFilterEnvironmentMasksSecretKeys(t *testing.T) {
	h, database, mgr := newProxyForEnvTest(t)

	// Set up admin user with env:read permission
	require.NoError(t, mgr.SaveUser("admin@test.com", &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	appName := "myapp"
	// No protected env vars configured for this app
	require.NoError(t, database.UpsertSetting(&appName, "protected_env_vars", []string{}, nil))

	// These keys should be masked by default (secretNames configured in handler)
	envResponse := `{"DATABASE_URL":"postgres://secret@localhost","REDIS_URL":"redis://secret@localhost","PORT":"3000"}`

	filtered := h.filterEnvironmentMapResponse("admin@test.com", []byte(envResponse), appName)

	var result map[string]string
	require.NoError(t, json.Unmarshal(filtered, &result))

	// DATABASE_URL and REDIS_URL are in h.secretNames (configured in newProxyForEnvTest)
	require.Equal(t, envutil.MaskedSecret, result["DATABASE_URL"],
		"Secret key DATABASE_URL should be masked")
	require.Equal(t, envutil.MaskedSecret, result["REDIS_URL"],
		"Secret key REDIS_URL should be masked")

	// Non-secret keys should NOT be masked
	require.Equal(t, "3000", result["PORT"],
		"Non-secret key PORT should NOT be masked")
}
