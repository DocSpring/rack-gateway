package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// Service provides settings resolution with environment variable fallback.
// Resolution order: Database -> Environment Variable -> Default
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
	EnvVar string        `json:"env_var,omitempty"` // e.g., "RGW_SETTING_REQUIRE_MFA_ALL_USERS"
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

// parseEnvValue attempts to parse an environment variable value as JSON.
// Falls back to treating it as a plain string if JSON parsing fails.
func parseEnvValue(envValue string, targetType interface{}) (interface{}, error) {
	// Try to unmarshal as JSON first
	if err := json.Unmarshal([]byte(envValue), &targetType); err == nil {
		return targetType, nil
	}

	// For booleans, try parsing
	if _, ok := targetType.(bool); ok {
		if val, err := strconv.ParseBool(envValue); err == nil {
			return val, nil
		}
	}

	// For strings, return as-is
	if _, ok := targetType.(string); ok {
		return envValue, nil
	}

	// For arrays, try splitting by comma
	if _, ok := targetType.([]string); ok {
		if envValue == "" {
			return []string{}, nil
		}
		parts := strings.Split(envValue, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result, nil
	}

	return nil, fmt.Errorf("unsupported type for env value")
}

// getSetting retrieves a setting with environment fallback.
// targetType is used to determine how to parse the value (e.g., bool, string, []string).
func (s *Service) getSetting(appName *string, key string, defaultValue interface{}) (*Setting, error) {
	return s.resolveSetting(appName, key, defaultValue, nil)
}

// resolveSetting retrieves a setting using either a preloaded map of DB values or
// by querying the database directly, then falling back to env/default values.
func (s *Service) resolveSetting(
	appName *string,
	key string,
	defaultValue interface{},
	preloaded map[string][]byte,
) (*Setting, error) {
	envVarName := getEnvVarName(appName, key)

	// 1. Check database (preloaded map avoids multiple round-trips)
	raw, found, err := s.loadSettingValue(appName, key, preloaded)
	if err != nil {
		return nil, err
	}
	if found {
		var value interface{}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("failed to unmarshal setting %s: %w", key, err)
		}
		return &Setting{Value: value, Source: SourceDB, EnvVar: envVarName}, nil
	}

	// 2. Check environment variable
	if envValue := strings.TrimSpace(os.Getenv(envVarName)); envValue != "" {
		parsedValue, err := parseEnvValue(envValue, defaultValue)
		if err == nil {
			return &Setting{
				Value:  parsedValue,
				Source: SourceEnv,
				EnvVar: envVarName,
			}, nil
		}
		// If parsing fails, log and fall through to default
		gtwlog.Warnf("settings: failed to parse env var %s: %v", envVarName, err)
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
