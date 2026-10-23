package audit

import "github.com/DocSpring/rack-gateway/internal/gateway/rbac"

// Action type constants (high-level categorization for audit logs)
// These use the String() method from stringer-generated code
var (
	ActionTypeAuth     = rbac.ScopeAuth.String()
	ActionTypeConvox   = rbac.ScopeConvox.String()
	ActionTypeGateway  = rbac.ScopeGateway.String()
	ActionTypeSecurity = rbac.ScopeSecurity.String()
)

// Action scope constants (first part of action, e.g., "login" in "login.start")
// These use the String() method from stringer-generated code
var (
	ActionScopeAPIToken              = rbac.ResourceAPIToken.String()
	ActionScopeAuditLog              = rbac.ResourceAuditLog.String()
	ActionScopeDeployApprovalRequest = rbac.ResourceDeployApprovalRequest.String()
	ActionScopeLogin                 = "login"
	ActionScopeLogout                = "logout"
	ActionScopeMFA                   = "mfa"
	ActionScopeMFABackupCodes        = rbac.ResourceMFABackupCodes.String()
	ActionScopeMFAMethod             = rbac.ResourceMFAMethod.String()
	ActionScopeMFAPreferences        = rbac.ResourceMFAPreferences.String()
	ActionScopeRateLimit             = "rate_limit"
	ActionScopeRelease               = rbac.ResourceRelease.String()
	ActionScopeTrustedDevice         = rbac.ResourceTrustedDevice.String()
)

// ActionSessionCreate names the audit entries of a login creating a session (web or CLI).
const ActionSessionCreate = "session.create"

// MFA verification (rbac's auditActions names its routes with the same strings).
const (
	// ActionMFAVerify names every MFA verification, in the web UI or for a CLI login: its attempt, and its result
	// with status success or failed.
	ActionMFAVerify = "mfa.verify"
	// ActionMFAVerifyFailed is the alert for a failed MFA verification (or a wrong code while enrolling), recorded
	// besides the result: it emails the user and goes to the Slack channels whose patterns match it.
	ActionMFAVerifyFailed = "mfa.verify_failed"
	// ActionMFAChallenge names starting a WebAuthn verification, which issues its challenge.
	ActionMFAChallenge = "mfa.challenge"
)

// Action verb constants (second part of action, e.g., "start" in "login.start")
const (
	ActionVerbBackupCodeRevealed = "backup_code_revealed"
	ActionVerbBackupCodeUsed     = "backup_code_used"
	ActionVerbComplete           = "complete"
	ActionVerbEnroll             = "enroll"
	ActionVerbExceeded           = "exceeded"
	ActionVerbExport             = "export"
	ActionVerbFailed             = "failed"
	ActionVerbLock               = "lock"
	ActionVerbMFAReset           = "mfa_reset"
	ActionVerbOAuthFailed        = "oauth_failed"
	ActionVerbReject             = "reject"
	ActionVerbRequireAllUsers    = "require_all_users"
	ActionVerbSignInReset        = "sign_in_reset"
	ActionVerbUnlock             = "unlock"
	ActionVerbUnset              = "unset"
	ActionVerbUnsetProtected     = "unset_protected"
	ActionVerbUpdateRoles        = "update_roles"
	ActionVerbUserNotAuthorized  = "user_not_authorized"
	ActionVerbValidate           = "validate"
	// The scheduled audit log verification failing, and passing again after failing
	ActionVerbVerificationFailed    = "verification_failed"
	ActionVerbVerificationRecovered = "verification_recovered"
)

// Status constants
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusDenied  = "denied"
	StatusError   = "error"
	StatusAlert   = "alert"
)

// BuildAction builds an audit log action string from scope and verb (e.g., "login.start")
func BuildAction(scope, verb string) string {
	return scope + "." + verb
}
