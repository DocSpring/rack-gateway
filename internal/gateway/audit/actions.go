package audit

// Action type constants
const (
	ActionTypeAuth     = "auth"
	ActionTypeGateway  = "gateway"
	ActionTypeConvox   = "convox"
	ActionTypeSecurity = "security"
)

// Auth action constants
const (
	ActionAuthLoginStart        = "login.start"
	ActionAuthLoginComplete     = "login.complete"
	ActionAuthLoginMFA          = "login.mfa"
	ActionAuthLogout            = "logout"
	ActionAuthTokenValidate     = "token.validate"
	ActionMFAEnroll             = "mfa.enroll"
	ActionMFAVerify             = "mfa.verify"
	ActionMFAVerifyFailed       = "mfa.verify.failed"
	ActionMFAUpdate             = "mfa.update"
	ActionMFABackupCodeUsed     = "mfa.backup-code-used"
	ActionMFABackupCodeRevealed = "mfa.backup-code-revealed"
)

// Gateway action constants
const (
	ActionDeployApprovalRequestCreate  = "deploy-approval-request.create"
	ActionDeployApprovalRequestApprove = "deploy-approval-request.approve"
	ActionDeployApprovalRequestReject  = "deploy-approval-request.reject"
	ActionAPITokenCreate               = "api-token.create"
	ActionAPITokenUpdate               = "api-token.update"
	ActionAPITokenDelete               = "api-token.delete"
	ActionUserRoleAdd                  = "user.role.add"
	ActionUserRoleRemove               = "user.role.remove"
)

// Convox action constants
const (
	ActionEnvRead        = "env.read"
	ActionEnvUpdate      = "env.update"
	ActionEnvSet         = "env.set"
	ActionEnvUnset       = "env.unset"
	ActionSecretsRead    = "secrets.read"
	ActionReleasePromote = "release.promote"
)

// Security action constants
const (
	ActionRateLimitExceeded  = "rate_limit.exceeded"
	ActionSuspiciousActivity = "suspicious_activity"
)

// Resource type constants
const (
	ResourceTypeAuth                  = "auth"
	ResourceTypeMFAMethod             = "mfa_method"
	ResourceTypeDeployApprovalRequest = "deploy-approval-request"
	ResourceTypeAPIToken              = "api-token"
	ResourceTypeUser                  = "user"
	ResourceTypeEnv                   = "env"
	ResourceTypeSecret                = "secret"
	ResourceTypeRelease               = "release"
	ResourceTypeSecurity              = "security"
)

// Status constants
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusDenied  = "denied"
	StatusError   = "error"
	StatusAlert   = "alert"
)
