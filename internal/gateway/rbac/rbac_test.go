package rbac

import (
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

// TestEnforceDeployerPermissions verifies deployer can create/update apps but cannot delete apps.
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

	// Deployer: allowed create/update, denied delete
	ok, err := mgr.Enforce("deployer@test.com", "app", "create")
	require.NoError(t, err)
	require.True(t, ok, "deployer should be allowed to create apps")

	ok, err = mgr.Enforce("deployer@test.com", "app", "update")
	require.NoError(t, err)
	require.True(t, ok, "deployer should be allowed to update apps")

	ok, err = mgr.Enforce("deployer@test.com", "app", "delete")
	require.NoError(t, err)
	require.False(t, ok, "deployer should NOT be allowed to delete apps")

	// Admin: allowed delete
	ok, err = mgr.Enforce("admin@test.com", "app", "delete")
	require.NoError(t, err)
	require.True(t, ok, "admin should be allowed to delete apps")
}
