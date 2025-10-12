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
	// MFAStepUp - MFA required within time window (default 10 minutes)
	MFAStepUp
	// MFAAlways - MFA required immediately before action, no time window
	MFAAlways
)

// MFARequirements is the EXHAUSTIVE list of write/mutating operations and their MFA level
// Read/list operations default to MFANone and don't need to be listed
// This is shared between the gateway (for API enforcement) and the CLI (for pre-emptive step-up)
// CRITICAL: All write operations must be listed. GetMFALevel will panic for unlisted write operations.
var MFARequirements = map[string]MFALevel{
	// ========================================================================
	// CONVOX RESOURCES - Operations on the Convox rack
	// ========================================================================

	// DESTRUCTIVE OPERATIONS - Always require fresh MFA, no time window
	Convox(ResourceApp, ActionDelete):         MFAAlways,
	Convox(ResourceInstance, ActionTerminate): MFAAlways,

	// SENSITIVE OPERATIONS - Step-up within time window

	// App lifecycle
	Convox(ResourceApp, ActionCreate):  MFAStepUp,
	Convox(ResourceApp, ActionUpdate):  MFAStepUp,
	Convox(ResourceApp, ActionRestart): MFAStepUp,

	// Deployments and builds
	Convox(ResourceBuild, ActionCreate):    MFAStepUp,
	Convox(ResourceRelease, ActionCreate):  MFAStepUp,
	Convox(ResourceRelease, ActionPromote): MFAStepUp,

	// Process control
	Convox(ResourceProcess, ActionExec):      MFAStepUp,
	Convox(ResourceProcess, ActionTerminate): MFAStepUp,
	Convox(ResourceProcess, ActionStart):     MFAStepUp,
	Convox(ResourceProcess, ActionStop):      MFAStepUp,

	// Environment variables (secrets)
	Convox(ResourceEnv, ActionSet): MFAStepUp,

	// Rack operations
	Convox(ResourceRack, ActionUpdate): MFAStepUp,

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
	Gateway(ResourceUser, ActionCreate): MFAAlways, // Creating users
	Gateway(ResourceUser, ActionUpdate): MFAAlways, // Role changes
	Gateway(ResourceUser, ActionDelete): MFAAlways,

	// Security Settings - Modifying security configuration
	Security(ResourceSecret, ActionUpdate): MFAAlways,

	// WRITE OPERATIONS - Need step-up

	// Deploy Approvals - Creating request is safe (just asking for approval)
	Gateway(ResourceDeployApprovalRequest, ActionCreate): MFANone,

	// Integrations - Configuring integrations
	Gateway(ResourceIntegration, ActionUpdate): MFAStepUp,
	Gateway(ResourceIntegration, ActionCreate): MFAStepUp,
	Gateway(ResourceIntegration, ActionDelete): MFAStepUp,

	// ========================================================================
	// AUTH RESOURCES - Authentication and MFA management
	// ========================================================================

	// MFA Methods - Deleting removes security, creating adds security
	Auth(ResourceMFAMethod, ActionDelete): MFAAlways, // Removing MFA is privileged
	Auth(ResourceMFAMethod, ActionCreate): MFANone,   // Enrolling in MFA is encouraged
	Auth(ResourceMFAMethod, ActionUpdate): MFAStepUp, // Updating MFA method settings
	Auth(ResourceMFA, ActionGenerate):     MFAStepUp, // Regenerating backup codes
	Auth(ResourceMFA, ActionDelete):       MFAStepUp, // Removing trusted devices
	Auth(ResourceMFA, ActionUpdate):       MFAStepUp, // Updating MFA preferences

	// MFA Verification - Step-up operations
	Auth(ResourceMFA, ActionCreate): MFANone, // Verifying MFA is the action itself

	// ========================================================================
	// SETTINGS RESOURCES - Gateway and app configuration
	// Format: gateway:setting:{key} (applies to both update and delete)
	// ========================================================================

	// Global Settings - Security-critical configurations
	GatewayGlobalSetting(settings.GlobalSettingAllowDestructiveActions):     MFAAlways, // Enabling destructive operations
	GatewayGlobalSetting(settings.GlobalSettingDefaultCIOrgSlug):            MFANone,   // Just defaults
	GatewayGlobalSetting(settings.GlobalSettingDefaultCIProvider):           MFANone,   // Just defaults
	GatewayGlobalSetting(settings.GlobalSettingDefaultVCSOrgName):           MFANone,   // Just defaults
	GatewayGlobalSetting(settings.GlobalSettingDefaultVCSProvider):          MFANone,   // Just defaults
	GatewayGlobalSetting(settings.GlobalSettingDeployApprovalsEnabled):      MFAAlways, // Disabling approval workflow
	GatewayGlobalSetting(settings.GlobalSettingDeployApprovalWindowMinutes): MFAStepUp, // Extending approval window
	GatewayGlobalSetting(settings.GlobalSettingMFARequireAllUsers):          MFAAlways, // Disabling MFA globally
	GatewayGlobalSetting(settings.GlobalSettingStepUpWindowMinutes):         MFAStepUp, // Extending step-up window
	GatewayGlobalSetting(settings.GlobalSettingTrustedDeviceTTLDays):        MFAStepUp, // Extending trust window

	// App Settings - Security controls
	GatewayAppSetting(settings.AppSettingAllowDeployFromDefaultBranch):  MFAStepUp, // Allowing more deploys
	GatewayAppSetting(settings.AppSettingApprovedDeployCommands):        MFAStepUp, // Defining allowed commands
	GatewayAppSetting(settings.AppSettingCIOrgSlug):                     MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingCIProvider):                    MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingCircleCIApprovalJobName):       MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingCircleCIAutoApproveOnApproval): MFAStepUp, // Auto-approving
	GatewayAppSetting(settings.AppSettingDefaultBranch):                 MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingGitHubPostPRComment):           MFANone,   // Just notification preference
	GatewayAppSetting(settings.AppSettingGitHubVerification):            MFAAlways, // Disabling GitHub verification
	GatewayAppSetting(settings.AppSettingProtectedEnvVars):              MFAStepUp, // Marking vars as protected
	GatewayAppSetting(settings.AppSettingRequirePRForBranch):            MFAAlways, // Disabling PR requirement
	GatewayAppSetting(settings.AppSettingSecretEnvVars):                 MFAStepUp, // Marking vars as secret
	GatewayAppSetting(settings.AppSettingServiceImagePatterns):          MFAStepUp, // Restricting images
	GatewayAppSetting(settings.AppSettingVCSProvider):                   MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingVCSRepo):                       MFANone,   // Just configuration
	GatewayAppSetting(settings.AppSettingVerifyGitCommitMode):           MFAStepUp, // Changing verification level
}

// GetMFALevel returns the highest MFA level required by any of the given permissions
// ONLY allows unlisted permissions if they are read/list actions - panics otherwise
func GetMFALevel(permissions []string) MFALevel {
	highest := MFANone
	for _, perm := range permissions {
		level, ok := MFARequirements[perm]
		if !ok {
			// ONLY allow read/list actions to default to MFANone
			// Everything else MUST be explicitly listed
			if isReadOnlyAction(perm) {
				level = MFANone
			} else {
				panic(fmt.Sprintf("CRITICAL: Permission %q not found in MFARequirements - all write/mutating operations must be explicitly listed", perm))
			}
		}
		if level > highest {
			highest = level
		}
	}
	return highest
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
	return action == ActionStringList || action == ActionStringRead
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
	case MFAStepUp:
		return "step_up"
	case MFAAlways:
		return "always"
	default:
		panic(fmt.Sprintf("CRITICAL: Unknown MFALevel %d", m))
	}
}
