package rbac

import (
	"fmt"
	"sync"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

// DBManager implements RBAC using the database
type DBManager struct {
	db       *db.Database
	enforcer *casbin.Enforcer
	mu       sync.RWMutex
	domain   string
}

const modelConf = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (p.obj == "convox:*:*" || p.obj == r.obj || keyMatch3(r.obj, p.obj)) && (p.act == "*" || r.act == p.act)
`

// NewDBManager creates a new RBAC manager using the database
func NewDBManager(database *db.Database, domain string) (*DBManager, error) {
	// Create Casbin model
	m, err := model.NewModelFromString(modelConf)
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	// Create enforcer with in-memory adapter
	adapter := &memoryAdapter{policies: policies}
	enforcer, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}

	manager := &DBManager{
		db:       database,
		enforcer: enforcer,
		domain:   domain,
	}

	// Policies are already loaded via the adapter in NewEnforcer
	// Just sync users from database
	if err := manager.syncUsersFromDB(); err != nil {
		return nil, fmt.Errorf("failed to sync users: %w", err)
	}

	return manager, nil
}

// Define the embedded RBAC policies
var policies = [][]string{
	// Viewer role permissions
	{"p", "viewer", "convox:apps:list", "*"},
	{"p", "viewer", "convox:ps:list", "*"},
	{"p", "viewer", "convox:logs:read", "*"},
	{"p", "viewer", "convox:builds:list", "*"},
	{"p", "viewer", "convox:rack:read", "*"},

	// Ops role inherits viewer and adds more
	{"g", "ops", "viewer"},
	{"p", "ops", "convox:ps:manage", "*"},
	{"p", "ops", "convox:restart:app", "*"},
	{"p", "ops", "convox:releases:list", "*"},
	{"p", "ops", "convox:env:view", "*"},

	// Deployer role inherits ops and adds deployment permissions
	{"g", "deployer", "ops"},
	{"p", "deployer", "convox:builds:create", "*"},
	{"p", "deployer", "convox:releases:create", "*"},
	{"p", "deployer", "convox:releases:promote", "*"},
	{"p", "deployer", "convox:env:view", "*"},
	{"p", "deployer", "convox:env:set", "*"},
	// Allow creating and updating apps/services, but not deleting apps
	{"p", "deployer", "convox:apps:create", "*"},
	{"p", "deployer", "convox:apps:update", "*"},

	// Admin role has all permissions
	{"p", "admin", "convox:*:*", "*"},

	// CI/CD role for automated deployments
	{"p", "cicd", "convox:apps:list", "*"},
	{"p", "cicd", "convox:builds:create", "*"},
	{"p", "cicd", "convox:releases:create", "*"},
	{"p", "cicd", "convox:releases:promote", "*"},
	{"p", "cicd", "convox:ps:manage", "*"},
	{"p", "cicd", "convox:restart:app", "*"},
}

// syncUsersFromDB loads user-role mappings from the database
func (m *DBManager) syncUsersFromDB() error {
	users, err := m.db.ListUsers()
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	// Add user-role mappings from database
	for _, user := range users {
		if user.Suspended {
			continue // Skip suspended users
		}
		for _, role := range user.Roles {
			// AddGroupingPolicy returns false if already exists, but no error
			m.enforcer.AddGroupingPolicy(user.Email, role)
		}
	}

	return nil
}

// Enforce checks if a user has permission to perform an action
func (m *DBManager) Enforce(userEmail, resource, action string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// First check if user exists and is not suspended
	user, err := m.db.GetUser(userEmail)
	if err != nil {
		return false, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return false, nil // User doesn't exist
	}
	if user.Suspended {
		return false, nil // User is suspended
	}

	// Format permission as convox:resource:action
	permission := fmt.Sprintf("convox:%s:%s", resource, action)

	// Check permission using Casbin with 3 parameters (sub, obj, act)
	// The third parameter is "*" as we don't use it in our model
	ok, err := m.enforcer.Enforce(userEmail, permission, "*")
	if err != nil {
		return false, fmt.Errorf("failed to enforce: %w", err)
	}

	return ok, nil
}

// GetAllowedDomain returns the configured domain
func (m *DBManager) GetAllowedDomain() string {
	return m.domain
}

// GetUser returns a user's configuration from the database
func (m *DBManager) GetUser(email string) (*UserConfig, error) {
	user, err := m.db.GetUser(email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, nil
	}

	return &UserConfig{
		Name:  user.Name,
		Roles: user.Roles,
	}, nil
}

// GetUserWithID returns a user's configuration with database ID
func (m *DBManager) GetUserWithID(email string) (*UserWithID, error) {
	user, err := m.db.GetUser(email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, nil
	}

	return &UserWithID{
		ID:    user.ID,
		Name:  user.Name,
		Roles: user.Roles,
	}, nil
}

// GetUsers returns all users from the database
func (m *DBManager) GetUsers() (map[string]*UserConfig, error) {
	users, err := m.db.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	result := make(map[string]*UserConfig)
	for _, user := range users {
		result[user.Email] = &UserConfig{
			Name:  user.Name,
			Roles: user.Roles,
		}
	}

	return result, nil
}

// SaveUser saves or updates a user in the database
func (m *DBManager) SaveUser(email string, user *UserConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if user exists
	existing, err := m.db.GetUser(email)
	if err != nil {
		return fmt.Errorf("failed to check existing user: %w", err)
	}

	if existing != nil {
		// Update existing user
		if err := m.db.UpdateUserRoles(email, user.Roles); err != nil {
			return fmt.Errorf("failed to update user: %w", err)
		}
	} else {
		// Create new user
		if _, err := m.db.CreateUser(email, user.Name, user.Roles); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
	}

	// Resync users to update Casbin policies
	if err := m.syncUsersFromDB(); err != nil {
		return fmt.Errorf("failed to sync users: %w", err)
	}

	return nil
}

// DeleteUser removes a user from the database
func (m *DBManager) DeleteUser(email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.db.DeleteUser(email); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// Resync users to update Casbin policies
	if err := m.syncUsersFromDB(); err != nil {
		return fmt.Errorf("failed to sync users: %w", err)
	}

	return nil
}

// GetUserRoles returns the roles for a user
func (m *DBManager) GetUserRoles(email string) ([]string, error) {
	user, err := m.db.GetUser(email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, nil
	}
	return user.Roles, nil
}

// GetRolePermissions returns permissions for a role
func (m *DBManager) GetRolePermissions(role string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	permissions, _ := m.enforcer.GetPermissionsForUser(role)
	var result []string
	for _, p := range permissions {
		if len(p) > 1 {
			result = append(result, p[1]) // The permission is at index 1
		}
	}
	return result, nil
}

// memoryAdapter implements persist.Adapter for in-memory policies
type memoryAdapter struct {
	policies [][]string
}

func (a *memoryAdapter) LoadPolicy(model model.Model) error {
	for _, p := range a.policies {
		if len(p) < 2 {
			continue
		}

		key := p[0]
		sec := key[:1]
		ptype := key

		if sec == "p" {
			model.AddPolicy(sec, ptype, p[1:])
		} else if sec == "g" {
			// For grouping policies (role inheritance), we still use AddPolicy
			// but the section is "g" not "p"
			model.AddPolicy(sec, ptype, p[1:])
		}
	}
	return nil
}

func (a *memoryAdapter) SavePolicy(model model.Model) error {
	return nil // We don't save policies, they're embedded
}

func (a *memoryAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	return nil
}

func (a *memoryAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	return nil
}

func (a *memoryAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	return nil
}
