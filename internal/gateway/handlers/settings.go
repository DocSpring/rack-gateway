package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// SettingsHandler handles generic settings API endpoints.
type SettingsHandler struct {
	settingsService *settings.Service
	rbac            rbac.Manager
}

// NewSettingsHandler creates a new settings handler.
func NewSettingsHandler(settingsService *settings.Service, rbacMgr rbac.Manager) *SettingsHandler {
	return &SettingsHandler{
		settingsService: settingsService,
		rbac:            rbacMgr,
	}
}

func buildKeySet(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

func isAllowedKey(allowed map[string]struct{}, key string) bool {
	if allowed == nil {
		return true
	}
	_, ok := allowed[key]
	return ok
}

// GetAllGlobalSettings godoc
// @Summary Get all global settings
// @Description Returns all global settings with their sources (db, env, or default)
// @Tags Settings
// @Produce json
// @Success 200 {object} map[string]settings.Setting
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /settings [get]
func (h *SettingsHandler) GetAllGlobalSettings(c *gin.Context) {
	allSettings, err := h.settingsService.GetAllGlobalSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get settings"})
		return
	}

	c.JSON(http.StatusOK, allSettings)
}

// GetGlobalSetting godoc
// @Summary Get a specific global setting
// @Description Returns a single global setting with its source
// @Tags Settings
// @Produce json
// @Param key path string true "Setting key"
// @Success 200 {object} settings.Setting
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /settings/{key} [get]
func (h *SettingsHandler) GetGlobalSetting(c *gin.Context) {
	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	// Get default value based on key
	defaultValue, err := settings.GetGlobalSettingDefault(key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown setting key"})
		return
	}

	setting, err := h.settingsService.GetGlobalSetting(key, defaultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get setting"})
		return
	}

	c.JSON(http.StatusOK, setting)
}

// UpdateGlobalSettings godoc
// @Summary Update multiple global settings
// @Description Updates multiple global settings atomically and stores them in the database
// @Tags Settings
// @Accept json
// @Produce json
// @Param settings body map[string]interface{} true "Map of setting keys to values"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /settings [put]
func (h *SettingsHandler) UpdateGlobalSettings(c *gin.Context) {
	h.updateGlobalSettingsWithKeys(c, nil)
}

// UpdateGlobalMFAConfiguration updates MFA configuration settings.
func (h *SettingsHandler) UpdateGlobalMFAConfiguration(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupMFAConfiguration)
	h.updateGlobalSettingsWithKeys(c, allowedKeys)
}

// UpdateGlobalAllowDestructiveActions updates allow destructive actions settings.
func (h *SettingsHandler) UpdateGlobalAllowDestructiveActions(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupAllowDestructive)
	h.updateGlobalSettingsWithKeys(c, allowedKeys)
}

// UpdateGlobalRequireCommandJustification updates whether every command in every app needs a justification.
func (h *SettingsHandler) UpdateGlobalRequireCommandJustification(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupCommandJustification)
	h.updateGlobalSettingsWithKeys(c, allowedKeys)
}

// UpdateGlobalVCSAndCIDefaults updates VCS and CI default settings.
func (h *SettingsHandler) UpdateGlobalVCSAndCIDefaults(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupVCSAndCIDefaults)
	h.updateGlobalSettingsWithKeys(c, allowedKeys)
}

// UpdateGlobalDeployApprovals updates deploy approval settings.
func (h *SettingsHandler) UpdateGlobalDeployApprovals(c *gin.Context) {
	h.updateGlobalSettingsWithKeys(c, settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupDeployApprovals))
}

// UpdateGlobalSessionConfiguration updates session configuration settings.
func (h *SettingsHandler) UpdateGlobalSessionConfiguration(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupSessionConfiguration)
	h.updateGlobalSettingsWithKeys(c, allowedKeys)
}

func (h *SettingsHandler) updateGlobalSettingsWithKeys(c *gin.Context, allowedKeys []string) {
	h.updateSettings(c, &globalSettingsOps{service: h.settingsService}, "", allowedKeys)
}

// DeleteGlobalSettings godoc
// @Summary Delete multiple global settings
// @Description Deletes global settings' database values; their environment variables or defaults apply
// @Tags Settings
// @Produce json
// @Param key query []string true "Setting keys to delete"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /settings [delete]
func (h *SettingsHandler) DeleteGlobalSettings(c *gin.Context) {
	h.deleteGlobalSettingsWithKeys(c, nil)
}

