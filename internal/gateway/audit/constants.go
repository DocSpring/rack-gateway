package audit

import "github.com/DocSpring/rack-gateway/internal/gateway/rbac"

// Action type constants (high-level categorization for audit logs)
const (
	ActionTypeAuth     = rbac.ScopeStringAuth
	ActionTypeConvox   = rbac.ScopeStringConvox
	ActionTypeGateway  = rbac.ScopeStringGateway
	ActionTypeSecurity = rbac.ScopeStringSecurity
)

// Action scope constants (first part of action, e.g., "login" in "login.start")
const (
	ActionScopeAPIToken              = rbac.ResourceStringAPIToken
	ActionScopeDeployApprovalRequest = rbac.ResourceStringDeployApprovalRequest
	ActionScopeLogin                 = "login"
	ActionScopeLogout                = "logout"
	ActionScopeMFABackupCodes        = rbac.ResourceStringMFABackupCodes
	ActionScopeMFAMethod             = rbac.ResourceStringMFAMethod
	ActionScopeMFAPreferences        = rbac.ResourceStringMFAPreferences
	ActionScopeMFAVerification       = rbac.ResourceStringMFAVerification
	ActionScopeRateLimit             = "rate_limit"
	ActionScopeRelease               = rbac.ResourceStringRelease
	ActionScopeSuspiciousActivity    = "suspicious_activity"
	ActionScopeTrustedDevice         = rbac.ResourceStringTrustedDevice
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
	ActionVerbOAuthFailed        = "oauth_failed"
	ActionVerbParamsSet          = "params.set"
	ActionVerbReject             = "reject"
	ActionVerbRequireAllUsers    = "require_all_users"
	ActionVerbUnlock             = "unlock"
	ActionVerbUnset              = "unset"
	ActionVerbUpdateRoles        = "update_roles"
	ActionVerbUserNotAuthorized  = "user_not_authorized"
	ActionVerbValidate           = "validate"
	ActionVerbVerify             = "verify"
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
