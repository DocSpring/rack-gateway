package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const (
	auditEnvAdmin      = "admin@example.com"
	auditRedactedValue = `{"old":"[REDACTED]","new":"[REDACTED]"}`
	auditEnvRackEnv    = "DATABASE_PASSWORD=old-value\nDATABASE_URL=postgres://old-host/app\nPORT=3000\n" +
		"BILLING_ID=old-id\nSTRIPE_SECRET=old-key"
)

// updateEnvAuditDetails changes every env var of myapp through the web env editor, with the app's
// audit_visible_env_vars and secret_env_vars set to visible and secret, and returns each audit entry's details by
// key.
func updateEnvAuditDetails(t *testing.T, visible, secret []string) map[string]string {
	t.Helper()
	t.Setenv("CONVOX_SECRET_ENV_VARS", "") // mise.toml names DATABASE_URL a secret
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases":
			_, _ = w.Write([]byte(`[{"id":"R1"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/apps/myapp/releases/R1":
			body, _ := json.Marshal(map[string]string{"env": auditEnvRackEnv})
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && r.URL.Path == "/apps/myapp/releases":
			_, _ = w.Write([]byte(`{"id":"R2"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	database := dbtest.NewDatabase(t)
	app := "myapp"
	require.NoError(t, database.UpsertSetting(&app, settings.KeyAuditVisibleEnvVars, visible, nil))
	require.NoError(t, database.UpsertSetting(&app, settings.KeySecretEnvVars, secret, nil))
	handler, rbacManager := newAPIHandler(t, database, server.URL)
	require.NoError(t, rbacManager.SaveUser(auditEnvAdmin, &rbac.UserConfig{Name: "Admin", Roles: []string{"admin"}}))

	body, err := json.Marshal(map[string]interface{}{"set": map[string]string{
		"DATABASE_PASSWORD": "new-value",
		"DATABASE_URL":      "postgres://new-host/app",
		"PORT":              "4000",
		"BILLING_ID":        "new-id",
		"STRIPE_SECRET":     "new-key",
	}})
	require.NoError(t, err)
	c, w := newJSONContext(body)
	c.Params = gin.Params{{Key: "app", Value: app}}
	attachUser(t, c, database, auditEnvAdmin, "Admin User")
	handler.UpdateEnvValues(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	logs, err := database.GetAuditLogs(auditEnvAdmin, time.Time{}, 50)
	require.NoError(t, err)
	details := map[string]string{}
	for _, entry := range logs {
		if key, ok := strings.CutPrefix(entry.Resource, app+"/"); ok {
			details[key] = entry.Details
		}
	}
	require.Len(t, details, 5)
	return details
}

// The env editor used to log every value that wasn't secret, so a password under an ordinary name reached the
// audit log and Slack.
func TestUpdateEnvValuesRedactsEveryValueByDefault(t *testing.T) {
	for key, details := range updateEnvAuditDetails(t, []string{}, []string{}) {
		require.Equal(t, auditRedactedValue, details, key)
	}
}

// Only keys listed in audit_visible_env_vars show values, and never secret ones.
func TestUpdateEnvValuesShowsAuditVisibleValues(t *testing.T) {
	details := updateEnvAuditDetails(t, []string{"port", "BILLING_ID", "STRIPE_SECRET"}, []string{"BILLING_ID"})
	require.Equal(t, `{"old":"3000","new":"4000"}`, details["PORT"])
	for _, key := range []string{"DATABASE_PASSWORD", "DATABASE_URL", "BILLING_ID", "STRIPE_SECRET"} {
		require.Equal(t, auditRedactedValue, details[key], key)
	}
}
