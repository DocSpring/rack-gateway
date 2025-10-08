package rbac

import (
	"sync"
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

// TestEnforceDeployerPermissions verifies deployer can update but not create/delete apps.
func TestEnforceDeployerPermissions(t *testing.T) {
	database := dbtest.NewDatabase(t)

	// Create users
	_, err := database.CreateUser("deployer@test.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)
	_, err = database.CreateUser("admin@test.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	// RBAC manager (DB-backed)
	mgr, err := NewDBManager(database, "example.com")
	require.NoError(t, err)

	// Deployer: denied create, allowed update, denied delete
	ok, err := mgr.Enforce("deployer@test.com", ScopeConvox, ResourceApp, ActionCreate)
	require.NoError(t, err)
	require.False(t, ok, "deployer should NOT be allowed to create apps")

	ok, err = mgr.Enforce("deployer@test.com", ScopeConvox, ResourceApp, ActionUpdate)
	require.NoError(t, err)
	require.True(t, ok, "deployer should be allowed to update apps")

	ok, err = mgr.Enforce("deployer@test.com", ScopeConvox, ResourceApp, ActionDelete)
	require.NoError(t, err)
	require.False(t, ok, "deployer should NOT be allowed to delete apps")

	// Deploy approval permissions
	ok, err = mgr.Enforce("deployer@test.com", ScopeGateway, ResourceDeployApprovalRequest, ActionCreate)
	require.NoError(t, err)
	require.True(t, ok, "deployer should be allowed to request deploy approval")

	ok, err = mgr.Enforce("deployer@test.com", ScopeGateway, ResourceDeployApprovalRequest, ActionApprove)
	require.NoError(t, err)
	require.False(t, ok, "deployer should NOT be allowed to approve deploy approval requests")

	// Admin: allowed delete
	ok, err = mgr.Enforce("admin@test.com", ScopeConvox, ResourceApp, ActionDelete)
	require.NoError(t, err)
	require.True(t, ok, "admin should be allowed to delete apps")

	ok, err = mgr.Enforce("admin@test.com", ScopeGateway, ResourceDeployApprovalRequest, ActionApprove)
	require.NoError(t, err)
	require.True(t, ok, "admin should be allowed to approve deploy approval requests")
}

func TestSaveUserUpdatesDisplayName(t *testing.T) {
	database := dbtest.NewDatabase(t)
	_, err := database.CreateUser("user@example.com", "Old Name", []string{"viewer"})
	require.NoError(t, err)

	mgr, err := NewDBManager(database, "example.com")
	require.NoError(t, err)

	err = mgr.SaveUser("user@example.com", &UserConfig{Name: "New Name", Roles: []string{"viewer"}})
	require.NoError(t, err)

	updated, err := database.GetUser("user@example.com")
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, "New Name", updated.Name)

	err = mgr.SaveUser("user@example.com", &UserConfig{Name: "   ", Roles: []string{"viewer"}})
	require.NoError(t, err)

	unchanged, err := database.GetUser("user@example.com")
	require.NoError(t, err)
	require.NotNil(t, unchanged)
	require.Equal(t, "New Name", unchanged.Name)
}

func TestDeployWithApprovalPermission(t *testing.T) {
	t.Run("without approval object:create is denied", func(t *testing.T) {
		mockDB := &mockDatabase{
			apiToken: &db.APIToken{
				ID:          1,
				Permissions: []string{"convox:deploy:deploy_with_approval"},
			},
			hasActiveApproval: false,
		}

		mgr := &DBManager{
			db: mockDB,
			mu: sync.RWMutex{},
		}

		ok, err := mgr.EnforceForAPIToken(1, ScopeConvox, ResourceObject, ActionCreate)
		require.NoError(t, err)
		require.False(t, ok, "should NOT be allowed to create objects without approval")
	})

	t.Run("with active approval object:create is granted", func(t *testing.T) {
		mockDB := &mockDatabase{
			apiToken: &db.APIToken{
				ID:          1,
				Permissions: []string{"convox:deploy:deploy_with_approval"},
			},
			hasActiveApproval: true,
		}

		mgr := &DBManager{
			db: mockDB,
			mu: sync.RWMutex{},
		}

		ok, err := mgr.EnforceForAPIToken(1, ScopeConvox, ResourceObject, ActionCreate)
		require.NoError(t, err)
		require.True(t, ok, "should be allowed to create objects with active approval")
	})

	t.Run("without deploy_with_approval permission is denied", func(t *testing.T) {
		mockDB := &mockDatabase{
			apiToken: &db.APIToken{
				ID:          1,
				Permissions: []string{"convox:app:list"},
			},
			hasActiveApproval: true, // Even with approval, no deploy_with_approval permission
		}

		mgr := &DBManager{
			db: mockDB,
			mu: sync.RWMutex{},
		}

		ok, err := mgr.EnforceForAPIToken(1, ScopeConvox, ResourceObject, ActionCreate)
		require.NoError(t, err)
		require.False(t, ok, "should NOT be allowed without deploy_with_approval permission")
	})
}

// mockDatabase implements RBACDatabase interface for testing
type mockDatabase struct {
	apiToken          *db.APIToken
	hasActiveApproval bool
	user              *db.User
	users             []*db.User
}

func (m *mockDatabase) GetAPITokenByID(id int64) (*db.APIToken, error) {
	return m.apiToken, nil
}

func (m *mockDatabase) HasActiveDeployApproval(tokenID int64) (bool, error) {
	return m.hasActiveApproval, nil
}

func (m *mockDatabase) GetUser(email string) (*db.User, error) {
	return m.user, nil
}

func (m *mockDatabase) ListUsers() ([]*db.User, error) {
	return m.users, nil
}

func (m *mockDatabase) CreateUser(email, name string, roles []string) (*db.User, error) {
	return nil, nil
}

func (m *mockDatabase) UpdateUserRoles(email string, roles []string) error {
	return nil
}

func (m *mockDatabase) UpdateUserName(email, name string) error {
	return nil
}

func (m *mockDatabase) DeleteUser(email string) error {
	return nil
}
