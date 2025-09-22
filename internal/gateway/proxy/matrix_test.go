package proxy

import (
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

// Test a matrix of sensitive routes mapped to RBAC permissions for deployer vs admin.
func TestPermissionMatrix_DeployerVsAdmin(t *testing.T) {
	database, err := db.NewFromEnv()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	dbtest.Reset(t, database)

	// Users
	_, err = database.CreateUser("deployer@test.com", "Deployer", []string{"deployer"})
	require.NoError(t, err)
	_, err = database.CreateUser("admin@test.com", "Admin", []string{"admin"})
	require.NoError(t, err)

	mgr, err := rbac.NewDBManager(database, "example.com")
	require.NoError(t, err)

	type tc struct {
		name          string
		resource      string
		action        string
		deployerAllow bool
		adminAllow    bool
	}

	cases := []tc{
		{"apps create allowed for deployer", "app", "create", true, true},
		{"apps update allowed for deployer", "app", "update", true, true},
		{"apps delete denied for deployer", "app", "delete", false, true},
		{"apps restart allowed for deployer", "app", "restart", true, true},
		{"releases promote allowed for deployer", "release", "promote", true, true},
		{"releases create allowed for deployer", "release", "create", true, true},
		{"instances delete denied for deployer", "rack", "terminate", false, true},
		{"registries create denied for deployer", "registry", "create", false, true},
		{"registries delete denied for deployer", "registry", "delete", false, true},
		{"system update denied for deployer", "rack", "update", false, true},
		{"system jwt token denied for deployer", "system", "jwt_token", false, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Deployer
			ok, err := mgr.Enforce("deployer@test.com", c.resource, c.action)
			require.NoError(t, err)
			require.Equal(t, c.deployerAllow, ok, "deployer mismatch for %s:%s", c.resource, c.action)

			// Admin
			ok, err = mgr.Enforce("admin@test.com", c.resource, c.action)
			require.NoError(t, err)
			require.Equal(t, c.adminAllow, ok, "admin mismatch for %s:%s", c.resource, c.action)
		})
	}
}
