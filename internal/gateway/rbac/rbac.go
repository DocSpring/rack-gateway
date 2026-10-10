package rbac

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// Database defines the database operations needed by the RBAC manager.
type Database interface {
	GetUser(email string) (*db.User, error)
	ListUsers() ([]*db.User, error)
	CreateUser(email, name string, roles []string) (*db.User, error)
	UpdateUserRoles(email string, roles []string) error
	UpdateUserName(email, name string) error
	DeleteUser(email string) error
}

// DBManager implements RBAC using the database
type DBManager struct {
	db       Database
	enforcer *casbin.Enforcer
	mu       sync.RWMutex
	domain   string
}

//nolint:lll
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

	return manager, nil
}

// Authorize reports whether the principal holds the permission.
//
// Human users are checked against the roles on their current database record, so role changes take
// effect on the next request. API tokens must hold the permission themselves AND their owner's
// current roles must allow it. Suspended or locked users (and tokens they own) are always denied.
func (m *DBManager) Authorize(p Principal, permission string) (bool, error) {
	user := p.User
	if user == nil || user.Suspended || user.LockedAt != nil {
		return false, nil
	}
	if p.APIToken && !matchesAnyPermission(p.TokenPermissions, permission) {
		return false, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, role := range user.Roles {
		ok, err := m.enforcer.Enforce(role, permission, "*")
		if err != nil {
			return false, fmt.Errorf("failed to enforce: %w", err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// matchesAnyPermission checks if the requested permission matches any in the list
// Supports wildcards like "convox:*:*" (all convox permissions) and "convox:app:*" (all app actions)
func matchesAnyPermission(permissions []string, requested string) bool {
	for _, perm := range permissions {
		if perm == requested {
			return true
		}
		if matchesWildcard(perm, requested) {
			return true
		}
	}
	return false
}

func matchesWildcard(perm, requested string) bool {
	if !strings.Contains(perm, "*") {
		return false
	}

	permParts := strings.Split(perm, ":")
	reqParts := strings.Split(requested, ":")

	if len(permParts) != len(reqParts) {
		return false
	}

	for i := range permParts {
		if permParts[i] != "*" && permParts[i] != reqParts[i] {
			return false
		}
	}
	return true
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
	existing, err := m.db.GetUser(email)
	if err != nil {
		return fmt.Errorf("failed to check existing user: %w", err)
	}
	if existing != nil {
		return m.updateExistingUser(email, user, existing)
	}
	return m.createNewUser(email, user)
}

func (m *DBManager) updateExistingUser(email string, user *UserConfig, existing *db.User) error {
	trimmedName := strings.TrimSpace(user.Name)
	if trimmedName != "" && trimmedName != existing.Name {
		if err := m.db.UpdateUserName(email, trimmedName); err != nil {
			return fmt.Errorf("failed to update user name: %w", err)
		}
	}
	if err := m.db.UpdateUserRoles(email, user.Roles); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

func (m *DBManager) createNewUser(email string, user *UserConfig) error {
	name := strings.TrimSpace(user.Name)
	if _, err := m.db.CreateUser(email, name, user.Roles); err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// DeleteUser removes a user from the database
func (m *DBManager) DeleteUser(email string) error {
	if err := m.db.DeleteUser(email); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
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

	permissions, err := m.enforcer.GetImplicitPermissionsForUser(role)
	if err != nil {
		return nil, fmt.Errorf("failed to get implicit permissions: %w", err)
	}
	set := make(map[string]struct{}, len(permissions))
	for _, p := range permissions {
		if len(p) > 1 {
			set[p[1]] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for perm := range set {
		result = append(result, perm)
	}
	sort.Strings(result)
	return result, nil
}

// memoryAdapter implements persist.Adapter for in-memory policies
type memoryAdapter struct {
	policies [][]string
}

func (a *memoryAdapter) LoadPolicy(mdl model.Model) error {
	for _, p := range a.policies {
		if len(p) < 2 {
			continue
		}

		key := p[0]
		sec := key[:1]
		ptype := key

		switch sec {
		case "p", "g":
			if err := mdl.AddPolicy(sec, ptype, p[1:]); err != nil {
				return fmt.Errorf("failed to add policy %v: %w", p, err)
			}
		}
	}
	return nil
}

func (_ *memoryAdapter) SavePolicy(model.Model) error {
	return nil // We don't save policies, they're embedded
}

func (_ *memoryAdapter) AddPolicy(string, string, []string) error {
	return nil
}

func (_ *memoryAdapter) RemovePolicy(string, string, []string) error {
	return nil
}

func (_ *memoryAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return nil
}
