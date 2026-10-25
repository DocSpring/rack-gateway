package rbac

import (
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// MFALevel defines the MFA verification requirement level
type MFALevel int

const (
	// MFANone - No MFA required beyond enrollment
	MFANone MFALevel = iota
	// MFAStepUpOnceEnrolled - MFAStepUp for a user who has a factor, none for a user who has yet to enroll one
	MFAStepUpOnceEnrolled
	// MFAStepUp - MFA required within time window (default 10 minutes)
	MFAStepUp
	// MFAAlways - MFA required immediately before action, no time window
	MFAAlways
)

// MFARequirements is the EXHAUSTIVE list of write/mutating operations and their MFA level
// Read/list operations default to MFANone and don't need to be listed
// This is shared between the gateway (for API enforcement) and the CLI (for pre-emptive step-up)
// CRITICAL: All write operations must be listed. An unlisted write operation fails closed to MFAAlways, and a test
// checks that every write permission in the route tables and the CLI command table is listed.
var MFARequirements = map[string]MFALevel{
	// ========================================================================
	// CONVOX RESOURCES - Operations on the Convox rack
	// ========================================================================

	// DESTRUCTIVE OPERATIONS - Always require fresh MFA, no time window
	Convox(ResourceApp, ActionDelete): MFAAlways,

	// Importing a build skips the manifest and image checks a build gets: its manifest and images come straight
	// from the archive. Admins only (convox:*:*), and rare.
	Convox(ResourceBuild, ActionImport): MFAAlways,

	// SENSITIVE OPERATIONS - Step-up within time window

	// App lifecycle
	Convox(ResourceApp, ActionCreate):  MFAStepUp,
	Convox(ResourceApp, ActionUpdate):  MFAStepUp,
	Convox(ResourceApp, ActionRestart): MFAStepUp,

	// Deployments and builds
	Convox(ResourceBuild, ActionCreate):    MFAStepUp,
	Convox(ResourceBuild, ActionUpdate):    MFAStepUp, // Changes a build's manifest, release or status
	Convox(ResourceRelease, ActionCreate):  MFAStepUp,
	Convox(ResourceRelease, ActionPromote): MFAStepUp,

	// Running a command in the app (exec, run) can read, change or delete any of its data, so every one needs a
	// proof of its own, whatever the step-up window says. `convox run` starts a process with a placeholder command
	// and execs the real command into it: the placeholder start runs nothing and needs no proof (see the proxy's
	// attached run check), and the exec carries the command's one proof.
	Convox(ResourceProcess, ActionExec):  MFAAlways,
	Convox(ResourceProcess, ActionStart): MFAAlways,

	// Process control
	Convox(ResourceProcess, ActionTerminate): MFAStepUp,
	Convox(ResourceProcess, ActionStop):      MFAStepUp,

	// Environment variables (secrets)
	Convox(ResourceEnv, ActionSet):   MFAStepUp,
	Convox(ResourceEnv, ActionUnset): MFAStepUp,

	// Rack operations

	// Object uploads for builds (write operation but low risk)
	Convox(ResourceObject, ActionCreate): MFANone,

	// ========================================================================
	// GATEWAY RESOURCES - Gateway-specific operations
	// ========================================================================

	// PRIVILEGED OPERATIONS - Always require fresh MFA

	// Deploy Approvals - Granting permission to deploy
	Gateway(ResourceDeployApprovalRequest, ActionApprove): MFAAlways,

	// API Tokens - Creating/deleting long-lived credentials
	Gateway(ResourceAPIToken, ActionCreate): MFAAlways,
	Gateway(ResourceAPIToken, ActionUpdate): MFAAlways,
	Gateway(ResourceAPIToken, ActionDelete): MFAAlways,

	// User Management - Changing roles/permissions
	Gateway(ResourceUser, ActionCreate):     MFAAlways, // Creating users
	Gateway(ResourceUser, ActionUpdate):     MFAAlways, // Role changes
	Gateway(ResourceUser, ActionUpdateName): MFAStepUp, // Name-only updates
	Gateway(ResourceUser, ActionDelete):     MFAAlways,

	// Security Settings - Modifying security configuration
	Security(ResourceSecret, ActionUpdate): MFAAlways,

	// Revealing secret env values (GET /apps/:app/env?secrets=true; middleware.RequireMFAToRevealSecrets)
	Convox(ResourceSecret, ActionRead): MFAStepUp,

	// Deploy Approvals - Creating request is safe (just asking for approval)
	Gateway(ResourceDeployApprovalRequest, ActionCreate): MFANone,

	// Integrations - Configuring integrations
	Gateway(ResourceIntegration, ActionUpdate): MFAStepUp,
	Gateway(ResourceIntegration, ActionCreate): MFAStepUp,
	Gateway(ResourceIntegration, ActionDelete): MFAStepUp,

	// Background Jobs - Administrative job management
	Gateway(ResourceJob, ActionDelete): MFAStepUp,
	Gateway(ResourceJob, ActionUpdate): MFAStepUp,

	// ========================================================================
	// AUTH RESOURCES - Authentication and MFA management
	// ========================================================================

	// MFA Methods - Deleting removes security. A first factor needs no MFA (there's nothing to verify with); another
	// one needs an existing factor, or a session without one could add its own and gain step-up with it.
	Auth(ResourceMFAMethod, ActionDelete):        MFAAlways,
	Auth(ResourceMFAMethod, ActionCreate):        MFAStepUpOnceEnrolled,
	Auth(ResourceMFAMethod, ActionUpdate):        MFAStepUp, // Updating MFA method settings
	Auth(ResourceMFABackupCodes, ActionGenerate): MFAStepUp, // Regenerating backup codes
	Auth(ResourceTrustedDevice, ActionDelete):    MFAStepUp, // Removing trusted devices
	Auth(ResourceTrustedDevice, ActionCreate):    MFAStepUp, // Trusting a device requires fresh MFA
	Auth(ResourceMFAPreferences, ActionUpdate):   MFAStepUp, // Updating MFA preferences

	// MFA Verification - Step-up operations
	Auth(ResourceMFAVerification, ActionCreate): MFANone, // Verifying MFA is the action itself

	// ========================================================================
	// SETTINGS RESOURCES - Gateway and app configuration
	// Format: gateway:setting_group:{group} for grouped endpoints, or
	// gateway:setting:{key} for single-setting endpoints (applies to update/delete)
	// ========================================================================

	// Global Settings - Security-critical configurations
	GatewayGlobalSettingGroup(settings.GlobalSettingGroupAllowDestructive):     MFAAlways, // Destructive ops
	GatewayGlobalSettingGroup(settings.GlobalSettingGroupDeployApprovals):      MFAAlways, // Approval workflow
	GatewayGlobalSettingGroup(settings.GlobalSettingGroupMFAConfiguration):     MFAAlways, // MFA config
	GatewayGlobalSettingGroup(settings.GlobalSettingGroupSessionConfiguration): MFAStepUp, // Session timeouts
	GatewayGlobalSettingGroup(settings.GlobalSettingGroupVCSAndCIDefaults):     MFAStepUp, // Repo/CI defaults

	// App Settings - Security controls
	// Changing the protected or secret list needs MFA on that request, as changing a protected var does. Within a
	// step-up window, a var could otherwise be unprotected (or unmarked as secret), changed and protected again.
	GatewayAppSettingGroup(settings.AppSettingGroupVCSCIDeploy):         MFAAlways, // GitHub verify/PR reqs
	GatewayAppSetting(settings.AppSettingAllowDeployFromDefaultBranch):  MFAStepUp, // Allowing more deploys
	GatewayAppSetting(settings.AppSettingApprovedDeployCommands):        MFAStepUp, // Defining allowed commands
	GatewayAppSetting(settings.AppSettingAuditVisibleEnvVars):           MFAStepUp, // Showing values in audit logs
	GatewayAppSetting(settings.AppSettingCIProvider):                    MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingCircleCIApprovalJobName):       MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingCircleCIAutoApproveOnApproval): MFAStepUp, // Auto-approving
	GatewayAppSetting(settings.AppSettingDefaultBranch):                 MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingGitHubPostPRComment):           MFANone,   // Just notification preference
	GatewayAppSetting(settings.AppSettingGitHubVerification):            MFAAlways, // Disabling GitHub verification
	GatewayAppSetting(settings.AppSettingProtectedEnvVars):              MFAAlways, // Protecting vars
	GatewayAppSetting(settings.AppSettingRequireCommandJustification):   MFAAlways, // Requiring justifications
	GatewayAppSetting(settings.AppSettingRequirePRForBranch):            MFAAlways, // Disabling PR requirement
	GatewayAppSetting(settings.AppSettingSecretEnvVars):                 MFAAlways, // Masking secret values
	GatewayAppSetting(settings.AppSettingServiceImagePatterns):          MFAStepUp, // Restricting images
	GatewayAppSetting(settings.AppSettingVCSProvider):                   MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingVCSRepo):                       MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingVerifyGitCommitMode):           MFAStepUp, // Changing verification level
}

