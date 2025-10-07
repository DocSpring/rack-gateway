package proxy

import (
	"testing"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

// Test a matrix of sensitive routes mapped to RBAC permissions for deployer vs admin.
func TestPermissionMatrix_DeployerVsAdmin(t *testing.T) {
	database, err := db.NewFromEnv()
	require.NoError(t, err)
	t.Cleanup(func() {
		database.Close() //nolint:errcheck // test cleanup
	})
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
		scope         rbac.Scope
		resource      rbac.Resource
		action        rbac.Action
		deployerAllow bool
		adminAllow    bool
	}

	cases := []tc{
		{"apps create denied for deployer", rbac.ScopeConvox, rbac.ResourceApp, rbac.ActionCreate, false, true},
		{"apps update allowed for deployer", rbac.ScopeConvox, rbac.ResourceApp, rbac.ActionUpdate, true, true},
		{"apps delete denied for deployer", rbac.ScopeConvox, rbac.ResourceApp, rbac.ActionDelete, false, true},
		{"apps restart allowed for deployer", rbac.ScopeConvox, rbac.ResourceApp, rbac.ActionRestart, true, true},
		{"releases promote allowed for deployer", rbac.ScopeConvox, rbac.ResourceRelease, rbac.ActionPromote, true, true},
		{"releases create allowed for deployer", rbac.ScopeConvox, rbac.ResourceRelease, rbac.ActionCreate, true, true},
		{"rack update denied for deployer", rbac.ScopeConvox, rbac.ResourceRack, rbac.ActionUpdate, false, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Deployer
			ok, err := mgr.Enforce("deployer@test.com", c.scope, c.resource, c.action)
			require.NoError(t, err)
			require.Equal(t, c.deployerAllow, ok, "deployer mismatch for %s:%s:%s", c.scope, c.resource, c.action)

			// Admin
			ok, err = mgr.Enforce("admin@test.com", c.scope, c.resource, c.action)
			require.NoError(t, err)
			require.Equal(t, c.adminAllow, ok, "admin mismatch for %s:%s:%s", c.scope, c.resource, c.action)
		})
	}
}
