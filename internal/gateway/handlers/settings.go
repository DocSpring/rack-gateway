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
	defaultValue := getDefaultValueForGlobalKey(key)
	if defaultValue == nil {
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

// UpdateGlobalSetting godoc
// @Summary Update a global setting
// @Description Updates a single global setting and stores it in the database
// @Tags Settings
// @Accept json
// @Produce json
// @Param key path string true "Setting key"
// @Param value body interface{} true "Setting value"
// @Success 200 {object} settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/{key} [put]
func (h *SettingsHandler) UpdateGlobalSetting(c *gin.Context) {
	email := c.GetString("user_email")
	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	// Check if key is valid
	if !isValidGlobalKey(key) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown setting key"})
		return
	}

	// Parse request body as raw JSON value
	var value interface{}
	if err := c.ShouldBindJSON(&value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Get user ID for audit
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	// Save to database
	if err := h.settingsService.SetGlobalSetting(key, value, uid); err != nil {
		fmt.Printf("ERROR saving setting %s: %v\n", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
		return
	}

	// Return updated setting
	defaultValue := getDefaultValueForGlobalKey(key)
	setting, err := h.settingsService.GetGlobalSetting(key, defaultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get setting"})
		return
	}

	c.JSON(http.StatusOK, setting)
}

// DeleteGlobalSetting godoc
// @Summary Delete a global setting
// @Description Deletes a global setting from the database, reverting to env or default value
// @Tags Settings
// @Produce json
// @Param key path string true "Setting key"
// @Success 200 {object} settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /admin/settings/{key} [delete]
func (h *SettingsHandler) DeleteGlobalSetting(c *gin.Context) {
	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	// Check if key is valid
	if !isValidGlobalKey(key) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown setting key"})
		return
	}

	// Delete the setting
	if err := h.settingsService.DeleteGlobalSetting(key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete setting"})
		return
	}

	// Return the setting after deletion (will show env or default source)
	defaultValue := getDefaultValueForGlobalKey(key)
	setting, err := h.settingsService.GetGlobalSetting(key, defaultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get setting"})
		return
	}

	c.JSON(http.StatusOK, setting)
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
	defaultValue := getDefaultValueForAppKey(key)
	if defaultValue == nil {
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

// UpdateAppSetting godoc
// @Summary Update an app setting
// @Description Updates a single app setting and stores it in the database
// @Tags Settings
// @Accept json
// @Produce json
// @Param app path string true "App name"
// @Param key path string true "Setting key"
// @Param value body interface{} true "Setting value"
// @Success 200 {object} settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/{key} [put]
func (h *SettingsHandler) UpdateAppSetting(c *gin.Context) {
	email := c.GetString("user_email")
	appName := c.Param("app")
	key := c.Param("key")
	if appName == "" || key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name and key are required"})
		return
	}

	// Check if key is valid
	if !isValidAppKey(key) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown setting key"})
		return
	}

	// Parse request body as raw JSON value
	var value interface{}
	if err := c.ShouldBindJSON(&value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Get user ID for audit
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	// Save to database
	if err := h.settingsService.SetAppSetting(appName, key, value, uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save setting"})
		return
	}

	// Return updated setting
	defaultValue := getDefaultValueForAppKey(key)
	setting, err := h.settingsService.GetAppSetting(appName, key, defaultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get setting"})
		return
	}

	c.JSON(http.StatusOK, setting)
}

// DeleteAppSetting godoc
// @Summary Delete an app setting
// @Description Deletes an app setting from the database, reverting to env or default value
// @Tags Settings
// @Produce json
// @Param app path string true "App name"
// @Param key path string true "Setting key"
// @Success 200 {object} settings.Setting
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security SessionCookie
// @Security CSRFToken
// @Router /apps/{app}/settings/{key} [delete]
func (h *SettingsHandler) DeleteAppSetting(c *gin.Context) {
	appName := c.Param("app")
	key := c.Param("key")
	if appName == "" || key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app name and key are required"})
		return
	}

	// Check if key is valid
	if !isValidAppKey(key) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown setting key"})
		return
	}

	// Delete the setting
	if err := h.settingsService.DeleteAppSetting(appName, key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete setting"})
		return
	}

	// Return the setting after deletion (will show env or default source)
	defaultValue := getDefaultValueForAppKey(key)
	setting, err := h.settingsService.GetAppSetting(appName, key, defaultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get setting"})
		return
	}

	c.JSON(http.StatusOK, setting)
}

// getDefaultValueForGlobalKey returns the default value for a global setting key.
func getDefaultValueForGlobalKey(key string) interface{} {
	switch key {
	case settings.KeyMFARequireAllUsers:
		return true
	case settings.KeyTrustedDeviceTTLDays:
		return 30
	case settings.KeyStepUpWindowMinutes:
		return 10
	case settings.KeyAllowDestructiveActions:
		return false
	default:
		return nil
	}
}

// getDefaultValueForAppKey returns the default value for an app setting key.
func getDefaultValueForAppKey(key string) interface{} {
	switch key {
	case settings.KeyApprovedDeployCommands:
		return []string(nil)
	case settings.KeyProtectedEnvVars:
		return []string(nil)
	case settings.KeySecretEnvVars:
		return []string(nil)
	case settings.KeyServiceImagePatterns:
		return map[string]string(nil)
	case settings.KeyGitHubVerification:
		return true
	case settings.KeyAllowDeployFromDefaultBranch:
		return false
	case settings.KeyDefaultBranch:
		return "main"
	case settings.KeyRequirePRForBranch:
		return true
	case settings.KeyVerifyGitCommitMode:
		return "latest"
	default:
		return nil
	}
}

// isValidGlobalKey checks if a key is a valid global setting.
func isValidGlobalKey(key string) bool {
	validKeys := map[string]bool{
		settings.KeyMFARequireAllUsers:      true,
		settings.KeyTrustedDeviceTTLDays:    true,
		settings.KeyStepUpWindowMinutes:     true,
		settings.KeyAllowDestructiveActions: true,
	}
	return validKeys[key]
}

// isValidAppKey checks if a key is a valid app setting.
func isValidAppKey(key string) bool {
	validKeys := map[string]bool{
		settings.KeyApprovedDeployCommands:       true,
		settings.KeyProtectedEnvVars:             true,
		settings.KeySecretEnvVars:                true,
		settings.KeyServiceImagePatterns:         true,
		settings.KeyGitHubVerification:           true,
		settings.KeyAllowDeployFromDefaultBranch: true,
		settings.KeyDefaultBranch:                true,
		settings.KeyRequirePRForBranch:           true,
		settings.KeyVerifyGitCommitMode:          true,
	}
	return validKeys[key]
}
