package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// settingsOperations defines operations for global or app settings.
type settingsOperations interface {
	isValid(key string) bool
	getDefault(key string) (interface{}, error)
	getSetting(appName, key string, defaultValue interface{}) (*settings.Setting, error)
	setSettings(appName string, values map[string]interface{}, uid *int64) error
	deleteSettings(appName string, keys []string) error
}

// globalSettingsOps implements settingsOperations for global settings.
type globalSettingsOps struct {
	service *settings.Service
}

func (_ *globalSettingsOps) isValid(key string) bool {
	return settings.IsValidGlobalSetting(key)
}

func (_ *globalSettingsOps) getDefault(key string) (interface{}, error) {
	return settings.GetGlobalSettingDefault(key)
}

func (o *globalSettingsOps) getSetting(_ string, key string, defaultValue interface{}) (*settings.Setting, error) {
	return o.service.GetGlobalSetting(key, defaultValue)
}

func (o *globalSettingsOps) setSettings(_ string, values map[string]interface{}, uid *int64) error {
	return o.service.SetGlobalSettings(values, uid)
}

func (o *globalSettingsOps) deleteSettings(_ string, keys []string) error {
	return o.service.DeleteGlobalSettings(keys)
}

// appSettingsOps implements settingsOperations for app settings.
type appSettingsOps struct {
	service *settings.Service
}

func (_ *appSettingsOps) isValid(key string) bool {
	return settings.IsValidAppSetting(key)
}

func (_ *appSettingsOps) getDefault(key string) (interface{}, error) {
	return settings.GetAppSettingDefault(key)
}

func (o *appSettingsOps) getSetting(appName, key string, defaultValue interface{}) (*settings.Setting, error) {
	return o.service.GetAppSetting(appName, key, defaultValue)
}

func (o *appSettingsOps) setSettings(appName string, values map[string]interface{}, uid *int64) error {
	return o.service.SetAppSettings(appName, values, uid)
}

func (o *appSettingsOps) deleteSettings(appName string, keys []string) error {
	return o.service.DeleteAppSettings(appName, keys)
}

// validateSettingKeys validates a list of keys against allowed keys and operations.
// Returns an error message if validation fails, empty string otherwise.
func validateSettingKeys(keys []string, allowed map[string]struct{}, ops settingsOperations) string {
	for _, key := range keys {
		if !isAllowedKey(allowed, key) {
			return fmt.Sprintf("setting %s is not managed by this endpoint", key)
		}
		if !ops.isValid(key) {
			return fmt.Sprintf("unknown setting key: %s", key)
		}
	}
	return ""
}

// buildSettingsResponse builds a response map for a list of keys by fetching settings with defaults.
// Returns the result map and an error message if any operation fails.
func buildSettingsResponse(
	ops settingsOperations,
	appName string,
	keys []string,
) (map[string]settings.Setting, string) {
	result := make(map[string]settings.Setting)
	for _, key := range keys {
		defaultValue, err := ops.getDefault(key)
		if err != nil {
			return nil, fmt.Sprintf("failed to get default for: %s", key)
		}
		setting, err := ops.getSetting(appName, key, defaultValue)
		if err != nil {
			return nil, fmt.Sprintf("failed to get setting: %s", key)
		}
		result[key] = *setting
	}
	return result, ""
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

	// Extract keys from updates map for validation
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}

	allowed := buildKeySet(allowedKeys)

	// Validate all keys first
	if errMsg := validateSettingKeys(keys, allowed, ops); errMsg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		return
	}

	// Get user ID for audit
	var uid *int64
	if h.rbac != nil {
		if u, err := h.rbac.GetUserWithID(email); err == nil && u != nil {
			uid = &u.ID
		}
	}

	// Every value is checked, then they're saved in one transaction: the request changes all of them or none
	save := func() error { return ops.setSettings(appName, updates, uid) }
	if err := changeSettings(c, ops, appName, keys, save); err != nil {
		respondSettingSaveError(c, err)
		return
	}

	// Return updated settings (full group when restricted, otherwise just the keys touched)
	keysToReturn := keys
	if len(allowedKeys) > 0 {
		keysToReturn = allowedKeys
	}
	result, errMsg := buildSettingsResponse(ops, appName, keysToReturn)
	if errMsg != "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// deleteSettings handles deleting multiple settings (global or app). On a group route (allowedKeys set), no
// keys means every key in the group.
func (_ *SettingsHandler) deleteSettings(c *gin.Context, ops settingsOperations, appName string, allowedKeys []string) {
	keys := c.QueryArray("key")
	if len(keys) == 0 {
		keys = allowedKeys
	}
	if len(keys) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one key is required"})
		return
	}

	allowed := buildKeySet(allowedKeys)

	// Validate all keys first
	if errMsg := validateSettingKeys(keys, allowed, ops); errMsg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		return
	}

	// Delete them together, in one statement
	remove := func() error { return ops.deleteSettings(appName, keys) }
	if err := changeSettings(c, ops, appName, keys, remove); err != nil {
		gtwlog.Errorf("settings: failed to delete %s: %v", strings.Join(keys, ", "), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete settings"})
		return
	}

	// Return all settings after deletion
	result, errMsg := buildSettingsResponse(ops, appName, keys)
	if errMsg != "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// changeSettings runs change, which saves or deletes keys, and records each key's value before and after it in
// the request's audit result ({"changes": {key: {"from": ..., "to": ...}}}), so the audit log shows what every
// settings change changed. No setting holds a secret. When the values can't be read first, nothing is changed.
func changeSettings(
	c *gin.Context,
	ops settingsOperations,
	appName string,
	keys []string,
	change func() error,
) error {
	before, errMsg := buildSettingsResponse(ops, appName, keys)
	if errMsg != "" {
		return errors.New(errMsg)
	}
	if err := change(); err != nil {
		return err
	}
	after, errMsg := buildSettingsResponse(ops, appName, keys)
	if errMsg != "" {
		gtwlog.Errorf("settings: changed %s but can't read the new values: %s", strings.Join(keys, ", "), errMsg)
	}
	changes := make(map[string]map[string]interface{}, len(keys))
	for _, key := range keys {
		changes[key] = map[string]interface{}{"from": before[key].Value}
		if after != nil {
			changes[key]["to"] = after[key].Value
		}
	}
	if attempt := audit.AttemptFromContext(c.Request.Context()); attempt != nil {
		attempt.SetResultDetail("changes", changes)
	}
	return nil
}

// getSingleSettingResponse gets a single setting and returns it as a map response.
func (_ *SettingsHandler) getSingleSettingResponse(c *gin.Context, ops settingsOperations, appName, key string) {
	result, errMsg := buildSettingsResponse(ops, appName, []string{key})
	if errMsg != "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// respondSettingSaveError answers 400 for a value with the wrong shape, 409 for a setting an environment variable
// defines, and 500 for anything else.
func respondSettingSaveError(c *gin.Context, err error) {
	if errors.Is(err, settings.ErrInvalidSettingValue) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, settings.ErrSetByEnvironment) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	gtwlog.Errorf("settings: failed to save: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
}
