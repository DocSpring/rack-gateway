package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
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

const (
	SourceDB      SettingSource = "db"
	SourceEnv     SettingSource = "env"
	SourceDefault SettingSource = "default"
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
	envVarName := getEnvVarName(appName, key)

	// 1. Check database first
	raw, found, err := s.db.GetSetting(appName, key)
	if err != nil {
		return nil, err
	}
	if found {
		var value interface{}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("failed to unmarshal setting %s: %w", key, err)
		}
		return &Setting{
			Value:  value,
			Source: SourceDB,
		}, nil
	}

	// 2. Check environment variable
	if envValue := strings.TrimSpace(os.Getenv(envVarName)); envValue != "" {
		parsedValue, err := parseEnvValue(envValue, defaultValue)
		if err != nil {
			// If parsing fails, log and fall through to default
			fmt.Printf("Warning: failed to parse env var %s: %v\n", envVarName, err)
		} else {
			return &Setting{
				Value:  parsedValue,
				Source: SourceEnv,
				EnvVar: envVarName,
			}, nil
		}
	}

	// 3. Return default
	return &Setting{
		Value:  defaultValue,
		Source: SourceDefault,
	}, nil
}

// GetGlobalSetting retrieves a global setting.
func (s *Service) GetGlobalSetting(key string, defaultValue interface{}) (*Setting, error) {
	return s.getSetting(nil, key, defaultValue)
}

// GetAppSetting retrieves an app-specific setting.
func (s *Service) GetAppSetting(appName, key string, defaultValue interface{}) (*Setting, error) {
	return s.getSetting(&appName, key, defaultValue)
}

// Global setting keys
const (
	KeyMFARequireAllUsers      = "mfa_require_all_users"
	KeyTrustedDeviceTTLDays    = "mfa_trusted_device_ttl_days"
	KeyStepUpWindowMinutes     = "mfa_step_up_window_minutes"
	KeyAllowDestructiveActions = "allow_destructive_actions"
)

// App setting keys
const (
	KeyApprovedDeployCommands        = "approved_deploy_commands"
	KeyProtectedEnvVars              = "protected_env_vars"
	KeySecretEnvVars                 = "secret_env_vars"
	KeyServiceImagePatterns          = "service_image_patterns"
	KeyGitHubVerification            = "github_verification"
	KeyAllowDeployFromDefaultBranch  = "allow_deploy_from_default_branch"
	KeyDefaultBranch                 = "default_branch"
	KeyRequirePRForBranch            = "require_pr_for_branch"
	KeyVerifyGitCommitMode           = "verify_git_commit_mode"
	KeyCircleCIApprovalJobName       = "circleci_approval_job_name"
	KeyCircleCIAutoApproveOnApproval = "circleci_auto_approve_on_approval"
)

// DefaultGlobalSettings defines all valid global settings with their default values.
var DefaultGlobalSettings = map[string]interface{}{
	KeyMFARequireAllUsers:      true,
	KeyTrustedDeviceTTLDays:    30,
	KeyStepUpWindowMinutes:     10,
	KeyAllowDestructiveActions: false,
}

// DefaultAppSettings defines all valid app-specific settings with their default values.
var DefaultAppSettings = map[string]interface{}{
	KeyApprovedDeployCommands:        []string(nil),
	KeyProtectedEnvVars:              []string(nil),
	KeySecretEnvVars:                 []string(nil),
	KeyServiceImagePatterns:          map[string]string(nil),
	KeyGitHubVerification:            true,
	KeyAllowDeployFromDefaultBranch:  false,
	KeyDefaultBranch:                 "main",
	KeyRequirePRForBranch:            true,
	KeyVerifyGitCommitMode:           "latest",
	KeyCircleCIApprovalJobName:       "",
	KeyCircleCIAutoApproveOnApproval: false,
}

// IsValidGlobalSetting checks if a key is a valid global setting.
func IsValidGlobalSetting(key string) bool {
	_, exists := DefaultGlobalSettings[key]
	return exists
}

// IsValidAppSetting checks if a key is a valid app-specific setting.
func IsValidAppSetting(key string) bool {
	_, exists := DefaultAppSettings[key]
	return exists
}

