package rbac

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

// RBACDatabase defines the database operations needed by the RBAC manager
type RBACDatabase interface {
	GetUser(email string) (*db.User, error)
	GetAPITokenByID(id int64) (*db.APIToken, error)
	HasActiveDeployApprovalForApp(tokenID int64, app string) (bool, error)
	ListUsers() ([]*db.User, error)
	CreateUser(email, name string, roles []string) (*db.User, error)
	UpdateUserRoles(email string, roles []string) error
	UpdateUserName(email, name string) error
	DeleteUser(email string) error
}

// DBManager implements RBAC using the database
type DBManager struct {
	db       RBACDatabase
	enforcer *casbin.Enforcer
	mu       sync.RWMutex
	domain   string
}

// RoleMetadata describes presentation attributes for a role exposed to the UI.
type RoleMetadata struct {
	Label       string
	Description string
}

type roleConfig struct {
	Permissions []string
	Parents     []string
}

var roleOrder = []string{"viewer", "ops", "deployer", "cicd", "admin"}

var roleMetadata = map[string]RoleMetadata{
	"viewer": {
		Label:       "Viewer",
		Description: "Read-only access to apps, builds, processes, and rack status",
	},
	"ops": {
		Label:       "Operations",
		Description: "Restart apps, manage processes, and view environments",
	},
	"deployer": {
		Label:       "Deployer",
		Description: "Full deployment permissions including env updates",
	},
	"cicd": {
		Label:       "CI/CD",
		Description: "Recommended scope for automation tokens (not assignable to human users)",
	},
	"admin": {
		Label:       "Admin",
		Description: "Complete access to all gateway operations",
	},
}

var roleConfigs = map[string]roleConfig{
	"viewer": {
		Permissions: []string{
			Convox(ResourceApp, ActionList),
			Convox(ResourceApp, ActionRead),
			Convox(ResourceProcess, ActionList),
			Convox(ResourceProcess, ActionRead),
			Convox(ResourceInstance, ActionList),
			Convox(ResourceInstance, ActionRead),
			Convox(ResourceLog, ActionRead),
			Convox(ResourceBuild, ActionList),
			Convox(ResourceBuild, ActionRead),
			Convox(ResourceRack, ActionRead),
		},
	},
	"ops": {
		Permissions: []string{
			Convox(ResourceApp, ActionRestart),
			Convox(ResourceProcess, ActionStart),
			Convox(ResourceProcess, ActionExec),
			Convox(ResourceProcess, ActionTerminate),
			Convox(ResourceRelease, ActionList),
			Convox(ResourceEnv, ActionRead),
		},
		Parents: []string{"viewer"},
	},
	"deployer": {
		Permissions: []string{
			Convox(ResourceApp, ActionRestart),
			Convox(ResourceBuild, ActionCreate),
			Convox(ResourceObject, ActionCreate),
			Convox(ResourceRelease, ActionCreate),
			Convox(ResourceRelease, ActionRead),
			Convox(ResourceRelease, ActionPromote),
			Convox(ResourceEnv, ActionRead),
			Convox(ResourceEnv, ActionSet),
			Convox(ResourceApp, ActionUpdate),
			Gateway(ResourceDeployApprovalRequest, ActionCreate),
			Gateway(ResourceDeployApprovalRequest, ActionRead),
		},
		Parents: []string{"ops"},
	},
	// CI/CD role for automated deployments
	// Uses single deploy_with_approval permission that grants all deployment actions when an active approval exists
	"cicd": {
		Permissions: []string{
			Convox(ResourceApp, ActionList),
			Convox(ResourceApp, ActionRead),
			Convox(ResourceProcess, ActionList),
			Convox(ResourceProcess, ActionRead),
			Gateway(ResourceDeployApprovalRequest, ActionCreate),
			Gateway(ResourceDeployApprovalRequest, ActionRead),
			Convox(ResourceDeploy, ActionDeployWithApproval), // Grants build, release, process ops when approval exists
			Convox(ResourceInstance, ActionList),
			Convox(ResourceInstance, ActionRead),
			Convox(ResourceLog, ActionRead),
			Convox(ResourceRack, ActionRead),
		},
	},
	"admin": {
		Permissions: []string{
			"convox:*:*",  // Wildcard - can't be represented by enums
			"gateway:*:*", // Wildcard - can't be represented by enums
		},
	},
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

var (
	policies               = buildPolicies(roleConfigs)
	defaultRolePermissions = buildDefaultRolePermissions(roleConfigs)
)

func buildPolicies(cfg map[string]roleConfig) [][]string {
	var out [][]string
	for _, role := range roleOrder {
		config, ok := cfg[role]
		if !ok {
			continue
		}
		perms := append([]string(nil), config.Permissions...)
		sort.Strings(perms)
		for _, perm := range perms {
			out = append(out, []string{"p", role, perm, "*"})
		}
		parents := append([]string(nil), config.Parents...)
		sort.Strings(parents)
		for _, parent := range parents {
			out = append(out, []string{"g", role, parent})
		}
	}
	return out
}

func buildDefaultRolePermissions(cfg map[string]roleConfig) map[string][]string {
	result := make(map[string][]string, len(cfg))
	cache := make(map[string]map[string]struct{}, len(cfg))
	var flatten func(role string) map[string]struct{}
	flatten = func(role string) map[string]struct{} {
		if set, ok := cache[role]; ok {
			return set
		}
		config, ok := cfg[role]
		if !ok {
			cache[role] = map[string]struct{}{}
			return cache[role]
		}
		set := make(map[string]struct{})
		for _, perm := range config.Permissions {
			set[perm] = struct{}{}
		}
		for _, parent := range config.Parents {
			for perm := range flatten(parent) {
				set[perm] = struct{}{}
			}
		}
		cache[role] = set
		return set
	}

	for _, role := range roleOrder {
		set := flatten(role)
		perms := make([]string, 0, len(set))
		for perm := range set {
			perms = append(perms, perm)
		}
		sort.Strings(perms)
		result[role] = perms
	}

	return result
}

// RoleOrder returns the canonical display order for roles.
func RoleOrder() []string {
	return append([]string(nil), roleOrder...)
}

// RoleMetadataMap returns a copy of the role metadata keyed by role name.
func RoleMetadataMap() map[string]RoleMetadata {
	out := make(map[string]RoleMetadata, len(roleMetadata))
	for k, v := range roleMetadata {
		out[k] = v
	}
	return out
}

// DefaultRolePermissions returns a copy of the flattened permissions per role.
func DefaultRolePermissions() map[string][]string {
	clone := make(map[string][]string, len(defaultRolePermissions))
	for role, perms := range defaultRolePermissions {
		clone[role] = append([]string(nil), perms...)
	}
	return clone
}

// DefaultPermissionsForRole returns the flattened permission list for a specific role.
func DefaultPermissionsForRole(role string) []string {
	if perms, ok := defaultRolePermissions[role]; ok {
		return append([]string(nil), perms...)
	}
	return nil
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
			if _, err := m.enforcer.AddGroupingPolicy(user.Email, role); err != nil {
				return fmt.Errorf("failed to assign role %s to %s: %w", role, user.Email, err)
			}
		}
	}

	return nil
}

