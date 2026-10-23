package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/envutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const (
	redactedEnvDetails = `{"old":"[REDACTED]","new":"[REDACTED]"}`
	envAuditApp        = "web"
	envAuditActor      = "admin@test.com"
)

// newEnvAuditProxy returns a proxy with no CONVOX_SECRET_ENV_VARS (mise.toml names DATABASE_URL a secret, which
// would redact it for another reason).
func newEnvAuditProxy(t *testing.T) (*Handler, *db.Database) {
	t.Helper()
	t.Setenv("CONVOX_SECRET_ENV_VARS", "")
	database := dbtest.NewDatabase(t)
	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)
	require.NoError(t, mgr.CreateUser(envAuditActor, &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))
	cfg := &config.Config{Racks: map[string]config.RackConfig{
		"default": {Name: "default", URL: "http://mock", Username: "convox", APIKey: "token", Enabled: true},
	}}
	h := NewHandler(
		cfg, mgr, audit.NewLogger(database), database, settings.NewService(database),
		"testrack", nil, nil, nil,
	)
	return h, database
}

func setEnvAuditAppSetting(t *testing.T, database *db.Database, key string, value []string) {
	t.Helper()
	app := envAuditApp
	require.NoError(t, database.UpsertSetting(&app, key, value, nil))
}

// logEnvAuditDetails logs diffs as a release create for the app would, and returns each entry's details by key.
func logEnvAuditDetails(t *testing.T, h *Handler, database *db.Database, diffs []envutil.EnvDiff) map[string]string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/apps/"+envAuditApp+"/releases", nil)
	h.logEnvDiffs(req, envAuditActor, "default", diffs)

	logs, err := database.GetAuditLogs(envAuditActor, time.Time{}, 50)
	require.NoError(t, err)
	details := map[string]string{}
	for _, entry := range logs {
		if key, ok := strings.CutPrefix(entry.Resource, envAuditApp+"/"); ok {
			details[key] = entry.Details
		}
	}
	require.Len(t, details, len(diffs))
	return details
}

// ordinaryEnvDiffs change env vars that don't look secret by name, though SENTRY_DSN and DATABASE_URL usually
// hold credentials.
var ordinaryEnvDiffs = []envutil.EnvDiff{
	{Key: "SENTRY_DSN", OldVal: "old-value", NewVal: "new-value"},
	{Key: "DATABASE_URL", OldVal: "postgres://old-host/app", NewVal: "postgres://new-host/app"},
	{Key: "PORT", OldVal: "3000", NewVal: "4000"},
	{Key: "RAILS_ENV", OldVal: "staging", NewVal: ""},
}

// Audit entries (and so Slack) used to show every value that wasn't secret by name or protected.
func TestLogEnvDiffsRedactsEveryValueByDefault(t *testing.T) {
	h, database := newEnvAuditProxy(t)
	for key, details := range logEnvAuditDetails(t, h, database, ordinaryEnvDiffs) {
		require.Equal(t, redactedEnvDetails, details, key)
	}
}

func TestLogEnvDiffsShowsAuditVisibleValues(t *testing.T) {
	h, database := newEnvAuditProxy(t)
	setEnvAuditAppSetting(t, database, settings.KeyAuditVisibleEnvVars, []string{"port", "RAILS_ENV"})

	details := logEnvAuditDetails(t, h, database, ordinaryEnvDiffs)
	require.Equal(t, `{"old":"3000","new":"4000"}`, details["PORT"])
	require.Equal(t, `{"old":"staging","new":""}`, details["RAILS_ENV"])
	require.Equal(t, redactedEnvDetails, details["SENTRY_DSN"])
	require.Equal(t, redactedEnvDetails, details["DATABASE_URL"])
}

// A listed key stays redacted when it is secret (by name, the app's secret_env_vars or CONVOX_SECRET_ENV_VARS)
// or protected. The diffs are flagged as the proxy flags them: it doesn't read secret_env_vars.
func TestLogEnvDiffsKeepsListedSecretAndProtectedValuesRedacted(t *testing.T) {
	h, database := newEnvAuditProxy(t)
	h.secretNames["REDIS_URL"] = struct{}{}
	setEnvAuditAppSetting(t, database, settings.KeyAuditVisibleEnvVars,
		[]string{"PORT", "STRIPE_SECRET", "BILLING_ID", "REDIS_URL", "ADMIN_EMAIL"})
	setEnvAuditAppSetting(t, database, settings.AppSettingSecretEnvVars.String(), []string{"BILLING_ID"})
	setEnvAuditAppSetting(t, database, settings.KeyProtectedEnvVars, []string{"ADMIN_EMAIL"})

	details := logEnvAuditDetails(t, h, database, []envutil.EnvDiff{
		{Key: "PORT", OldVal: "3000", NewVal: "4000"},
		{Key: "STRIPE_SECRET", OldVal: "a", NewVal: "b", Secret: true},
		{Key: "BILLING_ID", OldVal: "a", NewVal: "b"},
		{Key: "REDIS_URL", OldVal: "a", NewVal: "b", Secret: true},
		{Key: "ADMIN_EMAIL", OldVal: "a", NewVal: "b", Protected: true},
	})
	require.Equal(t, `{"old":"3000","new":"4000"}`, details["PORT"])
	for _, key := range []string{"STRIPE_SECRET", "BILLING_ID", "REDIS_URL", "ADMIN_EMAIL"} {
		require.Equal(t, redactedEnvDetails, details[key], key)
	}
}
