package rbac

import (
	"encoding/json"
	"fmt"

	"github.com/DocSpring/rack-gateway/internal/gateway/settings"
)

// ============================================================================
// RBAC Permission Enums (for authorization: scope:resource:action)
// ============================================================================

//go:generate stringer -type=Scope -linecomment

// Scope enumerates the permission scopes that map to the first segment of
// permissions (e.g. `gateway:setting:set`).
type Scope uint8

const (
	// ScopeAuth covers authentication-related permissions.
	ScopeAuth Scope = iota // auth
	// ScopeConvox covers permissions passed through to the Convox API.
	ScopeConvox // convox
	// ScopeGateway covers permissions implemented within the gateway itself.
	ScopeGateway // gateway
	// ScopeSecurity covers security-specific operations.
	ScopeSecurity // security
)

// IsValid reports whether the scope represents a defined value.
func (s Scope) IsValid() bool { return s <= ScopeSecurity }

// ParseScope converts a string name into a Scope value.
func ParseScope(v string) (Scope, error) {
	switch v {
	case "convox":
		return ScopeConvox, nil
	case "gateway":
		return ScopeGateway, nil
	case "auth":
		return ScopeAuth, nil
	case "security":
		return ScopeSecurity, nil
	default:
		return 0, fmt.Errorf("invalid scope %q", v)
	}
}

// MarshalText encodes the scope to its string representation.
func (s Scope) MarshalText() ([]byte, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid scope %d", s)
	}
	return []byte(s.String()), nil
}

// UnmarshalText decodes a textual scope value.
func (s *Scope) UnmarshalText(b []byte) error {
	v, err := ParseScope(string(b))
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// MarshalJSON encodes the scope for JSON payloads.
func (s Scope) MarshalJSON() ([]byte, error) {
	t, err := s.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(t))
}

// UnmarshalJSON decodes the scope from JSON payloads.
func (s *Scope) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return s.UnmarshalText([]byte(v))
}

//go:generate stringer -type=Resource -linecomment

// Resource enumerates gateway resource types used in permission strings.
type Resource uint8

const (
	// ResourceApp identifies a Convox app resource.
	ResourceApp Resource = iota // app
	// ResourceBuild identifies a Convox build resource.
	ResourceBuild // build
	// ResourceCert identifies a Convox certificate resource.
	ResourceCert // cert
	// ResourceDeploy identifies a Convox deploy resource.
	ResourceDeploy // deploy
	// ResourceEnv identifies a Convox environment resource.
	ResourceEnv // env
	// ResourceInstance identifies a Convox instance resource.
	ResourceInstance // instance
	// ResourceLog identifies a Convox log resource.
	ResourceLog // log
	// ResourceObject identifies a Convox object resource.
	ResourceObject // object
	// ResourceProcess identifies a Convox process resource.
	ResourceProcess // process
	// ResourceRack identifies a Convox rack resource.
	ResourceRack // rack
	// ResourceRegistry identifies a Convox registry resource.
	ResourceRegistry // registry
	// ResourceRelease identifies a Convox release resource.
	ResourceRelease // release
	// ResourceResource identifies a generic Convox resource.
	ResourceResource // resource
	// ResourceAPIToken identifies a gateway API token resource.
	ResourceAPIToken // api_token
	// ResourceDeployApprovalRequest identifies a deploy approval request.
	ResourceDeployApprovalRequest // deploy_approval_request
	// ResourceIntegration identifies a gateway integration resource.
	ResourceIntegration // integration
	// ResourceJob identifies a gateway background job resource.
	ResourceJob // job
	// ResourceSecret identifies a gateway secret resource.
	ResourceSecret // secret
	// ResourceSetting identifies a gateway setting resource.
	ResourceSetting // setting
	// ResourceUser identifies a gateway user resource.
	ResourceUser // user
	// ResourceAuth identifies an auth resource.
	ResourceAuth // auth
	// ResourceMFABackupCodes identifies MFA backup code resources.
	ResourceMFABackupCodes // mfa_backup_codes
	// ResourceMFAMethod identifies MFA method resources.
	ResourceMFAMethod // mfa_method
	// ResourceMFAPreferences identifies MFA preference resources.
	ResourceMFAPreferences // mfa_preferences
	// ResourceMFAVerification identifies MFA verification resources.
	ResourceMFAVerification // mfa_verification
	// ResourceTrustedDevice identifies trusted device resources.
	ResourceTrustedDevice // trusted_device
)

