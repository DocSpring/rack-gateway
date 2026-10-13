package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

func putSettings(t *testing.T, call func(*gin.Context), path, appName, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if appName != "" {
		c.Params = gin.Params{{Key: "app", Value: appName}}
	}
	c.Set("user_email", "admin@example.com")
	call(c)
	return w
}

// A setting an environment variable defines can't be changed through the API, and a refused change saves nothing.
// Forms submit every field, so resubmitting the env value along with other changes still works.
func TestEnvDefinedSettingsAreRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("RGW_SETTING_MFA_STEP_UP_WINDOW_MINUTES", "20")
	t.Setenv("RGW_APP_WEB_SETTING_PROTECTED_ENV_VARS", "DATABASE_URL")
	handler, _, _ := setupSettingsHandler(t)
	svc := handler.settingsService

	w := putSettings(t, handler.UpdateGlobalSettings, "/api/v1/settings", "",
		`{"mfa_step_up_window_minutes": 5, "mfa_trusted_device_ttl_days": 7}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "RGW_SETTING_MFA_STEP_UP_WINDOW_MINUTES")
	ttl, err := svc.GetGlobalSetting(settings.KeyTrustedDeviceTTLDays, 30)
	require.NoError(t, err)
	require.Equal(t, settings.SourceDefault, ttl.Source, "a refused update saves nothing")

	w = putSettings(t, handler.UpdateGlobalSettings, "/api/v1/settings", "",
		`{"mfa_step_up_window_minutes": 20, "mfa_trusted_device_ttl_days": 7}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ttl, err = svc.GetGlobalSetting(settings.KeyTrustedDeviceTTLDays, 30)
	require.NoError(t, err)
	require.Equal(t, settings.SourceDB, ttl.Source)

	w = putSettings(t, handler.UpdateAppProtectedEnvVars, "/api/v1/apps/web/settings/protected-env-vars", "web",
		`["SECRET_KEY_BASE"]`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	w = putSettings(t, handler.UpdateAppSettings, "/api/v1/apps/web/settings", "web",
		`{"protected_env_vars": ["DATABASE_URL"], "default_branch": "develop"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// The env var lists must hold env var names: a quoted or otherwise invalid name would protect, mask or show nothing,
// so it is refused with a 400 and nothing is saved.
func TestEnvVarListSettingsRefuseInvalidNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _, _ := setupSettingsHandler(t)

	w := putSettings(t, handler.UpdateAppProtectedEnvVars, "/api/v1/apps/web/settings/protected-env-vars", "web",
		`["\"DATABASE_URL", "SECRET_KEY_BASE\""]`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "protected_env_vars must list env var names")

	w = putSettings(t, handler.UpdateAppSettings, "/api/v1/apps/web/settings", "web",
		`{"secret_env_vars": ["BILLING ID"], "default_branch": "develop"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	all, err := handler.settingsService.GetAllAppSettings("web")
	require.NoError(t, err)
	secretEnvVars := settings.AppSettingSecretEnvVars.String()
	for _, key := range []string{settings.KeyProtectedEnvVars, secretEnvVars, settings.KeyDefaultBranch} {
		require.Equalf(t, settings.SourceDefault, all[key].Source, "%s: a refused update saves nothing", key)
	}
}
