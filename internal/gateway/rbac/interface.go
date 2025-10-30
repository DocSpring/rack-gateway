package rbac

import "github.com/DocSpring/rack-gateway/internal/gateway/db"

// UserWithID extends UserConfig with database ID
type UserWithID struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

// Manager defines the interface for RBAC operations.
type Manager interface {
	// Enforce checks if a user has permission to perform an action
	Enforce(userEmail string, scope Scope, resource Resource, action Action) (bool, error)

	// EnforceUser checks permissions using a preloaded user record (no additional DB query)
	EnforceUser(user *db.User, scope Scope, resource Resource, action Action) (bool, error)

	// EnforceForAPIToken checks if an API token has permission to perform an action
	EnforceForAPIToken(tokenID int64, scope Scope, resource Resource, action Action) (bool, error)

	// GetAllowedDomain returns the configured domain
	GetAllowedDomain() string

	// GetUser returns a user's configuration
	GetUser(email string) (*UserConfig, error)

	// GetUserWithID returns a user's configuration with database ID
	GetUserWithID(email string) (*UserWithID, error)

	// GetUsers returns all users
	GetUsers() (map[string]*UserConfig, error)

	// SaveUser saves or updates a user
	SaveUser(email string, user *UserConfig) error

	// DeleteUser removes a user
	DeleteUser(email string) error

	// GetUserRoles returns the roles for a user
	GetUserRoles(email string) ([]string, error)

	// GetRolePermissions returns permissions for a role
	GetRolePermissions(role string) ([]string, error)
}