// GetGlobalSettingDefault returns the default value for a global setting.
// Returns error if the setting key is unknown.
func GetGlobalSettingDefault(key string) (interface{}, error) {
	defaultValue, exists := DefaultGlobalSettings[key]
	if !exists {
		return nil, fmt.Errorf("unknown global setting key: %s", key)
	}
	return defaultValue, nil
}

// GetAppSettingDefault returns the default value for an app setting.
// Returns error if the setting key is unknown.
func GetAppSettingDefault(key string) (interface{}, error) {
	defaultValue, exists := DefaultAppSettings[key]
	if !exists {
		return nil, fmt.Errorf("unknown app setting key: %s", key)
	}
	return defaultValue, nil
}

// GetAllGlobalSettings retrieves all global settings with environment fallback.
func (s *Service) GetAllGlobalSettings() (map[string]*Setting, error) {
	result := make(map[string]*Setting)
	for key, defaultValue := range DefaultGlobalSettings {
		setting, err := s.GetGlobalSetting(key, defaultValue)
		if err != nil {
			return nil, err
		}
		result[key] = setting
	}

	return result, nil
}

// GetAllAppSettings retrieves all app-specific settings with environment fallback.
func (s *Service) GetAllAppSettings(appName string) (map[string]*Setting, error) {
	result := make(map[string]*Setting)
	for key, defaultValue := range DefaultAppSettings {
		setting, err := s.GetAppSetting(appName, key, defaultValue)
		if err != nil {
			return nil, err
		}
		result[key] = setting
	}

	return result, nil
}

// SetGlobalSetting saves a global setting to the database.
func (s *Service) SetGlobalSetting(key string, value interface{}, updatedByUserID *int64) error {
	return s.db.UpsertSetting(nil, key, value, updatedByUserID)
}

// SetAppSetting saves an app-specific setting to the database.
func (s *Service) SetAppSetting(appName, key string, value interface{}, updatedByUserID *int64) error {
	return s.db.UpsertSetting(&appName, key, value, updatedByUserID)
}

// DeleteGlobalSetting removes a global setting from the database (reverts to env/default).
func (s *Service) DeleteGlobalSetting(key string) error {
	return s.db.DeleteSetting(nil, key)
}

// DeleteAppSetting removes an app-specific setting from the database (reverts to env/default).
func (s *Service) DeleteAppSetting(appName, key string) error {
	return s.db.DeleteSetting(&appName, key)
}

// GetMFASettings returns MFA configuration with environment fallback.
func (s *Service) GetMFASettings() (*db.MFASettings, error) {
	requireAll, err := s.GetGlobalSetting(KeyMFARequireAllUsers, true)
	if err != nil {
		return nil, err
	}

	ttlDays, err := s.GetGlobalSetting(KeyTrustedDeviceTTLDays, 30)
	if err != nil {
		return nil, err
	}

	stepUpMinutes, err := s.GetGlobalSetting(KeyStepUpWindowMinutes, 10)
	if err != nil {
		return nil, err
	}

	// Convert values to correct types
	requireAllBool, ok := requireAll.Value.(bool)
	if !ok {
		requireAllBool = true
	}

	ttlDaysInt, ok := ttlDays.Value.(int)
	if !ok {
		if f, ok := ttlDays.Value.(float64); ok {
			ttlDaysInt = int(f)
		} else {
			ttlDaysInt = 30
		}
	}

	stepUpMinutesInt, ok := stepUpMinutes.Value.(int)
	if !ok {
		if f, ok := stepUpMinutes.Value.(float64); ok {
			stepUpMinutesInt = int(f)
		} else {
			stepUpMinutesInt = 10
		}
	}

	return &db.MFASettings{
		RequireAllUsers:      requireAllBool,
		TrustedDeviceTTLDays: ttlDaysInt,
		StepUpWindowMinutes:  stepUpMinutesInt,
	}, nil
}

