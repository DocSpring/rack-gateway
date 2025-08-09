package rbac

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"gopkg.in/yaml.v3"
)

type Manager struct {
	enforcer         *casbin.Enforcer
	users            map[string]*User
	roles            map[string]*Role
	policies         map[string]*Policy  // Legacy, kept for compatibility
	compiledPolicies map[string]*PolicyDef // New compiled-in policies
	mu               sync.RWMutex
	configPaths      ConfigPaths
}

type ConfigPaths struct {
	UsersPath    string
	RolesPath    string
	PoliciesPath string
}

type User struct {
	Email string   `yaml:"email"`
	Name  string   `yaml:"name"`
	Roles []string `yaml:"roles"`
}

type Role struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Permissions []string `yaml:"permissions"`
}

// Policy is the runtime representation (kept for compatibility)
type Policy struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Inherits    string   `yaml:"inherits,omitempty"`
	Routes      []string `yaml:"routes"`
}

type Rule struct {
	Resource string   `yaml:"resource"`
	Actions  []string `yaml:"actions"`
	Effect   string   `yaml:"effect"`
	Racks    []string `yaml:"racks,omitempty"`
}

func NewManager(paths ConfigPaths) (*Manager, error) {
	m := &Manager{
		users:            make(map[string]*User),
		roles:            make(map[string]*Role),
		policies:         make(map[string]*Policy),
		compiledPolicies: make(map[string]*PolicyDef),
		configPaths:      paths,
	}

	modelText := `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (p.obj == "*" || keyMatch3(r.obj, p.obj) || keyMatch3Multi(r.obj, p.obj)) && (p.act == "*" || r.act == p.act)
`

	casbinModel, err := model.NewModelFromString(modelText)
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	enforcer, err := casbin.NewEnforcer(casbinModel)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}

	// Register our custom multi-segment matcher
	enforcer.AddFunction("keyMatch3Multi", keyMatch3Multi)

	m.enforcer = enforcer

	if err := m.LoadConfigs(); err != nil {
		return nil, fmt.Errorf("failed to load configs: %w", err)
	}

	return m, nil
}

func (m *Manager) LoadConfigs() error {
	if err := m.loadUsers(); err != nil {
		return fmt.Errorf("failed to load users: %w", err)
	}

	if err := m.loadRoles(); err != nil {
		return fmt.Errorf("failed to load roles: %w", err)
	}

	if err := m.loadPolicies(); err != nil {
		return fmt.Errorf("failed to load policies: %w", err)
	}

	return m.buildPolicyRules()
}

func (m *Manager) loadUsers() error {
	if _, err := os.Stat(m.configPaths.UsersPath); os.IsNotExist(err) {
		m.createDefaultUsers()
		return nil
	}

	data, err := os.ReadFile(m.configPaths.UsersPath)
	if err != nil {
		return err
	}

	var users map[string]*User
	if err := yaml.Unmarshal(data, &users); err != nil {
		return err
	}

	m.mu.Lock()
	m.users = users
	m.mu.Unlock()

	return nil
}

func (m *Manager) loadRoles() error {
	if _, err := os.Stat(m.configPaths.RolesPath); os.IsNotExist(err) {
		m.createDefaultRoles()
		return nil
	}

	data, err := os.ReadFile(m.configPaths.RolesPath)
	if err != nil {
		return err
	}

	var roles map[string]*Role
	if err := yaml.Unmarshal(data, &roles); err != nil {
		return err
	}

	m.mu.Lock()
	m.roles = roles
	m.mu.Unlock()

	return nil
}

func (m *Manager) loadPolicies() error {
	// Use compiled-in policies
	policies := make(map[string]*PolicyDef)
	for name, def := range DefaultPolicies {
		// Create a copy to avoid modifying the original
		policyCopy := &PolicyDef{
			Description: def.Description,
			Inherits:    def.Inherits,
			Routes:      make([]Route, len(def.Routes)),
		}
		copy(policyCopy.Routes, def.Routes)
		policies[name] = policyCopy
	}

	// Resolve inheritance
	ResolveInheritance(policies)

	// Store the resolved policies
	m.mu.Lock()
	m.compiledPolicies = policies
	m.mu.Unlock()

	return nil
}

