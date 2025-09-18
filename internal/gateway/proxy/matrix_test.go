package proxy

import (
	"testing"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/rbac"
	"github.com/DocSpring/convox-gateway/internal/testutil/dbtest"
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

	h := &Handler{}

	type tc struct {
		name          string
		method        string
		path          string
		deployerAllow bool
		adminAllow    bool
	}

	cases := []tc{
		{"apps create allowed for deployer", "POST", "/apps", true, true},
		{"apps update allowed for deployer", "PUT", "/apps/myapp", true, true},
		{"apps delete denied for deployer", "DELETE", "/apps/myapp", false, true},

		// Releases promote should be allowed for deployer
		{"releases promote allowed for deployer", "POST", "/apps/myapp/releases/r123/promote", true, true},
		{"releases create allowed for deployer", "POST", "/apps/myapp/releases", true, true},

		// Sensitive admin-only endpoints
		{"instances delete denied for deployer", "DELETE", "/instances/i-123", false, true},
		{"registries post denied for deployer", "POST", "/registries", false, true},
		{"registries delete denied for deployer", "DELETE", "/registries/docker.io", false, true},
		{"system put denied for deployer", "PUT", "/system", false, true},
		{"system jwt token denied for deployer", "POST", "/system/jwt/token", false, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, act := h.pathToResourceAction(c.path, c.method)

			// Deployer
			ok, err := mgr.Enforce("deployer@test.com", res, act)
			require.NoError(t, err)
			require.Equal(t, c.deployerAllow, ok, "deployer mismatch for %s %s -> %s:%s", c.method, c.path, res, act)

			// Admin
			ok, err = mgr.Enforce("admin@test.com", res, act)
			require.NoError(t, err)
			require.Equal(t, c.adminAllow, ok, "admin mismatch for %s %s -> %s:%s", c.method, c.path, res, act)
		})
	}
}
