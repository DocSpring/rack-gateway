package rbac

import "github.com/DocSpring/rack-gateway/internal/gateway/db"

// Principal is the caller being authorized.
//
// Human users are authorized by the roles currently stored on their database record. API tokens are
// authorized by their own permission list, capped by the owner's current roles, so a token can never
// do more than the person who owns it.
type Principal struct {
	// User is the human making the request, or the owner of the API token.
	User *db.User
	// APIToken is true when the request is authenticated with an API token.
	APIToken bool
	// TokenPermissions is the API token's own permission list.
	TokenPermissions []string
}

// UserPrincipal returns the principal for a human user.
func UserPrincipal(user *db.User) Principal {
	return Principal{User: user}
}

// TokenPrincipal returns the principal for an API token owned by owner.
func TokenPrincipal(owner *db.User, permissions []string) Principal {
	return Principal{User: owner, APIToken: true, TokenPermissions: permissions}
}
