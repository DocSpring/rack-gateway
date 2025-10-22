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
			EnvVar: envVarName,
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
		EnvVar: envVarName,
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

// GlobalSettingKey is an enum for global setting keys
type GlobalSettingKey uint8

const (
	GlobalSettingAllowDestructiveActions GlobalSettingKey = iota
	GlobalSettingDefaultCIOrgSlug
	GlobalSettingDefaultCIProvider
	GlobalSettingDefaultVCSOrgName
	GlobalSettingDefaultVCSProvider
	GlobalSettingDeployApprovalsEnabled
	GlobalSettingDeployApprovalWindowMinutes
	GlobalSettingMFARequireAllUsers
	GlobalSettingStepUpWindowMinutes
	GlobalSettingTrustedDeviceTTLDays
)

// Global setting key strings
const (
	KeyAllowDestructiveActions     = "allow_destructive_actions"
	KeyDefaultCIOrgSlug            = "default_ci_org_slug"
	KeyDefaultCIProvider           = "default_ci_provider"
	KeyDefaultVCSOrgName           = "default_vcs_org_name"
	KeyDefaultVCSProvider          = "default_vcs_provider"
	KeyDeployApprovalsEnabled      = "deploy_approvals_enabled"
	KeyDeployApprovalWindowMinutes = "deploy_approval_window_minutes"
	KeyMFARequireAllUsers          = "mfa_require_all_users"
	KeyStepUpWindowMinutes         = "mfa_step_up_window_minutes"
	KeyTrustedDeviceTTLDays        = "mfa_trusted_device_ttl_days"
)

var globalSettingKeyToString = [...]string{
	KeyAllowDestructiveActions,
	KeyDefaultCIOrgSlug,
	KeyDefaultCIProvider,
	KeyDefaultVCSOrgName,
	KeyDefaultVCSProvider,
	KeyDeployApprovalsEnabled,
	KeyDeployApprovalWindowMinutes,
	KeyMFARequireAllUsers,
	KeyStepUpWindowMinutes,
	KeyTrustedDeviceTTLDays,
}

func (g GlobalSettingKey) String() string {
	if int(g) < len(globalSettingKeyToString) {
		return globalSettingKeyToString[g]
	}
	return fmt.Sprintf("GlobalSettingKey(%d)", g)
}

func (g GlobalSettingKey) IsValid() bool { return g <= GlobalSettingTrustedDeviceTTLDays }

// ParseGlobalSettingKey converts a string key to a GlobalSettingKey enum
func ParseGlobalSettingKey(key string) (GlobalSettingKey, error) {
	for i, s := range globalSettingKeyToString {
		if s == key {
			return GlobalSettingKey(i), nil
		}
	}
	return 0, fmt.Errorf("invalid global setting key %q", key)
}

// AppSettingKey is an enum for app setting keys
type AppSettingKey uint8

const (
	AppSettingAllowDeployFromDefaultBranch AppSettingKey = iota
	AppSettingApprovedDeployCommands
	AppSettingCIOrgSlug
	AppSettingCIProvider
	AppSettingCircleCIApprovalJobName
	AppSettingCircleCIAutoApproveOnApproval
	AppSettingDefaultBranch
	AppSettingGitHubPostPRComment
	AppSettingGitHubVerification
	AppSettingProtectedEnvVars
	AppSettingRequirePRForBranch
	AppSettingSecretEnvVars
	AppSettingServiceImagePatterns
	AppSettingVCSProvider
	AppSettingVCSRepo
	AppSettingVerifyGitCommitMode
)

// App setting key strings
const (
	KeyAllowDeployFromDefaultBranch  = "allow_deploy_from_default_branch"
	KeyApprovedDeployCommands        = "approved_deploy_commands"
	KeyCIOrgSlug                     = "ci_org_slug"
	KeyCIProvider                    = "ci_provider"
	KeyCircleCIApprovalJobName       = "circleci_approval_job_name"
	KeyCircleCIAutoApproveOnApproval = "circleci_auto_approve_on_approval"
	KeyDefaultBranch                 = "default_branch"
	KeyGitHubPostPRComment           = "github_post_pr_comment"
	KeyGitHubVerification            = "github_verification"
	KeyProtectedEnvVars              = "protected_env_vars"
	KeyRequirePRForBranch            = "require_pr_for_branch"
	KeySecretEnvVars                 = "secret_env_vars"
	KeyServiceImagePatterns          = "service_image_patterns"
	KeyVCSProvider                   = "vcs_provider"
	KeyVCSRepo                       = "vcs_repo"
	KeyVerifyGitCommitMode           = "verify_git_commit_mode"
)

var appSettingKeyToString = [...]string{
	KeyAllowDeployFromDefaultBranch,
	KeyApprovedDeployCommands,
	KeyCIOrgSlug,
	KeyCIProvider,
	KeyCircleCIApprovalJobName,
	KeyCircleCIAutoApproveOnApproval,
	KeyDefaultBranch,
	KeyGitHubPostPRComment,
	KeyGitHubVerification,
	KeyProtectedEnvVars,
	KeyRequirePRForBranch,
	KeySecretEnvVars,
	KeyServiceImagePatterns,
	KeyVCSProvider,
	KeyVCSRepo,
	KeyVerifyGitCommitMode,
}

