package handlers

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

type allowAllRBAC struct {
	users map[string]*rbac.UserWithID
}

func newAllowAllRBAC(users ...*db.User) *allowAllRBAC {
	m := make(map[string]*rbac.UserWithID)
	for _, u := range users {
		if u == nil {
			continue
		}
		rolesCopy := append([]string(nil), u.Roles...)
		m[u.Email] = &rbac.UserWithID{ID: u.ID, Name: u.Name, Roles: rolesCopy}
	}
	return &allowAllRBAC{users: m}
}

func (_ *allowAllRBAC) Enforce(
	_ string,
	_ rbac.Scope,
	_ rbac.Resource,
	_ rbac.Action,
) (bool, error) {
	return true, nil
}

func (_ *allowAllRBAC) EnforceUser(
	_ *db.User,
	_ rbac.Scope,
	_ rbac.Resource,
	_ rbac.Action,
) (bool, error) {
	return true, nil
}

func (_ *allowAllRBAC) EnforceForAPIToken(
	_ int64,
	_ rbac.Scope,
	_ rbac.Resource,
	_ rbac.Action,
) (bool, error) {
	return true, nil
}

func (_ *allowAllRBAC) GetAllowedDomain() string {
	return "example.com"
}

func (a *allowAllRBAC) GetUser(email string) (*rbac.UserConfig, error) {
	user, ok := a.users[email]
	if !ok {
		return nil, nil
	}
	rolesCopy := append([]string(nil), user.Roles...)
	return &rbac.UserConfig{Name: user.Name, Roles: rolesCopy}, nil
}

func (a *allowAllRBAC) GetUserWithID(email string) (*rbac.UserWithID, error) {
	user, ok := a.users[email]
	if !ok {
		return nil, nil
	}
	clone := *user
	clone.Roles = append([]string(nil), user.Roles...)
	return &clone, nil
}

func (a *allowAllRBAC) GetUsers() (map[string]*rbac.UserConfig, error) {
	result := make(map[string]*rbac.UserConfig, len(a.users))
	for email, user := range a.users {
		rolesCopy := append([]string(nil), user.Roles...)
		result[email] = &rbac.UserConfig{Name: user.Name, Roles: rolesCopy}
	}
	return result, nil
}

func (a *allowAllRBAC) SaveUser(email string, user *rbac.UserConfig) error {
	if user == nil {
		delete(a.users, email)
		return nil
	}
	rolesCopy := append([]string(nil), user.Roles...)
	a.users[email] = &rbac.UserWithID{Name: user.Name, Roles: rolesCopy}
	return nil
}

func (a *allowAllRBAC) DeleteUser(email string) error {
	delete(a.users, email)
	return nil
}

func (a *allowAllRBAC) GetUserRoles(email string) ([]string, error) {
	user, ok := a.users[email]
	if !ok {
		return nil, nil
	}
	return append([]string(nil), user.Roles...), nil
}

func (_ *allowAllRBAC) GetRolePermissions(_ string) ([]string, error) {
	return []string{}, nil
}
