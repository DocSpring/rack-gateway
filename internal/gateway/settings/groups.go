package settings

// GlobalSettingGroup identifies a logical collection of global settings that are
// saved together via a grouped API endpoint.
type GlobalSettingGroup string

// Global setting groups mapped to their constituent keys.
// App setting groups mapped to their constituent keys.
const (
	GlobalSettingGroupMFAConfiguration GlobalSettingGroup = "mfa_configuration"
	GlobalSettingGroupAllowDestructive GlobalSettingGroup = "allow_destructive_actions"
	GlobalSettingGroupVCSAndCIDefaults GlobalSettingGroup = "vcs_and_ci_defaults"
	GlobalSettingGroupDeployApprovals  GlobalSettingGroup = "deploy_approvals"
)

var globalSettingGroupKeys = map[GlobalSettingGroup][]GlobalSettingKey{
	GlobalSettingGroupMFAConfiguration: {
		GlobalSettingMFARequireAllUsers,
		GlobalSettingTrustedDeviceTTLDays,
		GlobalSettingStepUpWindowMinutes,
	},
	GlobalSettingGroupAllowDestructive: {
		GlobalSettingAllowDestructiveActions,
	},
	GlobalSettingGroupVCSAndCIDefaults: {
		GlobalSettingDefaultCIProvider,
		GlobalSettingDefaultCIOrgSlug,
		GlobalSettingDefaultVCSProvider,
		GlobalSettingDefaultVCSOrgName,
	},
	GlobalSettingGroupDeployApprovals: {
		GlobalSettingDeployApprovalsEnabled,
		GlobalSettingDeployApprovalWindowMinutes,
	},
}

// GlobalSettingGroupKeys returns a copy of the keys within the specified group.
func GlobalSettingGroupKeys(group GlobalSettingGroup) []GlobalSettingKey {
	keys := globalSettingGroupKeys[group]
	out := make([]GlobalSettingKey, len(keys))
	copy(out, keys)
	return out
}

// GlobalSettingGroupKeyStrings returns the string representation of all keys in the group.
func GlobalSettingGroupKeyStrings(group GlobalSettingGroup) []string {
	keys := GlobalSettingGroupKeys(group)
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = key.String()
	}
	return out
}

// IsGlobalSettingInGroup checks whether a string key belongs to the given group.
func IsGlobalSettingInGroup(group GlobalSettingGroup, key string) bool {
	for _, candidate := range globalSettingGroupKeys[group] {
		if candidate.String() == key {
			return true
		}
	}
	return false
}

// AppSettingGroup identifies a logical collection of app settings that are saved together.
type AppSettingGroup string

const (
	// AppSettingGroupVCSCIDeploy groups version control and CI related app settings.
	AppSettingGroupVCSCIDeploy AppSettingGroup = "vcs_ci_deploy"
)

var appSettingGroupKeys = map[AppSettingGroup][]AppSettingKey{
	AppSettingGroupVCSCIDeploy: {
		AppSettingVCSProvider,
		AppSettingVCSRepo,
		AppSettingCIProvider,
		AppSettingCIOrgSlug,
		AppSettingGitHubVerification,
		AppSettingAllowDeployFromDefaultBranch,
		AppSettingRequirePRForBranch,
		AppSettingDefaultBranch,
		AppSettingVerifyGitCommitMode,
	},
}

// AppSettingGroupKeys returns a copy of the keys contained in the group.
func AppSettingGroupKeys(group AppSettingGroup) []AppSettingKey {
	keys := appSettingGroupKeys[group]
	out := make([]AppSettingKey, len(keys))
	copy(out, keys)
	return out
}

// AppSettingGroupKeyStrings returns the string representation of the group's keys.
func AppSettingGroupKeyStrings(group AppSettingGroup) []string {
	keys := AppSettingGroupKeys(group)
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = key.String()
	}
	return out
}

// IsAppSettingInGroup checks whether a key belongs to the specified app setting group.
func IsAppSettingInGroup(group AppSettingGroup, key string) bool {
	for _, candidate := range appSettingGroupKeys[group] {
		if candidate.String() == key {
			return true
		}
	}
	return false
}