// IsValid reports whether the resource represents a defined value.
func (r Resource) IsValid() bool { return r <= ResourceTrustedDevice }

// ParseResource converts a string name into a Resource value.
func ParseResource(v string) (Resource, error) {
	// Try each known value
	for r := ResourceApp; r <= ResourceTrustedDevice; r++ {
		if r.String() == v {
			return r, nil
		}
	}
	return 0, fmt.Errorf("invalid resource %q", v)
}

// MarshalText encodes the resource to its string representation.
func (r Resource) MarshalText() ([]byte, error) {
	if !r.IsValid() {
		return nil, fmt.Errorf("invalid resource %d", r)
	}
	return []byte(r.String()), nil
}

// UnmarshalText decodes a textual resource value.
func (r *Resource) UnmarshalText(b []byte) error {
	v, err := ParseResource(string(b))
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// MarshalJSON encodes the resource to JSON.
func (r Resource) MarshalJSON() ([]byte, error) {
	t, err := r.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(t))
}

// UnmarshalJSON decodes the resource from JSON.
func (r *Resource) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return r.UnmarshalText([]byte(v))
}

//go:generate stringer -type=Action -linecomment

// Action enumerates the allowed actions within a scope/resource pair.
type Action uint8

const (
	// ActionAdd represents an add operation on a resource.
	ActionAdd Action = iota // add
	// ActionApprove represents an approval operation.
	ActionApprove // approve
	// ActionCreate represents a creation operation.
	ActionCreate // create
	// ActionDelete represents a deletion operation.
	ActionDelete // delete
	// ActionDeployWithApproval represents a deploy operation requiring approval.
	ActionDeployWithApproval // deploy_with_approval
	// ActionExec represents executing a command against a resource.
	ActionExec // exec
	// ActionGenerate represents a generate operation (e.g., credentials).
	ActionGenerate // generate
	// ActionImport represents an import operation.
	ActionImport // import
	// ActionKeyroll represents a key rotation operation.
	ActionKeyroll // keyroll
	// ActionList represents listing resources.
	ActionList // list
	// ActionManage represents management operations.
	ActionManage // manage
	// ActionPromote represents promoting a resource (e.g., release).
	ActionPromote // promote
	// ActionRead represents read-only access.
	ActionRead // read
	// ActionRemove represents removing associations.
	ActionRemove // remove
	// ActionRestart signifies restarting a resource.
	ActionRestart // restart
	// ActionSet represents setting a value.
	ActionSet // set
	// ActionStart represents starting a resource.
	ActionStart // start
	// ActionStop represents stopping a resource.
	ActionStop // stop
	// ActionTerminate represents terminating a resource.
	ActionTerminate // terminate
	// ActionUnset represents unsetting a value.
	ActionUnset // unset
	// ActionUpdate represents updating a resource.
	ActionUpdate // update
	// ActionUpdateName represents updating only the resource name.
	ActionUpdateName // update_name
)

// IsValid reports whether the action represents a defined value.
func (a Action) IsValid() bool { return a <= ActionUpdateName }

// ParseAction converts a string name into an Action value.
func ParseAction(v string) (Action, error) {
	// Try each known value
	for a := ActionAdd; a <= ActionUpdateName; a++ {
		if a.String() == v {
			return a, nil
		}
	}
	return 0, fmt.Errorf("invalid action %q", v)
}

// MarshalText encodes the action to its string representation.
func (a Action) MarshalText() ([]byte, error) {
	if !a.IsValid() {
		return nil, fmt.Errorf("invalid action %d", a)
	}
	return []byte(a.String()), nil
}

// UnmarshalText decodes a textual action value.
func (a *Action) UnmarshalText(b []byte) error {
	v, err := ParseAction(string(b))
	if err != nil {
		return err
	}
	*a = v
	return nil
}

// MarshalJSON encodes the action to JSON.
func (a Action) MarshalJSON() ([]byte, error) {
	t, err := a.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(t))
}

// UnmarshalJSON decodes the action from JSON.
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

// GatewayGlobalSettingGroup builds a permission for grouped global settings updates.
func GatewayGlobalSettingGroup(group settings.GlobalSettingGroup) string {
	return fmt.Sprintf("gateway:setting_group:%s", group)
}

// GatewayAppSettingGroup builds a permission for grouped app settings updates.
func GatewayAppSettingGroup(group settings.AppSettingGroup) string {
	return fmt.Sprintf("gateway:setting_group:%s", group)
}