// Enforce checks if a user has permission to perform an action
func (m *DBManager) Enforce(userEmail string, scope Scope, resource Resource, action Action) (bool, error) {
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

	// Build permission string from enum types
	permission := Permission(scope, resource, action)

	// Check permission using Casbin with 3 parameters (sub, obj, act)
	// The third parameter is "*" as we don't use it in our model
	ok, err := m.enforcer.Enforce(userEmail, permission, "*")
	if err != nil {
		return false, fmt.Errorf("failed to enforce: %w", err)
	}

	return ok, nil
}

// EnforceForAPIToken checks if an API token has permission to perform an action
func (m *DBManager) EnforceForAPIToken(tokenID int64, scope Scope, resource Resource, action Action) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get the API token
	token, err := m.db.GetAPITokenByID(tokenID)
	if err != nil {
		return false, fmt.Errorf("failed to get API token: %w", err)
	}
	if token == nil {
		return false, nil // Token doesn't exist
	}

	// Build permission string from enum types
	permission := Permission(scope, resource, action)

	// Check if permission is directly granted (with wildcard support)
	return matchesAnyPermission(token.Permissions, permission), nil
}

// matchesAnyPermission checks if the requested permission matches any in the list
// Supports wildcards like "convox:*:*" (all convox permissions) and "convox:app:*" (all app actions)
func matchesAnyPermission(permissions []string, requested string) bool {
	for _, perm := range permissions {
		if perm == requested {
			return true
		}
		// Check for wildcard patterns
		if strings.Contains(perm, "*") {
			// Handle multi-level wildcards like "convox:*:*"
			permParts := strings.Split(perm, ":")
			reqParts := strings.Split(requested, ":")

			// Must have same number of parts
			if len(permParts) != len(reqParts) {
				continue
			}

			// Check each part
			match := true
			for i := range permParts {
				if permParts[i] != "*" && permParts[i] != reqParts[i] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
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
		trimmedName := strings.TrimSpace(user.Name)
		if trimmedName != "" && trimmedName != existing.Name {
			if err := m.db.UpdateUserName(email, trimmedName); err != nil {
				return fmt.Errorf("failed to update user name: %w", err)
			}
		}
		if err := m.db.UpdateUserRoles(email, user.Roles); err != nil {
			return fmt.Errorf("failed to update user: %w", err)
		}
	} else {
		// Create new user
		name := strings.TrimSpace(user.Name)
		if _, err := m.db.CreateUser(email, name, user.Roles); err != nil {
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

func (a *memoryAdapter) LoadPolicy(model model.Model) error {
	for _, p := range a.policies {
		if len(p) < 2 {
			continue
		}

		key := p[0]
		sec := key[:1]
		ptype := key

		switch sec {
		case "p", "g":
			if err := model.AddPolicy(sec, ptype, p[1:]); err != nil {
				return fmt.Errorf("failed to add policy %v: %w", p, err)
			}
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