// DeleteGlobalMFAConfiguration deletes MFA configuration settings.
func (h *SettingsHandler) DeleteGlobalMFAConfiguration(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupMFAConfiguration)
	h.deleteGlobalSettingsWithKeys(c, allowedKeys)
}

// DeleteGlobalAllowDestructiveActions deletes allow destructive actions settings.
func (h *SettingsHandler) DeleteGlobalAllowDestructiveActions(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupAllowDestructive)
	h.deleteGlobalSettingsWithKeys(c, allowedKeys)
}

// DeleteGlobalRequireCommandJustification deletes the gateway-wide require_command_justification.
func (h *SettingsHandler) DeleteGlobalRequireCommandJustification(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupCommandJustification)
	h.deleteGlobalSettingsWithKeys(c, allowedKeys)
}

// DeleteGlobalVCSAndCIDefaults deletes VCS and CI default settings.
func (h *SettingsHandler) DeleteGlobalVCSAndCIDefaults(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupVCSAndCIDefaults)
	h.deleteGlobalSettingsWithKeys(c, allowedKeys)
}

// DeleteGlobalDeployApprovals deletes deploy approval settings.
func (h *SettingsHandler) DeleteGlobalDeployApprovals(c *gin.Context) {
	h.deleteGlobalSettingsWithKeys(c, settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupDeployApprovals))
}

// DeleteGlobalSessionConfiguration deletes session configuration settings.
func (h *SettingsHandler) DeleteGlobalSessionConfiguration(c *gin.Context) {
	allowedKeys := settings.GlobalSettingGroupKeyStrings(settings.GlobalSettingGroupSessionConfiguration)
	h.deleteGlobalSettingsWithKeys(c, allowedKeys)
}

func (h *SettingsHandler) deleteGlobalSettingsWithKeys(c *gin.Context, allowedKeys []string) {
	h.deleteSettings(c, &globalSettingsOps{service: h.settingsService}, "", allowedKeys)
}

// GetAllAppSettings godoc
// @Summary Get all app settings
// @Description Returns all settings for a specific app with their sources
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Success 200 {object} map[string]settings.Setting
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /apps/{app}/settings [get]
func (h *SettingsHandler) GetAllAppSettings(c *gin.Context) {
	appName := c.Param("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name is required"})
		return
	}

	allSettings, err := h.settingsService.GetAllAppSettings(appName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get settings"})
		return
	}

	c.JSON(http.StatusOK, allSettings)
}

// UpdateAppSettings godoc
// @Summary Update multiple app settings
// @Description Updates multiple app settings atomically and stores them in the database
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param settings body map[string]interface{} true "Map of setting keys to values"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings [put]
func (h *SettingsHandler) UpdateAppSettings(c *gin.Context) {
	h.updateAppSettingsWithKeys(c, nil)
}

// UpdateAppVCSCIDeploySettings updates VCS/CI deployment settings for an app.
func (h *SettingsHandler) UpdateAppVCSCIDeploySettings(c *gin.Context) {
	h.updateAppSettingsWithKeys(c, settings.AppSettingGroupKeyStrings(settings.AppSettingGroupVCSCIDeploy))
}

func (h *SettingsHandler) updateAppSettingsWithKeys(c *gin.Context, allowedKeys []string) {
	appName := c.Param("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name is required"})
		return
	}
	h.updateSettings(c, &appSettingsOps{service: h.settingsService}, appName, allowedKeys)
}

// DeleteAppSettings godoc
// @Summary Delete multiple app settings
// @Description Deletes app settings' database values; their environment variables or defaults apply
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Param key query []string true "Setting keys to delete"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings [delete]
func (h *SettingsHandler) DeleteAppSettings(c *gin.Context) {
	h.deleteAppSettingsWithKeys(c, nil)
}

// DeleteAppVCSCIDeploySettings deletes VCS/CI deployment settings for an app.
func (h *SettingsHandler) DeleteAppVCSCIDeploySettings(c *gin.Context) {
	h.deleteAppSettingsWithKeys(c, settings.AppSettingGroupKeyStrings(settings.AppSettingGroupVCSCIDeploy))
}

func (h *SettingsHandler) deleteAppSettingsWithKeys(c *gin.Context, allowedKeys []string) {
	appName := c.Param("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name is required"})
		return
	}
	h.deleteSettings(c, &appSettingsOps{service: h.settingsService}, appName, allowedKeys)
}
