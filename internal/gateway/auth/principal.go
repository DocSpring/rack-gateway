package auth

import (
	"context"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Principal returns the RBAC principal for this authenticated user or API token.
func (u *User) Principal() rbac.Principal {
	if u.IsAPIToken {
		return rbac.TokenPrincipal(u.DBUser, u.Permissions)
	}
	return rbac.UserPrincipal(u.DBUser)
}

// Authorize reports whether the authenticated caller on ctx holds the permission.
// Requests without an authenticated caller are denied.
func Authorize(ctx context.Context, manager rbac.Manager, permission string) (bool, error) {
	user, ok := GetAuthUser(ctx)
	if !ok || user == nil {
		return false, nil
	}
	return manager.Authorize(user.Principal(), permission)
}
