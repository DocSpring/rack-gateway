package audit

import "github.com/DocSpring/rack-gateway/internal/gateway/rbac"

// Action type constants (high-level categorization for audit logs)
const (
	ActionTypeAuth     = rbac.ScopeStringAuth
	ActionTypeGateway  = rbac.ScopeStringGateway
	ActionTypeConvox   = rbac.ScopeStringConvox
	ActionTypeSecurity = rbac.ScopeStringSecurity
)

// Action scope constants (first part of action, e.g., "login" in "login.start")
const (
	ActionScopeLogin                 = "login"
	ActionScopeLogout                = "logout"
	ActionScopeRateLimit             = "rate_limit"
	ActionScopeSuspiciousActivity    = "suspicious_activity"
	ActionScopeMFA                   = rbac.ResourceStringMFA
	ActionScopeAPIToken              = rbac.ResourceStringAPIToken
	ActionScopeDeployApprovalRequest = rbac.ResourceStringDeployApprovalRequest
	ActionScopeRelease               = rbac.ResourceStringRelease
)

// Action verb constants (second part of action, e.g., "start" in "login.start")
const (
	ActionVerbComplete           = "complete"
	ActionVerbOAuthFailed        = "oauth_failed"
	ActionVerbUserNotAuthorized  = "user_not_authorized"
	ActionVerbValidate           = "validate"
	ActionVerbEnroll             = "enroll"
	ActionVerbVerify             = "verify"
	ActionVerbFailed             = "failed"
	ActionVerbBackupCodeUsed     = "backup_code_used"
	ActionVerbBackupCodeRevealed = "backup_code_revealed"
	ActionVerbRequireAllUsers    = "require_all_users"
	ActionVerbReject             = "reject"
	ActionVerbLock               = "lock"
	ActionVerbUnlock             = "unlock"
	ActionVerbUpdateRoles        = "update_roles"
	ActionVerbExport             = "export"
	ActionVerbUnset              = "unset"
	ActionVerbParamsSet          = "params.set"
	ActionVerbExceeded           = "exceeded"
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
