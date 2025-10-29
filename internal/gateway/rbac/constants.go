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

// Scope is an enum for permission scopes
type Scope uint8

const (
	ScopeAuth     Scope = iota // auth
	ScopeConvox                // convox
	ScopeGateway               // gateway
	ScopeSecurity              // security
)

func (s Scope) IsValid() bool { return s <= ScopeSecurity }

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

//go:generate stringer -type=Resource -linecomment

// Resource is an enum for resource types
type Resource uint8

const (
	// Convox resources
	ResourceApp      Resource = iota // app
	ResourceBuild                    // build
	ResourceCert                     // cert
	ResourceDeploy                   // deploy
	ResourceEnv                      // env
	ResourceInstance                 // instance
	ResourceLog                      // log
	ResourceObject                   // object
	ResourceProcess                  // process
	ResourceRack                     // rack
	ResourceRegistry                 // registry
	ResourceRelease                  // release
	ResourceResource                 // resource
	// Gateway resources
	ResourceAPIToken              // api_token
	ResourceDeployApprovalRequest // deploy_approval_request
	ResourceIntegration           // integration
	ResourceJob                   // job
	ResourceSecret                // secret
	ResourceSetting               // setting
	ResourceUser                  // user
	// Auth/Security resources
	ResourceAuth            // auth
	ResourceMFABackupCodes  // mfa_backup_codes
	ResourceMFAMethod       // mfa_method
	ResourceMFAPreferences  // mfa_preferences
	ResourceMFAVerification // mfa_verification
	ResourceTrustedDevice   // trusted_device
)

func (r Resource) IsValid() bool { return r <= ResourceTrustedDevice }

func ParseResource(v string) (Resource, error) {
	// Try each known value
	for r := ResourceApp; r <= ResourceTrustedDevice; r++ {
		if r.String() == v {
			return r, nil
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

//go:generate stringer -type=Action -linecomment

// Action is an enum for action types
type Action uint8

const (
	ActionAdd                Action = iota // add
	ActionApprove                          // approve
	ActionCreate                           // create
	ActionDelete                           // delete
	ActionDeployWithApproval               // deploy_with_approval
	ActionExec                             // exec
	ActionGenerate                         // generate
	ActionImport                           // import
	ActionKeyroll                          // keyroll
	ActionList                             // list
	ActionManage                           // manage
	ActionPromote                          // promote
	ActionRead                             // read
	ActionRemove                           // remove
	ActionRestart                          // restart
	ActionSet                              // set
	ActionStart                            // start
	ActionStop                             // stop
	ActionTerminate                        // terminate
	ActionUnset                            // unset
	ActionUpdate                           // update
	ActionUpdateName                       // update_name
)

func (a Action) IsValid() bool { return a <= ActionUpdateName }

func ParseAction(v string) (Action, error) {
	// Try each known value
	for a := ActionAdd; a <= ActionUpdateName; a++ {
		if a.String() == v {
			return a, nil
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

// GatewayGlobalSettingGroup builds a permission for grouped global settings updates.
func GatewayGlobalSettingGroup(group settings.GlobalSettingGroup) string {
	return fmt.Sprintf("gateway:setting_group:%s", group)
}

// GatewayAppSettingGroup builds a permission for grouped app settings updates.
func GatewayAppSettingGroup(group settings.AppSettingGroup) string {
	return fmt.Sprintf("gateway:setting_group:%s", group)
}
