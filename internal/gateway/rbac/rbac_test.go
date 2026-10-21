package rbac

import (
	"testing"
	"time"

	"github.com/casbin/casbin/v3/model"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestModelMatcherIsOneExpression(t *testing.T) {
	m, err := model.NewModelFromString(modelConf)
	require.NoError(t, err)
	require.Equal(t,
		`g(r_sub, p_sub) && (p_obj == "convox:*:*" || p_obj == r_obj || keyMatch3(r_obj, p_obj)) && `+
			`(p_act == "*" || r_act == p_act)`,
		m["m"]["m"].Value)
}

func newTestManager(t *testing.T) (*DBManager, *db.Database) {
	t.Helper()
	database := dbtest.NewDatabase(t)
	mgr, err := NewDBManager(database, "example.com")
	require.NoError(t, err)
	return mgr, database
}

func createUser(t *testing.T, database *db.Database, email string, roles ...string) *db.User {
	t.Helper()
	user, err := database.CreateUser(email, email, roles)
	require.NoError(t, err)
	return user
}

func userCan(t *testing.T, mgr *DBManager, database *db.Database, email, permission string) bool {
	t.Helper()
	user, err := database.GetUser(email)
	require.NoError(t, err)
	ok, err := mgr.Authorize(UserPrincipal(user), permission)
	require.NoError(t, err)
	return ok
}

// TestAuthorizeRolePermissions verifies role permissions, inheritance and admin wildcards.
func TestAuthorizeRolePermissions(t *testing.T) {
	mgr, database := newTestManager(t)
	createUser(t, database, "deployer@test.com", "deployer")
	createUser(t, database, "viewer@test.com", "viewer")
	createUser(t, database, "admin@test.com", "admin")

	cases := []struct {
		email      string
		permission string
		want       bool
	}{
		{"deployer@test.com", Convox(ResourceApp, ActionCreate), false},
		{"deployer@test.com", Convox(ResourceApp, ActionUpdate), true},
		{"deployer@test.com", Convox(ResourceApp, ActionDelete), false},
		{"deployer@test.com", Convox(ResourceProcess, ActionExec), true}, // inherited from ops
		{"deployer@test.com", Convox(ResourceApp, ActionList), true},     // inherited from viewer
		{"deployer@test.com", Gateway(ResourceDeployApprovalRequest, ActionCreate), true},
		{"deployer@test.com", Gateway(ResourceDeployApprovalRequest, ActionApprove), false},
		{"deployer@test.com", Gateway(ResourceUser, ActionCreate), false},
		{"viewer@test.com", Gateway(ResourceAPIToken, ActionCreate), false},
		{"viewer@test.com", Gateway(ResourceAuditLog, ActionRead), false},
		{"viewer@test.com", Convox(ResourceEnv, ActionRead), false},
		{"viewer@test.com", Gateway(ResourceUser, ActionList), true},
		{"viewer@test.com", Gateway(ResourceUser, ActionRead), false},
		{"viewer@test.com", Gateway(ResourceAPIToken, ActionRead), true},
		{"deployer@test.com", Gateway(ResourceAPIToken, ActionCreate), true},
		{"deployer@test.com", Gateway(ResourceAPIToken, ActionDelete), true},
		{"deployer@test.com", Gateway(ResourceAPIToken, ActionManage), false},
		{"deployer@test.com", Convox(ResourceDeploy, ActionDeployWithApproval), true},
		{"deployer@test.com", Convox(ResourceEnv, ActionSetProtected), false},
		{"admin@test.com", Convox(ResourceEnv, ActionSetProtected), true},
		{"admin@test.com", Gateway(ResourceAPIToken, ActionManage), true},
		{"admin@test.com", Convox(ResourceApp, ActionDelete), true},
		{"admin@test.com", Gateway(ResourceDeployApprovalRequest, ActionApprove), true},
		{"admin@test.com", Gateway(ResourceAuditLog, ActionRead), true},
		{"admin@test.com", "gateway:setting_group:mfa_configuration", true},
		{"admin@test.com", Security(ResourceSecret, ActionUpdate), true},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, userCan(t, mgr, database, tc.email, tc.permission), "%s %s", tc.email, tc.permission)
	}
}

