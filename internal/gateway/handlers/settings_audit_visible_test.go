package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

const (
	auditVisiblePattern = "/api/v1/apps/:app/settings/audit-visible-env-vars"
	auditVisiblePath    = "/api/v1/apps/test-app/settings/audit-visible-env-vars"
)

// Showing env values in the audit log and Slack needs a recent step-up, like marking env vars secret.
func TestAppSettingAuditVisibleEnvVars_MFA(t *testing.T) {
	env := newSettingsTestEnv(t)
	params := gin.Params{{Key: "app", Value: "test-app"}}
	payload := []string{"port", "RAILS_ENV"}

	authUser, _ := env.newWebAuthUser(t)
	w := env.performSettingsRequest(
		t, http.MethodPut, auditVisiblePattern, auditVisiblePath, env.handler.UpdateAppAuditVisibleEnvVars,
		authUser, payload, params, nil, nil,
	)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "step-up", w.Header().Get("X-MFA-Required"))

	authUser, _ = env.newWebAuthUser(t)
	code := env.totpCode(t)
	authUser.MFAType = "totp"
	authUser.MFAValue = code
	w = env.performSettingsRequest(
		t, http.MethodPut, auditVisiblePattern, auditVisiblePath, env.handler.UpdateAppAuditVisibleEnvVars,
		authUser, payload, params, nil, func(req *http.Request) { req.Header.Set("X-MFA-TOTP", code) },
	)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	visible, err := env.handler.settingsService.GetAuditVisibleEnvVars("test-app")
	require.NoError(t, err)
	require.Equal(t, []string{"PORT", "RAILS_ENV"}, visible)
}

// The list is validated and can be cleared; an environment variable pins it.
func TestAppSettingAuditVisibleEnvVars_UpdateAndClear(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _, _ := setupSettingsHandler(t)

	w := putSettings(t, handler.UpdateAppAuditVisibleEnvVars, auditVisiblePath, "test-app", `"PORT"`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	w = putSettings(t, handler.UpdateAppAuditVisibleEnvVars, auditVisiblePath, "test-app", `["PORT"]`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var saved map[string]settings.Setting
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, []interface{}{"PORT"}, saved[settings.KeyAuditVisibleEnvVars].Value)
	require.Equal(t, settings.SourceDB, saved[settings.KeyAuditVisibleEnvVars].Source)

	w = putSettings(t, handler.DeleteAppAuditVisibleEnvVars, auditVisiblePath, "test-app", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	visible, err := handler.settingsService.GetAuditVisibleEnvVars("test-app")
	require.NoError(t, err)
	require.Empty(t, visible)

	t.Setenv("RGW_APP_TEST_APP_SETTING_AUDIT_VISIBLE_ENV_VARS", "PORT")
	w = putSettings(t, handler.UpdateAppAuditVisibleEnvVars, auditVisiblePath, "test-app", `["RAILS_ENV"]`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "RGW_APP_TEST_APP_SETTING_AUDIT_VISIBLE_ENV_VARS")
}
