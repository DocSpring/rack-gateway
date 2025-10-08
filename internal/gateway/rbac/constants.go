package rbac

import (
	"encoding/json"
	"fmt"
)

// ============================================================================
// RBAC Permission Enums (for authorization: scope:resource:action)
// ============================================================================

// Scope is an enum for permission scopes
type Scope uint8

const (
	ScopeConvox Scope = iota
	ScopeGateway
	ScopeAuth
	ScopeSecurity
)

const (
	ScopeStringConvox   = "convox"
	ScopeStringGateway  = "gateway"
	ScopeStringAuth     = "auth"
	ScopeStringSecurity = "security"
)

var scopeToString = [...]string{
	ScopeStringConvox,
	ScopeStringGateway,
	ScopeStringAuth,
	ScopeStringSecurity,
}

func (s Scope) String() string {
	if int(s) < len(scopeToString) {
		return scopeToString[s]
	}
	return fmt.Sprintf("Scope(%d)", s)
}

func (s Scope) IsValid() bool { return s <= ScopeSecurity }

func ParseScope(v string) (Scope, error) {
	switch v {
	case ScopeStringConvox:
		return ScopeConvox, nil
	case ScopeStringGateway:
		return ScopeGateway, nil
	case ScopeStringAuth:
		return ScopeAuth, nil
	case ScopeStringSecurity:
		return ScopeSecurity, nil
	default:
		return 0, fmt.Errorf("invalid scope %q", v)
	}
}

func (s Scope) MarshalText() ([]byte, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid scope %d", s)
	}
	return []byte(s.String()), nil
}

func (s *Scope) UnmarshalText(b []byte) error {
	v, err := ParseScope(string(b))
	if err != nil {
		return err
	}
	*s = v
	return nil
}

func (s Scope) MarshalJSON() ([]byte, error) {
	t, err := s.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(t))
}

func (s *Scope) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return s.UnmarshalText([]byte(v))
}

// Resource is an enum for resource types
type Resource uint8

const (
	// Convox resources
	ResourceApp Resource = iota
	ResourceProcess
	ResourceBuild
	ResourceRelease
	ResourceLog
	ResourceObject
	ResourceInstance
	ResourceRack
	ResourceEnv
	ResourceSecret
	ResourceDeploy
	// Gateway resources
	ResourceDeployApprovalRequest
	ResourceAPIToken
	ResourceUser
	ResourceIntegration
	// Auth/Security resources
	ResourceAuth
	ResourceMFA
	ResourceMFAMethod
)

const (
	ResourceStringApp                   = "app"
	ResourceStringProcess               = "process"
	ResourceStringBuild                 = "build"
	ResourceStringRelease               = "release"
	ResourceStringLog                   = "log"
	ResourceStringObject                = "object"
	ResourceStringInstance              = "instance"
	ResourceStringRack                  = "rack"
	ResourceStringEnv                   = "env"
	ResourceStringSecret                = "secret"
	ResourceStringDeploy                = "deploy"
	ResourceStringDeployApprovalRequest = "deploy-approval-request"
	ResourceStringAPIToken              = "api-token"
	ResourceStringUser                  = "user"
	ResourceStringIntegration           = "integration"
	ResourceStringAuth                  = "auth"
	ResourceStringMFA                   = "mfa"
	ResourceStringMFAMethod             = "mfa-method"
)

var resourceToString = [...]string{
	ResourceStringApp,
	ResourceStringProcess,
	ResourceStringBuild,
	ResourceStringRelease,
	ResourceStringLog,
	ResourceStringObject,
	ResourceStringInstance,
	ResourceStringRack,
	ResourceStringEnv,
	ResourceStringSecret,
	ResourceStringDeploy,
	ResourceStringDeployApprovalRequest,
	ResourceStringAPIToken,
	ResourceStringUser,
	ResourceStringIntegration,
	ResourceStringAuth,
	ResourceStringMFA,
	ResourceStringMFAMethod,
}

func (r Resource) String() string {
	if int(r) < len(resourceToString) {
		return resourceToString[r]
	}
	return fmt.Sprintf("Resource(%d)", r)
}

func (r Resource) IsValid() bool { return r <= ResourceMFAMethod }

func ParseResource(v string) (Resource, error) {
	for i, s := range resourceToString {
		if s == v {
			return Resource(i), nil
		}
	}
	return 0, fmt.Errorf("invalid resource %q", v)
}

func (r Resource) MarshalText() ([]byte, error) {
	if !r.IsValid() {
		return nil, fmt.Errorf("invalid resource %d", r)
	}
	return []byte(r.String()), nil
}

func (r *Resource) UnmarshalText(b []byte) error {
	v, err := ParseResource(string(b))
	if err != nil {
		return err
	}
	*r = v
	return nil
}

func (r Resource) MarshalJSON() ([]byte, error) {
	t, err := r.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(t))
}

func (r *Resource) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return r.UnmarshalText([]byte(v))
}

// Action is an enum for action types
type Action uint8