func (a AppSettingKey) String() string {
	if int(a) < len(appSettingKeyToString) {
		return appSettingKeyToString[a]
	}
	return fmt.Sprintf("AppSettingKey(%d)", a)
}

func (a AppSettingKey) IsValid() bool { return a <= AppSettingVerifyGitCommitMode }

// ParseAppSettingKey converts a string key to an AppSettingKey enum
func ParseAppSettingKey(key string) (AppSettingKey, error) {
	for i, s := range appSettingKeyToString {
		if s == key {
			return AppSettingKey(i), nil
		}
	}
	return 0, fmt.Errorf("invalid app setting key %q", key)
}

// VerifyGitCommitMode represents valid values for verify_git_commit_mode setting
const (
	VerifyGitCommitModeBranch = "branch" // Commit must exist on the specified branch
	VerifyGitCommitModeLatest = "latest" // Commit must be the latest on the specified branch
)

// DefaultGlobalSettings defines all valid global settings with their default values.
var DefaultGlobalSettings = map[string]interface{}{
	KeyAllowDestructiveActions:     false,
	KeyDefaultCIOrgSlug:            "",
	KeyDefaultCIProvider:           "circleci",
	KeyDefaultVCSOrgName:           "",
	KeyDefaultVCSProvider:          "github",
	KeyDeployApprovalsEnabled:      true,
	KeyDeployApprovalWindowMinutes: 15,
	KeyMFARequireAllUsers:          true,
	KeyStepUpWindowMinutes:         10,
	KeyTrustedDeviceTTLDays:        30,
}

// DefaultAppSettings defines all valid app-specific settings with their default values.
var DefaultAppSettings = map[string]interface{}{
	KeyAllowDeployFromDefaultBranch:  false,
	KeyApprovedDeployCommands:        []string(nil),
	KeyCIOrgSlug:                     nil, // nil means use global default
	KeyCIProvider:                    nil, // nil means use global default
	KeyCircleCIApprovalJobName:       "",
	KeyCircleCIAutoApproveOnApproval: false,
	KeyDefaultBranch:                 "main",
	KeyGitHubPostPRComment:           true,
	KeyGitHubVerification:            true,
	KeyProtectedEnvVars:              []string(nil),
	KeyRequirePRForBranch:            true,
	KeySecretEnvVars:                 []string(nil),
	KeyServiceImagePatterns:          map[string]string(nil),
	KeyVCSProvider:                   nil, // nil means use global default
	KeyVCSRepo:                       nil, // nil means not configured
	KeyVerifyGitCommitMode:           "latest",
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
// For VCS/CI provider fields, if the app setting is nil/empty, it will use the global default
// and mark the source as SourceGlobalDefault.
func (s *Service) GetAllAppSettings(appName string) (map[string]*Setting, error) {
	result := make(map[string]*Setting)

	// Map of app setting keys to their corresponding global default keys
	globalDefaultKeys := map[string]string{
		KeyVCSProvider: KeyDefaultVCSProvider,
		KeyCIProvider:  KeyDefaultCIProvider,
		KeyCIOrgSlug:   KeyDefaultCIOrgSlug,
	}

	for key, defaultValue := range DefaultAppSettings {
		setting, err := s.GetAppSetting(appName, key, defaultValue)
		if err != nil {
			return nil, err
		}

		// Check if this setting should fall back to a global default
		if globalKey, hasGlobal := globalDefaultKeys[key]; hasGlobal {
			// If app setting is nil or empty string, use global default
			if setting.Value == nil || setting.Value == "" {
				globalSetting, err := s.GetGlobalSetting(globalKey, "")
				if err != nil {
					return nil, err
				}
				// Use global value but mark as coming from global default
				setting.Value = globalSetting.Value
				setting.Source = SourceGlobalDefault
				// Keep the app-level env var name for reference
			}
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

// GetDeployApprovalsEnabled returns whether deploy approvals are enabled.
func (s *Service) GetDeployApprovalsEnabled() (bool, error) {
	setting, err := s.GetGlobalSetting(KeyDeployApprovalsEnabled, true)
	if err != nil {
		return true, err
	}

	if val, ok := setting.Value.(bool); ok {
		return val, nil
	}

	return true, nil
}

// GetDeployApprovalWindowMinutes returns the deploy approval window in minutes.
func (s *Service) GetDeployApprovalWindowMinutes() (int, error) {
	setting, err := s.GetGlobalSetting(KeyDeployApprovalWindowMinutes, 15)
	if err != nil {
		return 15, err
	}

	// Handle both int and float64 (JSON unmarshaling quirk)
	switch val := setting.Value.(type) {
	case int:
		return val, nil
	case float64:
		return int(val), nil
	default:
		return 15, nil
	}
}

// GetProtectedEnvVars returns protected environment variables for an app.
func (s *Service) GetProtectedEnvVars(appName string) ([]string, error) {
	return s.getEnvVarList(appName, KeyProtectedEnvVars)
}

// GetSecretEnvVars returns secret environment variables for an app.
func (s *Service) GetSecretEnvVars(appName string) ([]string, error) {
	return s.getEnvVarList(appName, KeySecretEnvVars)
}

func (s *Service) getEnvVarList(appName, key string) ([]string, error) {
	setting, err := s.GetAppSetting(appName, key, []string{})
	if err != nil {
		return []string{}, err
	}
	return normalizeEnvVars(extractStringSlice(setting.Value)), nil
}

func extractStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
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
