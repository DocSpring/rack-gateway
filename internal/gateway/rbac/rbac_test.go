package rbac

import (
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
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
	ok, err := mgr.Enforce("deployer@test.com", "app", "create")
	require.NoError(t, err)
	require.False(t, ok, "deployer should NOT be allowed to create apps")

	ok, err = mgr.Enforce("deployer@test.com", "app", "update")
	require.NoError(t, err)
	require.True(t, ok, "deployer should be allowed to update apps")

	ok, err = mgr.Enforce("deployer@test.com", "app", "delete")
	require.NoError(t, err)
	require.False(t, ok, "deployer should NOT be allowed to delete apps")

	// Deploy approval permissions
	ok, err = mgr.Enforce("deployer@test.com", "gateway:deploy-request", "create")
	require.NoError(t, err)
	require.True(t, ok, "deployer should be allowed to request deploy approval")

	ok, err = mgr.Enforce("deployer@test.com", "gateway:deploy-request", "approve")
	require.NoError(t, err)
	require.False(t, ok, "deployer should NOT be allowed to approve deploy requests")

	// Admin: allowed delete
	ok, err = mgr.Enforce("admin@test.com", "app", "delete")
	require.NoError(t, err)
	require.True(t, ok, "admin should be allowed to delete apps")

	ok, err = mgr.Enforce("admin@test.com", "gateway:deploy-request", "approve")
	require.NoError(t, err)
	require.True(t, ok, "admin should be allowed to approve deploy requests")
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
