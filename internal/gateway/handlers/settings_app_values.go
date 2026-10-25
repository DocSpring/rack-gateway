package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// Single-setting app endpoints: each saves or clears one app setting.

// updateAppSettingValue persists a single app setting identified by key.
func (h *SettingsHandler) updateAppSettingValue(c *gin.Context, key string) {
	appName := c.Param("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name is required"})
		return
	}

	var value interface{}
	if err := c.ShouldBindJSON(&value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	var uid *int64
	if h.rbac != nil {
		email := c.GetString("user_email")
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	ops := &appSettingsOps{service: h.settingsService}
	save := func() error { return ops.setSettings(appName, map[string]interface{}{key: value}, uid) }
	if err := changeSettings(c, ops, appName, []string{key}, save); err != nil {
		respondSettingSaveError(c, err)
		return
	}

	h.getSingleSettingResponse(c, ops, appName, key)
}

// deleteAppSettingValue removes a single app setting identified by key.
func (h *SettingsHandler) deleteAppSettingValue(c *gin.Context, key string) {
	appName := c.Param("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name is required"})
		return
	}

	ops := &appSettingsOps{service: h.settingsService}
	remove := func() error { return ops.deleteSettings(appName, []string{key}) }
	if err := changeSettings(c, ops, appName, []string{key}, remove); err != nil {
		gtwlog.Errorf("settings: failed to delete %s for app %s: %v", key, appName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete setting: %s", key)})
		return
	}

	h.getSingleSettingResponse(c, ops, appName, key)
}

// UpdateAppProtectedEnvVars godoc
// @Summary Update protected environment variables
// @Description Replaces the list of protected environment variables for the specified app
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body []string true "Protected environment variable names"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/protected-env-vars [put]
func (h *SettingsHandler) UpdateAppProtectedEnvVars(c *gin.Context) {
	h.updateAppSettingValue(c, settings.KeyProtectedEnvVars)
}

// DeleteAppProtectedEnvVars godoc
// @Summary Delete protected environment variables
// @Description Removes protected environment variable overrides for the specified app
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/protected-env-vars [delete]
func (h *SettingsHandler) DeleteAppProtectedEnvVars(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.KeyProtectedEnvVars)
}

// UpdateAppSecretEnvVars godoc
// @Summary Update secret environment variables
// @Description Replaces the list of secret environment variables for the specified app
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body []string true "Secret environment variable names"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/secret-env-vars [put]
func (h *SettingsHandler) UpdateAppSecretEnvVars(c *gin.Context) {
	h.updateAppSettingValue(c, settings.AppSettingSecretEnvVars.String())
}

// DeleteAppSecretEnvVars godoc
// @Summary Delete secret environment variables
// @Description Removes secret environment variable overrides for the specified app
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/secret-env-vars [delete]
func (h *SettingsHandler) DeleteAppSecretEnvVars(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.AppSettingSecretEnvVars.String())
}

// UpdateAppAuditVisibleEnvVars godoc
// @Summary Update env vars shown in audit logs
// @Description Replaces the list of environment variables whose values audit log entries may show for the
// @Description specified app. Secret and protected keys are always redacted.
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body []string true "Environment variable names whose values may be shown"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/audit-visible-env-vars [put]
func (h *SettingsHandler) UpdateAppAuditVisibleEnvVars(c *gin.Context) {
	h.updateAppSettingValue(c, settings.KeyAuditVisibleEnvVars)
}

// DeleteAppAuditVisibleEnvVars godoc
// @Summary Delete env vars shown in audit logs
// @Description Removes the stored list of environment variables whose values audit log entries may show
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/audit-visible-env-vars [delete]
func (h *SettingsHandler) DeleteAppAuditVisibleEnvVars(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.KeyAuditVisibleEnvVars)
}

// UpdateAppApprovedDeployCommands godoc
// @Summary Update approved deploy commands
// @Description Replaces the approved deploy commands for the specified app
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body []settings.ApprovedCommand true "Approved commands, each with the env vars it gets"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/approved-deploy-commands [put]
func (h *SettingsHandler) UpdateAppApprovedDeployCommands(c *gin.Context) {
	h.updateAppSettingValue(c, settings.KeyApprovedDeployCommands)
}

// DeleteAppApprovedDeployCommands godoc
// @Summary Delete approved deploy commands
// @Description Removes approved deploy command overrides for the specified app
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/approved-deploy-commands [delete]
func (h *SettingsHandler) DeleteAppApprovedDeployCommands(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.KeyApprovedDeployCommands)
}

// UpdateAppCommandEnvVars godoc
// @Summary Update the env vars commands get
// @Description Replaces the env vars every command run in the specified app (exec, run) gets. Empty: every env var
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body []string true "Env var names"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/command-env-vars [put]
func (h *SettingsHandler) UpdateAppCommandEnvVars(c *gin.Context) {
	h.updateAppSettingValue(c, settings.KeyCommandEnvVars)
}

// DeleteAppCommandEnvVars godoc
// @Summary Delete the env vars commands get
// @Description Removes the stored list, so every command run in the specified app gets every env var
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/command-env-vars [delete]
func (h *SettingsHandler) DeleteAppCommandEnvVars(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.KeyCommandEnvVars)
}

// UpdateAppCommandEnvReadOnly godoc
// @Summary Update the read-only env vars of commands
// @Description Replaces the env vars commands in the specified app get from another one unless run with --write
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body map[string]string true "Env var name to the env var it reads instead"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/command-env-read-only [put]
func (h *SettingsHandler) UpdateAppCommandEnvReadOnly(c *gin.Context) {
	h.updateAppSettingValue(c, settings.KeyCommandEnvReadOnly)
}

// DeleteAppCommandEnvReadOnly godoc
// @Summary Delete the read-only env vars of commands
// @Description Removes the stored substitutes, so commands in the specified app get every env var's own value
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/command-env-read-only [delete]
func (h *SettingsHandler) DeleteAppCommandEnvReadOnly(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.KeyCommandEnvReadOnly)
}

// UpdateAppServiceImagePatterns godoc
// @Summary Update service image patterns
// @Description Replaces service image constraints for the specified app
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body map[string]string true "Service image patterns"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/service-image-patterns [put]
func (h *SettingsHandler) UpdateAppServiceImagePatterns(c *gin.Context) {
	h.updateAppSettingValue(c, settings.KeyServiceImagePatterns)
}

// DeleteAppServiceImagePatterns godoc
// @Summary Delete service image patterns
// @Description Removes service image pattern overrides for the specified app
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/service-image-patterns [delete]
func (h *SettingsHandler) DeleteAppServiceImagePatterns(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.KeyServiceImagePatterns)
}

// UpdateAppRequireCommandJustification godoc
// @Summary Update whether commands need a justification
// @Description Sets whether every command run in the specified app (exec, run) must say why it's run
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param body body bool true "Whether a justification is required"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/require-command-justification [put]
func (h *SettingsHandler) UpdateAppRequireCommandJustification(c *gin.Context) {
	h.updateAppSettingValue(c, settings.KeyRequireCommandJustification)
}

// DeleteAppRequireCommandJustification godoc
// @Summary Delete whether commands need a justification
// @Description Removes the stored value; the environment variable or the default (not required) applies
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/require-command-justification [delete]
func (h *SettingsHandler) DeleteAppRequireCommandJustification(c *gin.Context) {
	h.deleteAppSettingValue(c, settings.KeyRequireCommandJustification)
}
