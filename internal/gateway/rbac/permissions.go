package rbac

import (
	"encoding/json"
	"fmt"
)

// Scope is an enum for permission scopes
type Scope uint8

const (
	ScopeConvox Scope = iota
	ScopeGateway
	ScopeAuth
	ScopeSecurity
)

var scopeToString = [...]string{
	"convox",
	"gateway",
	"auth",
	"security",
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

var resourceToString = [...]string{
	"app",
	"process",
	"build",
	"release",
	"log",
	"object",
	"instance",
	"rack",
	"env",
	"secret",
	"deploy",
	"deploy-approval-request",
	"api-token",
	"user",
	"integration",
	"auth",
	"mfa",
	"mfa-method",
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

var actionToString = [...]string{
	"list",
	"read",
	"create",
	"update",
	"delete",
	"promote",
	"exec",
	"start",
	"stop",
	"terminate",
	"restart",
	"approve",
	"manage",
	"set",
	"deploy-with-approval",
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