// TestAuthorizeUsesCurrentRoles verifies role changes and deletion take effect without a restart.
func TestAuthorizeUsesCurrentRoles(t *testing.T) {
	mgr, database := newTestManager(t)
	user := createUser(t, database, "user@test.com", "admin")
	deleteApps := Convox(ResourceApp, ActionDelete)
	require.True(t, userCan(t, mgr, database, "user@test.com", deleteApps))

	_, err := database.UpdateUser(user.ID, db.UserUpdate{Roles: []string{"viewer"}}, nil)
	require.NoError(t, err)
	require.False(t, userCan(t, mgr, database, "user@test.com", deleteApps), "demotion must apply immediately")

	_, err = database.UpdateUser(user.ID, db.UserUpdate{Roles: []string{"admin"}}, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.DeleteUser("user@test.com"))
	createUser(t, database, "user@test.com", "viewer")
	require.False(t, userCan(t, mgr, database, "user@test.com", deleteApps), "re-created user must not keep old roles")
}

// TestAuthorizeDeniesLockedUsers verifies locked users, and their tokens, get nothing.
func TestAuthorizeDeniesLockedUsers(t *testing.T) {
	mgr, _ := newTestManager(t)
	now := time.Now()
	locked := &db.User{Email: "locked@test.com", Roles: []string{"admin"}, LockedAt: &now}

	for _, p := range []Principal{
		UserPrincipal(locked),
		TokenPrincipal(locked, []string{"convox:*:*"}),
		UserPrincipal(nil),
	} {
		ok, err := mgr.Authorize(p, Convox(ResourceApp, ActionList))
		require.NoError(t, err)
		require.False(t, ok)
	}
}

// TestAuthorizeAPITokens verifies tokens are limited to their own permissions AND their owner's roles.
func TestAuthorizeAPITokens(t *testing.T) {
	mgr, _ := newTestManager(t)
	admin := &db.User{Email: "admin@test.com", Roles: []string{"admin"}}
	viewer := &db.User{Email: "viewer@test.com", Roles: []string{"viewer"}}

	cases := []struct {
		name       string
		owner      *db.User
		tokenPerms []string
		permission string
		want       bool
	}{
		{"token permission granted", admin, []string{"convox:app:list"}, Convox(ResourceApp, ActionList), true},
		{
			"admin-owned token limited to its own permissions", admin,
			[]string{"convox:app:list"},
			Convox(ResourceEnv, ActionRead), false,
		},
		{
			"admin-owned token cannot approve deploys", admin, defaultRolePermissions["cicd"],
			Gateway(ResourceDeployApprovalRequest, ActionApprove), false,
		},
		{"wildcard token permission", admin, []string{"convox:*:*"}, Convox(ResourceApp, ActionDelete), true},
		{"token capped by owner role", viewer, []string{"convox:*:*"}, Convox(ResourceApp, ActionDelete), false},
		{
			"token with gateway wildcard capped by owner role", viewer,
			[]string{"gateway:*:*"},
			Gateway(ResourceUser, ActionCreate), false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := mgr.Authorize(TokenPrincipal(tc.owner, tc.tokenPerms), tc.permission)
			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
		})
	}
}

// Creating a user never changes an existing one: the request might lower the roles of an admin, even the caller's
// own, past the update path's checks and audit action.
func TestCreateUserRefusesAnExistingEmail(t *testing.T) {
	mgr, database := newTestManager(t)
	createUser(t, database, "user@example.com", "admin")

	err := mgr.CreateUser("user@example.com", &UserConfig{Name: "New Name", Roles: []string{"viewer"}})
	require.ErrorIs(t, err, db.ErrUserExists)

	unchanged, err := database.GetUser("user@example.com")
	require.NoError(t, err)
	require.Equal(t, []string{"admin"}, unchanged.Roles)
	require.Equal(t, "user@example.com", unchanged.Name)
}
