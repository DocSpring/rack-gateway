package settings

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Service provides settings resolution.
// Resolution order: Environment Variable -> Database -> Default. A setting an environment variable defines can't
// be changed through the gateway.
type Service struct {
	db *db.Database
}

// NewService creates a new settings service.
func NewService(database *db.Database) *Service {
	return &Service{db: database}
}

// SettingSource indicates where a setting value came from.
type SettingSource string

// Setting sources describe where a resolved value originated.
const (
	SourceDB            SettingSource = "db"
	SourceEnv           SettingSource = "env"
	SourceDefault       SettingSource = "default"
	SourceGlobalDefault SettingSource = "global_default"
)

// Setting represents a resolved setting value with its source.
type Setting struct {
	Value  interface{}   `json:"value"`
	Source SettingSource `json:"source"`
	EnvVar string        `json:"env_var,omitempty"` // e.g., "RGW_SETTING_MFA_REQUIRE_ALL_USERS"
	// StoredValueIgnored reports a database value that the environment variable overrides. It has no effect, and
	// can be cleared.
	StoredValueIgnored bool `json:"stored_value_ignored,omitempty"`
}

// normalizeAppNameForEnv converts app name to environment variable format.
// Example: "my-service" -> "MY_SERVICE"
func normalizeAppNameForEnv(appName string) string {
	return strings.ToUpper(strings.ReplaceAll(appName, "-", "_"))
}

// getEnvVarName returns the environment variable name for a setting.
func getEnvVarName(appName *string, key string) string {
	normalizedKey := strings.ToUpper(key)
	if appName == nil {
		return fmt.Sprintf("RGW_SETTING_%s", normalizedKey)
	}
	normalizedApp := normalizeAppNameForEnv(*appName)
	return fmt.Sprintf("RGW_APP_%s_SETTING_%s", normalizedApp, normalizedKey)
}

// getSetting resolves a setting: its environment variable, then the database, then the default.
func (s *Service) getSetting(appName *string, key string, defaultValue interface{}) (*Setting, error) {
	return s.resolveSetting(appName, key, defaultValue, nil)
}

// resolveSetting resolves a setting: its environment variable, then the database value (from preloaded, or a
// query), then the default.
func (s *Service) resolveSetting(
	appName *string,
	key string,
	defaultValue interface{},
	preloaded map[string][]byte,
) (*Setting, error) {
	raw, found, err := s.loadSettingValue(appName, key, preloaded)
	if err != nil {
		return nil, err
	}

	// 1. An environment variable (the deployment config) overrides the database
	if setting, ok, err := envSetting(appName, key); ok {
		if err != nil {
			return nil, err
		}
		setting.StoredValueIgnored = found
		return setting, nil
	}
	envVarName := getEnvVarName(appName, key)

	// 2. The database value
	if found {
		var value interface{}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("failed to unmarshal setting %s: %w", key, err)
		}
		return &Setting{Value: value, Source: SourceDB, EnvVar: envVarName}, nil
	}

	// 3. Return default
	return &Setting{Value: defaultValue, Source: SourceDefault, EnvVar: envVarName}, nil
}

// loadSettingValue returns the raw DB value for a setting, preferring a preloaded
// map when provided to avoid repeated queries.
func (s *Service) loadSettingValue(
	appName *string,
	key string,
	preloaded map[string][]byte,
) ([]byte, bool, error) {
	if preloaded != nil {
		raw, ok := preloaded[key]
		return raw, ok, nil
	}

	raw, found, err := s.db.GetSetting(appName, key)
	if err != nil {
		return nil, false, err
	}
	return raw, found, nil
}
