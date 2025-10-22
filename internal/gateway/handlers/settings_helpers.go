package handlers

import (
	"fmt"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
	"github.com/gin-gonic/gin"
)

// settingsOperations defines operations for global or app settings.
type settingsOperations interface {
	isValid(key string) bool
	getDefault(key string) (interface{}, error)
	getSetting(appName, key string, defaultValue interface{}) (*settings.Setting, error)
	setSetting(appName, key string, value interface{}, uid *int64) error
	deleteSetting(appName, key string) error
}

// globalSettingsOps implements settingsOperations for global settings.
type globalSettingsOps struct {
	service *settings.Service
}

func (o *globalSettingsOps) isValid(key string) bool {
	return settings.IsValidGlobalSetting(key)
}

func (o *globalSettingsOps) getDefault(key string) (interface{}, error) {
	return settings.GetGlobalSettingDefault(key)
}

func (o *globalSettingsOps) getSetting(appName, key string, defaultValue interface{}) (*settings.Setting, error) {
	return o.service.GetGlobalSetting(key, defaultValue)
}

func (o *globalSettingsOps) setSetting(appName, key string, value interface{}, uid *int64) error {
	return o.service.SetGlobalSetting(key, value, uid)
}

func (o *globalSettingsOps) deleteSetting(appName, key string) error {
	return o.service.DeleteGlobalSetting(key)
}

// appSettingsOps implements settingsOperations for app settings.
type appSettingsOps struct {
	service *settings.Service
}

func (o *appSettingsOps) isValid(key string) bool {
	return settings.IsValidAppSetting(key)
}

func (o *appSettingsOps) getDefault(key string) (interface{}, error) {
	return settings.GetAppSettingDefault(key)
}

func (o *appSettingsOps) getSetting(appName, key string, defaultValue interface{}) (*settings.Setting, error) {
	return o.service.GetAppSetting(appName, key, defaultValue)
}

func (o *appSettingsOps) setSetting(appName, key string, value interface{}, uid *int64) error {
	return o.service.SetAppSetting(appName, key, value, uid)
}

func (o *appSettingsOps) deleteSetting(appName, key string) error {
	return o.service.DeleteAppSetting(appName, key)
}

// updateSettings handles updating multiple settings (global or app).
func (h *SettingsHandler) updateSettings(c *gin.Context, ops settingsOperations, appName string, allowedKeys []string) {
	email := c.GetString("user_email")

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no settings provided"})
		return
	}

	allowed := buildKeySet(allowedKeys)

	// Validate all keys first
	for key := range updates {
		if !isAllowedKey(allowed, key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("setting %s is not managed by this endpoint", key)})
			return
		}
		if !ops.isValid(key) {
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
		if err := ops.setSetting(appName, key, value, uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save setting: %s", key)})
			return
		}
	}

	// Return all updated settings
	result := make(map[string]settings.Setting)
	for key := range updates {
		defaultValue, err := ops.getDefault(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get default for: %s", key)})
			return
		}
		setting, err := ops.getSetting(appName, key, defaultValue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get setting: %s", key)})
			return
		}
		result[key] = *setting
	}

	c.JSON(http.StatusOK, result)
}

// deleteSettings handles deleting multiple settings (global or app).
func (h *SettingsHandler) deleteSettings(c *gin.Context, ops settingsOperations, appName string, allowedKeys []string) {
	keys := c.QueryArray("key")
	if len(keys) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one key is required"})
		return
	}

	allowed := buildKeySet(allowedKeys)

	// Validate all keys first
	for _, key := range keys {
		if !isAllowedKey(allowed, key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("setting %s is not managed by this endpoint", key)})
			return
		}
		if !ops.isValid(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown setting key: %s", key)})
			return
		}
	}

	// Delete all settings
	for _, key := range keys {
		if err := ops.deleteSetting(appName, key); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete setting: %s", key)})
			return
		}
	}

	// Return all settings after deletion
	result := make(map[string]settings.Setting)
	for _, key := range keys {
		defaultValue, err := ops.getDefault(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get default for: %s", key)})
			return
		}
		setting, err := ops.getSetting(appName, key, defaultValue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get setting: %s", key)})
			return
		}
		result[key] = *setting
	}

	c.JSON(http.StatusOK, result)
}

// getSingleSettingResponse gets a single setting and returns it as a map response.
func (h *SettingsHandler) getSingleSettingResponse(c *gin.Context, ops settingsOperations, appName, key string) {
	defaultValue, err := ops.getDefault(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get default for: %s", key)})
		return
	}

	setting, err := ops.getSetting(appName, key, defaultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get setting: %s", key)})
		return
	}

	c.JSON(http.StatusOK, map[string]settings.Setting{
		key: *setting,
	})
}
