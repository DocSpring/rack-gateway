package handlers

import (
	"fmt"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

// SettingsHandler handles generic settings API endpoints.
type SettingsHandler struct {
	settingsService *settings.Service
	rbac            rbac.RBACManager
}

// NewSettingsHandler creates a new settings handler.
func NewSettingsHandler(settingsService *settings.Service, rbacMgr rbac.RBACManager) *SettingsHandler {
	return &SettingsHandler{
		settingsService: settingsService,
		rbac:            rbacMgr,
	}
}

// GetAllGlobalSettings godoc
// @Summary Get all global settings
// @Description Returns all global settings with their sources (db, env, or default)
// @Tags Settings
// @Produce json
// @Success 200 {object} map[string]settings.Setting
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /admin/settings [get]
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
// @Router /admin/settings/{key} [get]
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
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings [put]
func (h *SettingsHandler) UpdateGlobalSettings(c *gin.Context) {
	email := c.GetString("user_email")

	// Parse request body as map of keys to values
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no settings provided"})
		return
	}

	// Validate all keys first
	for key := range updates {
		if !settings.IsValidGlobalSetting(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown setting key: %s", key)})
			return
		}
	}

	// Get user ID for audit
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	// Save all settings
	for key, value := range updates {
		if err := h.settingsService.SetGlobalSetting(key, value, uid); err != nil {
			fmt.Printf("ERROR saving setting %s: %v\n", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save setting: %s", key)})
			return
		}
	}

	// Return all updated settings
	result := make(map[string]settings.Setting)
	for key := range updates {
		defaultValue, err := settings.GetGlobalSettingDefault(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get default for: %s", key)})
			return
		}
		setting, err := h.settingsService.GetGlobalSetting(key, defaultValue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get setting: %s", key)})
			return
		}
		result[key] = *setting
	}

	c.JSON(http.StatusOK, result)
}

// DeleteGlobalSettings godoc
// @Summary Delete multiple global settings
// @Description Deletes multiple global settings from the database, reverting to env or default values
// @Tags Settings
// @Produce json
// @Param key query []string true "Setting keys to delete"
// @Success 200 {object} map[string]settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings [delete]
func (h *SettingsHandler) DeleteGlobalSettings(c *gin.Context) {
	// Get keys from query params (supports multiple ?key=foo&key=bar)
	keys := c.QueryArray("key")
	if len(keys) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one key is required"})
		return
	}

	// Validate all keys first
	for _, key := range keys {
		if !settings.IsValidGlobalSetting(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown setting key: %s", key)})
			return
		}
	}

	// Delete all settings
	for _, key := range keys {
		if err := h.settingsService.DeleteGlobalSetting(key); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete setting: %s", key)})
			return
		}
	}

	// Return all settings after deletion (will show env or default sources)
	result := make(map[string]settings.Setting)
	for _, key := range keys {
		defaultValue, err := settings.GetGlobalSettingDefault(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get default for: %s", key)})
			return
		}
		setting, err := h.settingsService.GetGlobalSetting(key, defaultValue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get setting: %s", key)})
			return
		}
		result[key] = *setting
	}

	c.JSON(http.StatusOK, result)
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

// GetAppSetting godoc
// @Summary Get a specific app setting
// @Description Returns a single app setting with its source
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Param key path string true "Setting key"
// @Success 200 {object} settings.Setting
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Router /apps/{app}/settings/{key} [get]
func (h *SettingsHandler) GetAppSetting(c *gin.Context) {
	appName := c.Param("app")
	key := c.Param("key")
	if appName == "" || key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name and key are required"})
		return
	}

	// Get default value based on key
	defaultValue, err := settings.GetAppSettingDefault(key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown setting key"})
		return
	}

	setting, err := h.settingsService.GetAppSetting(appName, key, defaultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get setting"})
		return
	}

	c.JSON(http.StatusOK, setting)
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
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings [put]
func (h *SettingsHandler) UpdateAppSettings(c *gin.Context) {
	email := c.GetString("user_email")
	appName := c.Param("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name is required"})
		return
	}

	// Parse request body as map of keys to values
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no settings provided"})
		return
	}

	// Validate all keys first
	for key := range updates {
		if !settings.IsValidAppSetting(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown setting key: %s", key)})
			return
		}
	}

	// Get user ID for audit
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	// Save all settings
	for key, value := range updates {
		if err := h.settingsService.SetAppSetting(appName, key, value, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save setting: %s", key)})
			return
		}
	}

	// Return all updated settings
	result := make(map[string]settings.Setting)
	for key := range updates {
		defaultValue, err := settings.GetAppSettingDefault(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get default for: %s", key)})
			return
		}
		setting, err := h.settingsService.GetAppSetting(appName, key, defaultValue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get setting: %s", key)})
			return
		}
		result[key] = *setting
	}

	c.JSON(http.StatusOK, result)
}

// DeleteAppSettings godoc
// @Summary Delete multiple app settings
// @Description Deletes multiple app settings from the database, reverting to env or default values
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
	appName := c.Param("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name is required"})
		return
	}

	// Get keys from query params (supports multiple ?key=foo&key=bar)
	keys := c.QueryArray("key")
	if len(keys) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one key is required"})
		return
	}

	// Validate all keys first
	for _, key := range keys {
		if !settings.IsValidAppSetting(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown setting key: %s", key)})
			return
		}
	}

	// Delete all settings
	for _, key := range keys {
		if err := h.settingsService.DeleteAppSetting(appName, key); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete setting: %s", key)})
			return
		}
	}

	// Return all settings after deletion (will show env or default sources)
	result := make(map[string]settings.Setting)
	for _, key := range keys {
		defaultValue, err := settings.GetAppSettingDefault(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get default for: %s", key)})
			return
		}
		setting, err := h.settingsService.GetAppSetting(appName, key, defaultValue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get setting: %s", key)})
			return
		}
		result[key] = *setting
	}

	c.JSON(http.StatusOK, result)
}
