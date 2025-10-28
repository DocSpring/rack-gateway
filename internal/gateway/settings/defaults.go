package settings

import "fmt"

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