const (
	ActionList Action = iota
	ActionRead
	ActionCreate
	ActionUpdate
	ActionDelete
	ActionPromote
	ActionExec
	ActionStart
	ActionStop
	ActionTerminate
	ActionRestart
	ActionApprove
	ActionManage
	ActionSet
	ActionDeployWithApproval
)

const (
	ActionStringList               = "list"
	ActionStringRead               = "read"
	ActionStringCreate             = "create"
	ActionStringUpdate             = "update"
	ActionStringDelete             = "delete"
	ActionStringPromote            = "promote"
	ActionStringExec               = "exec"
	ActionStringStart              = "start"
	ActionStringStop               = "stop"
	ActionStringTerminate          = "terminate"
	ActionStringRestart            = "restart"
	ActionStringApprove            = "approve"
	ActionStringManage             = "manage"
	ActionStringSet                = "set"
	ActionStringDeployWithApproval = "deploy-with-approval"
)

var actionToString = [...]string{
	ActionStringList,
	ActionStringRead,
	ActionStringCreate,
	ActionStringUpdate,
	ActionStringDelete,
	ActionStringPromote,
	ActionStringExec,
	ActionStringStart,
	ActionStringStop,
	ActionStringTerminate,
	ActionStringRestart,
	ActionStringApprove,
	ActionStringManage,
	ActionStringSet,
	ActionStringDeployWithApproval,
}

func (a Action) String() string {
	if int(a) < len(actionToString) {
		return actionToString[a]
	}
	return fmt.Sprintf("Action(%d)", a)
}

func (a Action) IsValid() bool { return a <= ActionDeployWithApproval }

func ParseAction(v string) (Action, error) {
	for i, s := range actionToString {
		if s == v {
			return Action(i), nil
		}
	}
	return 0, fmt.Errorf("invalid action %q", v)
}

func (a Action) MarshalText() ([]byte, error) {
	if !a.IsValid() {
		return nil, fmt.Errorf("invalid action %d", a)
	}
	return []byte(a.String()), nil
}

func (a *Action) UnmarshalText(b []byte) error {
	v, err := ParseAction(string(b))
	if err != nil {
		return err
	}
	*a = v
	return nil
}

func (a Action) MarshalJSON() ([]byte, error) {
	t, err := a.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(t))
}

func (a *Action) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return a.UnmarshalText([]byte(v))
}

// Permission builds a permission string from scope, resource, and action
func Permission(scope Scope, resource Resource, action Action) string {
	return fmt.Sprintf("%s:%s:%s", scope, resource, action)
}

// Convox builds a Convox permission
func Convox(resource Resource, action Action) string {
	return Permission(ScopeConvox, resource, action)
}

// Gateway builds a Gateway permission
func Gateway(resource Resource, action Action) string {
	return Permission(ScopeGateway, resource, action)
}

// Auth builds an Auth permission
func Auth(resource Resource, action Action) string {
	return Permission(ScopeAuth, resource, action)
}

// Security builds a Security permission
func Security(resource Resource, action Action) string {
	return Permission(ScopeSecurity, resource, action)
}

// ============================================================================
// Audit Log String Constants
// ============================================================================

// Action type constants (high-level categorization for audit logs)
const (
	ActionTypeAuth     = ScopeStringAuth
	ActionTypeGateway  = ScopeStringGateway
	ActionTypeConvox   = ScopeStringConvox
	ActionTypeSecurity = ScopeStringSecurity
)

// Additional resource strings for audit log actions (not in RBAC Resource enum)
const (
	ResourceStringLogin              = "login"
	ResourceStringLogout             = "logout"
	ResourceStringToken              = "token"
	ResourceStringAudit              = "audit"
	ResourceStringRateLimit          = "rate_limit"
	ResourceStringSuspiciousActivity = "suspicious_activity"
)

// Action verb strings (second part of audit log action, e.g., "start" in rbac.BuildAction(rbac.ResourceStringLogin, rbac.ActionStringStart))
const (
	ActionStringComplete           = "complete"
	ActionStringOAuthFailed        = "oauth_failed"
	ActionStringUserNotAuthorized  = "user_not_authorized"
	ActionStringValidate           = "validate"
	ActionStringEnroll             = "enroll"
	ActionStringVerify             = "verify"
	ActionStringFailed             = "failed"
	ActionStringBackupCodeUsed     = "backup-code-used"
	ActionStringBackupCodeRevealed = "backup-code-revealed"
	ActionStringRequireAllUsers    = "require_all_users"
	ActionStringReject             = "reject"
	ActionStringLock               = "lock"
	ActionStringUnlock             = "unlock"
	ActionStringUpdateRoles        = "update_roles"
	ActionStringExport             = "export"
	ActionStringUnset              = "unset"
	ActionStringParamsSet          = "params.set"
	ActionStringExceeded           = "exceeded"
)

// Audit log status constants
const (
	StatusStringSuccess = "success"
	StatusStringFailed  = "failed"
	StatusStringDenied  = "denied"
	StatusStringError   = "error"
	StatusStringAlert   = "alert"
)

// BuildAction builds an audit log action string from scope and verb (e.g., rbac.BuildAction(rbac.ResourceStringLogin, rbac.ActionStringStart))
func BuildAction(scope, verb string) string {
	return scope + "." + verb
}
