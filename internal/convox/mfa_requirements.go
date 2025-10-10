package convox

import (
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// MFALevel defines the MFA verification requirement level
type MFALevel int

const (
	// MFANone - No MFA required beyond enrollment
	MFANone MFALevel = iota
	// MFAStepUp - MFA required within time window (default 5 minutes)
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
	rbac.Convox(rbac.ResourceApp, rbac.ActionDelete):         MFAAlways,
	rbac.Convox(rbac.ResourceInstance, rbac.ActionTerminate): MFAAlways,

	// SENSITIVE OPERATIONS - Step-up within time window

	// App lifecycle
	rbac.Convox(rbac.ResourceApp, rbac.ActionCreate):  MFAStepUp,
	rbac.Convox(rbac.ResourceApp, rbac.ActionUpdate):  MFAStepUp,
	rbac.Convox(rbac.ResourceApp, rbac.ActionRestart): MFAStepUp,

	// Deployments and builds
	rbac.Convox(rbac.ResourceBuild, rbac.ActionCreate):    MFAStepUp,
	rbac.Convox(rbac.ResourceRelease, rbac.ActionCreate):  MFAStepUp,
	rbac.Convox(rbac.ResourceRelease, rbac.ActionPromote): MFAStepUp,

	// Process control
	rbac.Convox(rbac.ResourceProcess, rbac.ActionExec):      MFAStepUp,
	rbac.Convox(rbac.ResourceProcess, rbac.ActionTerminate): MFAStepUp,
	rbac.Convox(rbac.ResourceProcess, rbac.ActionStart):     MFAStepUp,
	rbac.Convox(rbac.ResourceProcess, rbac.ActionStop):      MFAStepUp,

	// Environment variables (secrets)
	rbac.Convox(rbac.ResourceEnv, rbac.ActionSet): MFAStepUp,

	// Rack operations
	rbac.Convox(rbac.ResourceRack, rbac.ActionUpdate): MFAStepUp,

	// Object uploads for builds (write operation but low risk)
	rbac.Convox(rbac.ResourceObject, rbac.ActionCreate): MFANone,

	// ========================================================================
	// GATEWAY RESOURCES - Gateway-specific operations
	// ========================================================================

	// PRIVILEGED OPERATIONS - Always require fresh MFA

	// Deploy Approvals - Granting permission to deploy
	rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionApprove): MFAAlways,

	// API Tokens - Creating/deleting long-lived credentials
	rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionCreate): MFAAlways,
	rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionUpdate): MFAAlways,
	rbac.Gateway(rbac.ResourceAPIToken, rbac.ActionDelete): MFAAlways,

	// User Management - Changing roles/permissions
	rbac.Gateway(rbac.ResourceUser, rbac.ActionCreate): MFAAlways, // Creating users
	rbac.Gateway(rbac.ResourceUser, rbac.ActionUpdate): MFAAlways, // Role changes
	rbac.Gateway(rbac.ResourceUser, rbac.ActionDelete): MFAAlways,

	// Security Settings - Modifying security configuration
	rbac.Security(rbac.ResourceSecret, rbac.ActionUpdate): MFAAlways,

	// WRITE OPERATIONS - Need step-up

	// Deploy Approvals - Creating request is safe (just asking for approval)
	rbac.Gateway(rbac.ResourceDeployApprovalRequest, rbac.ActionCreate): MFANone,

	// Integrations - Configuring integrations
	rbac.Gateway(rbac.ResourceIntegration, rbac.ActionUpdate): MFAStepUp,
	rbac.Gateway(rbac.ResourceIntegration, rbac.ActionCreate): MFAStepUp,
	rbac.Gateway(rbac.ResourceIntegration, rbac.ActionDelete): MFAStepUp,

	// ========================================================================
	// AUTH RESOURCES - Authentication and MFA management
	// ========================================================================

	// MFA Methods - Deleting removes security, creating adds security
	rbac.Auth(rbac.ResourceMFAMethod, rbac.ActionDelete): MFAAlways, // Removing MFA is privileged
	rbac.Auth(rbac.ResourceMFAMethod, rbac.ActionCreate): MFANone,   // Enrolling in MFA is encouraged
	rbac.Auth(rbac.ResourceMFAMethod, rbac.ActionUpdate): MFAStepUp, // Updating MFA method settings

	// MFA Verification - Step-up operations
	rbac.Auth(rbac.ResourceMFA, rbac.ActionCreate): MFANone, // Verifying MFA is the action itself
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
	return action == rbac.ActionStringList || action == rbac.ActionStringRead
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
		return "unknown"
	}
}