// GetMFALevel returns the highest MFA level required by any of the given permissions.
func GetMFALevel(permissions []string) MFALevel {
	highest := MFANone
	for _, perm := range permissions {
		if level := MFALevelForPermission(perm); level > highest {
			highest = level
		}
	}
	return highest
}

// MFALevelForPermission returns the MFA level a permission requires. An unlisted read or list permission needs
// none. Any other unlisted permission fails closed to MFAAlways, so a write that was never classified can't run
// without MFA.
func MFALevelForPermission(perm string) MFALevel {
	if level, ok := MFARequirements[perm]; ok {
		return level
	}
	if isReadOnlyAction(perm) {
		return MFANone
	}
	return MFAAlways
}

// isReadOnlyAction checks if a permission string ends with :list or :read
func isReadOnlyAction(permission string) bool {
	// Check suffix after last colon
	lastColon := -1
	for i := len(permission) - 1; i >= 0; i-- {
		if permission[i] == ':' {
			lastColon = i
			break
		}
	}
	if lastColon == -1 {
		return false
	}

	action := permission[lastColon+1:]
	return action == ActionList.String() || action == ActionRead.String()
}

// RequiresMFAStepUp returns true if any of the given permissions require MFA step-up (time window)
func RequiresMFAStepUp(permissions []string) bool {
	return GetMFALevel(permissions) >= MFAStepUp
}

// RequiresMFAAlways returns true if any of the given permissions require immediate MFA
func RequiresMFAAlways(permissions []string) bool {
	return GetMFALevel(permissions) == MFAAlways
}

// String returns the string representation of an MFALevel
func (m MFALevel) String() string {
	switch m {
	case MFANone:
		return "none"
	case MFAStepUpOnceEnrolled:
		return "step_up_once_enrolled"
	case MFAStepUp:
		return "step_up"
	case MFAAlways:
		return "always"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}