// SetMFASettings stores MFA configuration in the database.
func (s *Service) SetMFASettings(settings *db.MFASettings, updatedByUserID *int64) error {
	if settings == nil {
		return fmt.Errorf("mfa settings cannot be nil")
	}
	if settings.TrustedDeviceTTLDays <= 0 {
		settings.TrustedDeviceTTLDays = 30
	}
	if settings.StepUpWindowMinutes <= 0 {
		settings.StepUpWindowMinutes = 10
	}

	if err := s.SetGlobalSetting(KeyMFARequireAllUsers, settings.RequireAllUsers, updatedByUserID); err != nil {
		return err
	}
	if err := s.SetGlobalSetting(KeyTrustedDeviceTTLDays, settings.TrustedDeviceTTLDays, updatedByUserID); err != nil {
		return err
	}
	if err := s.SetGlobalSetting(KeyStepUpWindowMinutes, settings.StepUpWindowMinutes, updatedByUserID); err != nil {
		return err
	}

	return nil
}

// GetAllowDestructiveActions returns whether destructive actions are allowed.
func (s *Service) GetAllowDestructiveActions() (bool, error) {
	setting, err := s.GetGlobalSetting(KeyAllowDestructiveActions, false)
	if err != nil {
		return false, err
	}

	if val, ok := setting.Value.(bool); ok {
		return val, nil
	}

	return false, nil
}

// GetProtectedEnvVars returns protected environment variables for an app.
func (s *Service) GetProtectedEnvVars(appName string) ([]string, error) {
	setting, err := s.GetAppSetting(appName, KeyProtectedEnvVars, []string{})
	if err != nil {
		return []string{}, err
	}

	// Handle both []interface{} and []string from JSON unmarshaling
	switch val := setting.Value.(type) {
	case []string:
		return normalizeEnvVars(val), nil
	case []interface{}:
		strs := make([]string, 0, len(val))
		for _, v := range val {
			if s, ok := v.(string); ok {
				strs = append(strs, s)
			}
		}
		return normalizeEnvVars(strs), nil
	default:
		return []string{}, nil
	}
}

// GetSecretEnvVars returns secret environment variables for an app.
func (s *Service) GetSecretEnvVars(appName string) ([]string, error) {
	setting, err := s.GetAppSetting(appName, KeySecretEnvVars, []string{})
	if err != nil {
		return []string{}, err
	}

	// Handle both []interface{} and []string from JSON unmarshaling
	switch val := setting.Value.(type) {
	case []string:
		return normalizeEnvVars(val), nil
	case []interface{}:
		strs := make([]string, 0, len(val))
		for _, v := range val {
			if s, ok := v.(string); ok {
				strs = append(strs, s)
			}
		}
		return normalizeEnvVars(strs), nil
	default:
		return []string{}, nil
	}
}

// normalizeEnvVars normalizes env var names (uppercase, unique).
func normalizeEnvVars(vars []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(vars))
	for _, v := range vars {
		normalized := strings.TrimSpace(strings.ToUpper(v))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; !ok {
			seen[normalized] = struct{}{}
			result = append(result, normalized)
		}
	}
	return result
}

// GetApprovedDeployCommands returns approved commands for an app.
func (s *Service) GetApprovedDeployCommands(appName string) ([]string, error) {
	setting, err := s.GetAppSetting(appName, KeyApprovedDeployCommands, []string(nil))
	if err != nil {
		return []string{}, err
	}

	if setting.Value == nil {
		return []string{}, nil
	}

	// Handle both []interface{} and []string from JSON unmarshaling
	switch val := setting.Value.(type) {
	case []string:
		return val, nil
	case []interface{}:
		strs := make([]string, 0, len(val))
		for _, v := range val {
			if s, ok := v.(string); ok {
				strs = append(strs, s)
			}
		}
		return strs, nil
	default:
		return []string{}, nil
	}
}

// GetServiceImagePatterns returns service image patterns for an app.
func (s *Service) GetServiceImagePatterns(appName string) (map[string]string, error) {
	setting, err := s.GetAppSetting(appName, KeyServiceImagePatterns, map[string]string(nil))
	if err != nil {
		return map[string]string{}, err
	}

	if setting.Value == nil {
		return map[string]string{}, nil
	}

	// Handle both map[string]interface{} and map[string]string from JSON unmarshaling
	switch val := setting.Value.(type) {
	case map[string]string:
		return val, nil
	case map[string]interface{}:
		result := make(map[string]string)
		for k, v := range val {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
		return result, nil
	default:
		return map[string]string{}, nil
	}
}
