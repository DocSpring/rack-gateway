package rbac

import (
	"encoding/json"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// ============================================================================
// RBAC Permission Enums (for authorization: scope:resource:action)
// ============================================================================

// Scope is an enum for permission scopes
type Scope uint8

const (
	ScopeAuth Scope = iota
	ScopeConvox
	ScopeGateway
	ScopeSecurity
)

const (
	ScopeStringAuth     = "auth"
	ScopeStringConvox   = "convox"
	ScopeStringGateway  = "gateway"
	ScopeStringSecurity = "security"
)

var scopeToString = [...]string{
	ScopeStringAuth,
	ScopeStringConvox,
	ScopeStringGateway,
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
	ResourceBuild
	ResourceCert
	ResourceDeploy
	ResourceEnv
	ResourceInstance
	ResourceLog
	ResourceObject
	ResourceProcess
	ResourceRack
	ResourceRegistry
	ResourceRelease
	ResourceResource
	// Gateway resources
	ResourceAPIToken
	ResourceDeployApprovalRequest
	ResourceIntegration
	ResourceSecret
	ResourceSetting
	ResourceUser
	// Auth/Security resources
	ResourceAuth
	ResourceMFABackupCodes
	ResourceMFAMethod
	ResourceMFAPreferences
	ResourceMFAVerification
	ResourceTrustedDevice
)

const (
	// Convox resources
	ResourceStringApp      = "app"
	ResourceStringBuild    = "build"
	ResourceStringCert     = "cert"
	ResourceStringDeploy   = "deploy"
	ResourceStringEnv      = "env"
	ResourceStringInstance = "instance"
	ResourceStringLog      = "log"
	ResourceStringObject   = "object"
	ResourceStringProcess  = "process"
	ResourceStringRack     = "rack"
	ResourceStringRegistry = "registry"
	ResourceStringRelease  = "release"
	ResourceStringResource = "resource"
	// Gateway resources
	ResourceStringAPIToken              = "api_token"
	ResourceStringDeployApprovalRequest = "deploy_approval_request"
	ResourceStringIntegration           = "integration"
	ResourceStringSecret                = "secret"
	ResourceStringUser                  = "user"
	// Auth/Security resources
	ResourceStringAuth            = "auth"
	ResourceStringMFABackupCodes  = "mfa_backup_codes"
	ResourceStringMFAMethod       = "mfa_method"
	ResourceStringMFAPreferences  = "mfa_preferences"
	ResourceStringMFAVerification = "mfa_verification"
	ResourceStringTrustedDevice   = "trusted_device"
)

var resourceToString = [...]string{
	// Convox resources
	ResourceStringApp,
	ResourceStringBuild,
	ResourceStringCert,
	ResourceStringDeploy,
	ResourceStringEnv,
	ResourceStringInstance,
	ResourceStringLog,
	ResourceStringObject,
	ResourceStringProcess,
	ResourceStringRack,
	ResourceStringRegistry,
	ResourceStringRelease,
	ResourceStringResource,
	// Gateway resources
	ResourceStringAPIToken,
	ResourceStringDeployApprovalRequest,
	ResourceStringIntegration,
	ResourceStringSecret,
	ResourceStringUser,
	// Auth/Security resources
	ResourceStringAuth,
	ResourceStringMFABackupCodes,
	ResourceStringMFAMethod,
	ResourceStringMFAPreferences,
	ResourceStringMFAVerification,
	ResourceStringTrustedDevice,
}

func (r Resource) String() string {
	if int(r) < len(resourceToString) {
		return resourceToString[r]
	}
	return fmt.Sprintf("Resource(%d)", r)
}

func (r Resource) IsValid() bool { return r <= ResourceTrustedDevice }

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
	ActionAdd Action = iota
	ActionApprove
	ActionCreate
	ActionDelete
	ActionDeployWithApproval
	ActionExec
	ActionGenerate
	ActionImport
	ActionKeyroll
	ActionList
	ActionManage
	ActionPromote
	ActionRead
	ActionRemove
	ActionRestart
	ActionSet
	ActionStart
	ActionStop
	ActionTerminate
	ActionUnset
	ActionUpdate
)

const (
	ActionStringAdd                = "add"
	ActionStringApprove            = "approve"
	ActionStringCreate             = "create"
	ActionStringDelete             = "delete"
	ActionStringDeployWithApproval = "deploy_with_approval"
	ActionStringExec               = "exec"
	ActionStringGenerate           = "generate"
	ActionStringImport             = "import"
	ActionStringKeyroll            = "keyroll"
	ActionStringList               = "list"
	ActionStringManage             = "manage"
	ActionStringPromote            = "promote"
	ActionStringRead               = "read"
	ActionStringRemove             = "remove"
	ActionStringRestart            = "restart"
	ActionStringSet                = "set"
	ActionStringStart              = "start"
	ActionStringStop               = "stop"
	ActionStringTerminate          = "terminate"
	ActionStringUnset              = "unset"
	ActionStringUpdate             = "update"
)

var actionToString = [...]string{
	ActionStringAdd,
	ActionStringApprove,
	ActionStringCreate,
	ActionStringDelete,
	ActionStringDeployWithApproval,
	ActionStringExec,
	ActionStringGenerate,
	ActionStringImport,
	ActionStringKeyroll,
	ActionStringList,
	ActionStringManage,
	ActionStringPromote,
	ActionStringRead,
	ActionStringRemove,
	ActionStringRestart,
	ActionStringSet,
	ActionStringStart,
	ActionStringStop,
	ActionStringTerminate,
	ActionStringUnset,
	ActionStringUpdate,
}

func (a Action) String() string {
	if int(a) < len(actionToString) {
		return actionToString[a]
	}
	return fmt.Sprintf("Action(%d)", a)
}

func (a Action) IsValid() bool { return a <= ActionUpdate }

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

// GatewayGlobalSetting builds a global setting permission (gateway:setting:{key})
func GatewayGlobalSetting(key settings.GlobalSettingKey) string {
	return fmt.Sprintf("gateway:setting:%s", key)
}

// GatewayAppSetting builds an app setting permission (gateway:setting:{key})
func GatewayAppSetting(key settings.AppSettingKey) string {
	return fmt.Sprintf("gateway:setting:%s", key)
}
