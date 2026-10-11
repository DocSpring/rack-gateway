package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// A key in the app's secret_env_vars is a secret on the CLI path too: changing it needs secret:set, and its value
// is masked in release responses.
func TestAppSecretEnvVarsAreSecretsOnTheProxy(t *testing.T) {
	h, database, _ := newProxyForEnvTest(t)
	app := "app"
	require.NoError(t, database.UpsertSetting(&app, settings.KeySecretEnvVars, []string{"BILLING_ID"}, nil))
	deployer := createUserWithRole(t, database, "deployer@test.com", "deployer")

	form := url.Values{"env": {"BILLING_ID=changed\nPORT=3000"}}
	req := tokenRequest(
		deployer, []string{"convox:release:create", "convox:env:set"},
		http.MethodPost, "/apps/app/releases", form.Encode(),
	)
	rr := httptest.NewRecorder()
	h.ProxyToRack(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "You don't have permission to modify secrets: BILLING_ID")

	masked := maskSecretAndProtectedEnvVars("BILLING_ID=acct_123\nPORT=3000", h.loadAppEnvKeys(app))
	require.NotContains(t, masked, "acct_123")
	require.Contains(t, masked, "PORT=3000")
}
