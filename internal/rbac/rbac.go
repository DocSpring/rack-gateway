package rbac

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"gopkg.in/yaml.v3"
)

type Manager struct {
	enforcer    *casbin.Enforcer
	users       map[string]*User
	roles       map[string]*Role
	policies    map[string]*Policy
	mu          sync.RWMutex
	configPaths ConfigPaths
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

type Policy struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Rules       []Rule   `yaml:"rules"`
}

type Rule struct {
	Resource string   `yaml:"resource"`
	Actions  []string `yaml:"actions"`
	Effect   string   `yaml:"effect"`
	Racks    []string `yaml:"racks,omitempty"`
}

func NewManager(paths ConfigPaths) (*Manager, error) {
	m := &Manager{
		users:       make(map[string]*User),
		roles:       make(map[string]*Role),
		policies:    make(map[string]*Policy),
		configPaths: paths,
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
m = g(r.sub, p.sub) && (p.obj == "*" || keyMatch2(r.obj, p.obj)) && (p.act == "*" || regexMatch(r.act, p.act))
`

	casbinModel, err := model.NewModelFromString(modelText)
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	enforcer, err := casbin.NewEnforcer(casbinModel)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}

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
	if _, err := os.Stat(m.configPaths.PoliciesPath); os.IsNotExist(err) {
		m.createDefaultPolicies()
		return nil
	}

	data, err := os.ReadFile(m.configPaths.PoliciesPath)
	if err != nil {
		return err
	}

	var policies map[string]*Policy
	if err := yaml.Unmarshal(data, &policies); err != nil {
		return err
	}

	m.mu.Lock()
	m.policies = policies
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

	// Add role permissions
	for roleName, role := range m.roles {
		for _, permission := range role.Permissions {
			// Handle wildcard permissions
			if permission == "convox:*:*" {
				m.enforcer.AddPolicy(roleName, "*", "*")
				continue
			}

			parts := strings.Split(permission, ":")
			if len(parts) == 3 {
				resource := parts[1]
				action := parts[2]
				
				// Map permissions to HTTP resources
				if resource == "apps" {
					if action == "list" || action == "*" {
						m.enforcer.AddPolicy(roleName, "/apps", "GET")
						m.enforcer.AddPolicy(roleName, "/apps/*", "GET")
					}
					if action == "*" {
						m.enforcer.AddPolicy(roleName, "/apps", "POST")
						m.enforcer.AddPolicy(roleName, "/apps/*", "POST")
						m.enforcer.AddPolicy(roleName, "/apps/*", "PUT")
						m.enforcer.AddPolicy(roleName, "/apps/*", "DELETE")
					}
				} else if resource == "ps" {
					if action == "list" || action == "*" {
						m.enforcer.AddPolicy(roleName, "/ps", "GET")
						m.enforcer.AddPolicy(roleName, "/ps/*", "GET")
					}
					if action == "*" {
						m.enforcer.AddPolicy(roleName, "/ps/*", "POST")
					}
				} else if resource == "env" {
					if action == "get" || action == "*" {
						m.enforcer.AddPolicy(roleName, "/env/*", "GET")
					}
					if action == "set" || action == "*" {
						m.enforcer.AddPolicy(roleName, "/env/*", "POST")
						m.enforcer.AddPolicy(roleName, "/env/*", "PUT")
					}
				} else if resource == "logs" {
					m.enforcer.AddPolicy(roleName, "/logs/*", "GET")
				} else if resource == "restart" {
					m.enforcer.AddPolicy(roleName, "/restart/*", "POST")
				} else if resource == "run" {
					m.enforcer.AddPolicy(roleName, "/run/*", "POST")
				} else if resource == "build" {
					m.enforcer.AddPolicy(roleName, "/build/*", "POST")
				} else if resource == "deploy" {
					m.enforcer.AddPolicy(roleName, "/deploy/*", "POST")
				}
			}
		}
	}

	return nil
}

func (m *Manager) CheckPermission(email, resource, action string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allowed, err := m.enforcer.Enforce(email, resource, action)
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

	return []string{"viewer"}
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
	m.users = map[string]*User{
		"admin@docspring.com": {
			Email: "admin@docspring.com",
			Name:  "Admin User",
			Roles: []string{"admin"},
		},
	}
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
	m.policies = map[string]*Policy{
		"default": {
			Name:        "default",
			Description: "Default policy rules",
			Rules: []Rule{
				{
					Resource: "/apps/*",
					Actions:  []string{"GET"},
					Effect:   "allow",
				},
				{
					Resource: "/ps/*",
					Actions:  []string{"GET"},
					Effect:   "allow",
				},
			},
		},
	}
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