func (m *Manager) buildPolicyRules() error {
	m.enforcer.ClearPolicy()

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Add users to roles
	for email, user := range m.users {
		for _, roleName := range user.Roles {
			m.enforcer.AddGroupingPolicy(email, roleName)
		}
	}

	// Add compiled policies (roles) with their routes
	for policyName, policy := range m.compiledPolicies {
		for _, route := range policy.Routes {
			method := route.Method
			path := route.Path

			// Keep paths with {} patterns for keyMatch3
			// keyMatch3 supports {param} for single segments and {param:.*} for multi-segments
			// No conversion needed - use paths as-is
			m.enforcer.AddPolicy(policyName, path, method)
		}
	}

	// Legacy: keep roles if they still exist
	for roleName, role := range m.roles {
		for _, permission := range role.Permissions {
			// For backwards compatibility with old permission format
			if permission == "convox:*:*" {
				m.enforcer.AddPolicy(roleName, "*", "*")
			}
		}
	}

	return nil
}

func (m *Manager) CheckPermission(email, path, method string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if user exists - unknown users are denied
	if _, exists := m.users[email]; !exists {
		return false
	}

	// First check if they have explicit SOCKET permission for WebSocket routes
	if method == MethodSocket {
		allowed, err := m.enforcer.Enforce(email, path, MethodSocket)
		if err == nil && allowed {
			return true
		}
		// If no explicit SOCKET permission, WebSocket connections fall back to GET
		method = MethodGet
	}

	allowed, err := m.enforcer.Enforce(email, path, method)
	if err != nil {
		return false
	}

	return allowed
}

func (m *Manager) GetUserRoles(email string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if user, exists := m.users[email]; exists {
		return user.Roles
	}

	// Unknown users have no roles
	return []string{}
}

func (m *Manager) AddUser(email, name string, roles []string) error {
	m.mu.Lock()
	m.users[email] = &User{
		Email: email,
		Name:  name,
		Roles: roles,
	}
	m.mu.Unlock()

	if err := m.saveUsers(); err != nil {
		return err
	}

	// Rebuild policy rules after adding user
	return m.buildPolicyRules()
}

func (m *Manager) saveUsers() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := yaml.Marshal(m.users)
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.configPaths.UsersPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(m.configPaths.UsersPath, data, 0644)
}

func (m *Manager) createDefaultUsers() {
	// No default users - all users must be explicitly configured
	// via the mounted users.yaml file
	m.users = make(map[string]*User)
}

func (m *Manager) createDefaultRoles() {
	m.roles = map[string]*Role{
		"viewer": {
			Name:        "viewer",
			Description: "Read-only access",
			Permissions: []string{
				"convox:apps:list",
				"convox:ps:list",
				"convox:env:get",
				"convox:logs:read",
			},
		},
		"ops": {
			Name:        "ops",
			Description: "Operations team access",
			Permissions: []string{
				"convox:apps:*",
				"convox:ps:*",
				"convox:env:get",
				"convox:logs:*",
				"convox:restart:*",
			},
		},
		"deployer": {
			Name:        "deployer",
			Description: "Deployment access",
			Permissions: []string{
				"convox:apps:*",
				"convox:ps:*",
				"convox:env:*",
				"convox:logs:*",
				"convox:restart:*",
				"convox:run:*",
				"convox:build:*",
				"convox:deploy:*",
			},
		},
		"admin": {
			Name:        "admin",
			Description: "Full admin access",
			Permissions: []string{
				"convox:*:*",
			},
		},
	}
}

func (m *Manager) createDefaultPolicies() {
	// No longer needed - policies are compiled in
	// Just initialize empty map for legacy compatibility
	m.policies = make(map[string]*Policy)
}

func (m *Manager) GetUsers() map[string]*User {
	m.mu.RLock()
	defer m.mu.RUnlock()

	users := make(map[string]*User)
	for k, v := range m.users {
		users[k] = v
	}
	return users
}

func (m *Manager) GetRoles() map[string]*Role {
	m.mu.RLock()
	defer m.mu.RUnlock()

	roles := make(map[string]*Role)
	for k, v := range m.roles {
		roles[k] = v
	}
	return roles
}