package rbac

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRBACManager(t *testing.T) {
	manager := setupTestManager(t)

	t.Run("policies loaded from compiled defaults", func(t *testing.T) {
		// Check that all default policies are loaded
		assert.NotNil(t, manager.compiledPolicies["viewer"])
		assert.NotNil(t, manager.compiledPolicies["ops"])
		assert.NotNil(t, manager.compiledPolicies["deployer"])
		assert.NotNil(t, manager.compiledPolicies["admin"])
	})

	t.Run("policy inheritance resolved", func(t *testing.T) {
		// ops should have viewer routes plus its own
		opsPolicy := manager.compiledPolicies["ops"]
		
		// Check for a viewer route
		hasViewerRoute := false
		for _, route := range opsPolicy.Routes {
			if route.Method == MethodGet && route.Path == "/apps" {
				hasViewerRoute = true
				break
			}
		}
		assert.True(t, hasViewerRoute, "ops should inherit viewer routes")

		// Check for an ops-specific route
		hasOpsRoute := false
		for _, route := range opsPolicy.Routes {
			if route.Method == MethodDelete && route.Path == "/apps/{app}/processes/{pid}" {
				hasOpsRoute = true
				break
			}
		}
		assert.True(t, hasOpsRoute, "ops should have its own routes")
	})
}

func TestViewerPermissions(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add a viewer user
	err := manager.AddUser("viewer@test.com", "Viewer User", []string{"viewer"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		method   string
		path     string
		allowed  bool
	}{
		// Should be allowed (read-only operations)
		{"can list apps", MethodGet, "/apps", true},
		{"can view app", MethodGet, "/apps/myapp", true},
		{"can view processes", MethodGet, "/apps/myapp/processes", true},
		{"can view system info", MethodGet, "/system", true},
		{"can view releases", MethodGet, "/apps/myapp/releases", true},
		{"can view builds", MethodGet, "/apps/myapp/builds", true},
		{"can view configs", MethodGet, "/apps/myapp/configs", true},
		{"can view certificates", MethodGet, "/certificates", true},
		{"can check resource options", MethodOptions, "/resources", true},

		// Should be denied (write operations)
		{"cannot create app", MethodPost, "/apps", false},
		{"cannot delete app", MethodDelete, "/apps/myapp", false},
		{"cannot stop process", MethodDelete, "/apps/myapp/processes/p1", false},
		{"cannot restart service", MethodPost, "/apps/myapp/services/web/restart", false},
		{"cannot update config", MethodPut, "/apps/myapp/configs/database", false},
		{"cannot create build", MethodPost, "/apps/myapp/builds", false},
		{"cannot promote release", MethodPost, "/apps/myapp/releases/r123/promote", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission("viewer@test.com", tt.path, tt.method)
			assert.Equal(t, tt.allowed, result, "Permission check failed for %s %s", tt.method, tt.path)
		})
	}
}

func TestOpsPermissions(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add an ops user
	err := manager.AddUser("ops@test.com", "Ops User", []string{"ops"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		method   string
		path     string
		allowed  bool
	}{
		// Should inherit viewer permissions
		{"can list apps", MethodGet, "/apps", true},
		{"can view processes", MethodGet, "/apps/myapp/processes", true},

		// Ops-specific permissions (should be allowed)
		{"can stop process", MethodDelete, "/apps/myapp/processes/p1", true},
		{"can restart service", MethodPost, "/apps/myapp/services/web/restart", true},
		{"can run process", MethodPost, "/apps/myapp/services/web/processes", true},
		{"can access files", MethodGet, "/apps/myapp/processes/p1/files", true},
		{"can upload files", MethodPost, "/apps/myapp/processes/p1/files", true},
		{"can check object existence", MethodHead, "/apps/myapp/objects/file.txt", true},

		// Should still be denied (deployer/admin only)
		{"cannot create app", MethodPost, "/apps", false},
		{"cannot delete app", MethodDelete, "/apps/myapp", false},
		{"cannot create build", MethodPost, "/apps/myapp/builds", false},
		{"cannot update config", MethodPut, "/apps/myapp/configs/database", false},
		{"cannot manage certificates", MethodPost, "/certificates", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission("ops@test.com", tt.path, tt.method)
			assert.Equal(t, tt.allowed, result, "Permission check failed for %s %s", tt.method, tt.path)
		})
	}
}

func TestDeployerPermissions(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add a deployer user
	err := manager.AddUser("deployer@test.com", "Deployer User", []string{"deployer"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		method   string
		path     string
		allowed  bool
	}{
		// Should inherit viewer + ops permissions
		{"can list apps", MethodGet, "/apps", true},
		{"can stop process", MethodDelete, "/apps/myapp/processes/p1", true},

		// Deployer-specific permissions (should be allowed)
		{"can create app", MethodPost, "/apps", true},
		{"can update app", MethodPut, "/apps/myapp", true},
		{"can create build", MethodPost, "/apps/myapp/builds", true},
		{"can promote release", MethodPost, "/apps/myapp/releases/r123/promote", true},
		{"can update config", MethodPut, "/apps/myapp/configs/database", true},
		{"can manage certificates", MethodPost, "/certificates", true},
		{"can create resources", MethodPost, "/resources", true},
		{"can link resources", MethodPost, "/resources/db/links", true},
		{"can write objects", MethodPost, "/apps/myapp/objects/file.txt", true},
		{"can delete objects", MethodDelete, "/apps/myapp/objects/file.txt", true},
		{"can post events", MethodPost, "/events", true},

		// Should still be denied (admin only)
		{"cannot delete app", MethodDelete, "/apps/myapp", false},
		{"cannot delete certificate", MethodDelete, "/certificates/cert123", false},
		{"cannot terminate instance", MethodDelete, "/instances/i-123", false},
		{"cannot update system", MethodPut, "/system", false},
		{"cannot rotate JWT", MethodPut, "/system/jwt/rotate", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission("deployer@test.com", tt.path, tt.method)
			assert.Equal(t, tt.allowed, result, "Permission check failed for %s %s", tt.method, tt.path)
		})
	}
}

func TestAdminPermissions(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add an admin user
	err := manager.AddUser("admin@test.com", "Admin User", []string{"admin"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		method   string
		path     string
	}{
		// Admin should have access to everything
		{"can do anything with apps", MethodDelete, "/apps/myapp"},
		{"can delete certificates", MethodDelete, "/certificates/cert123"},
		{"can terminate instances", MethodDelete, "/instances/i-123"},
		{"can keyroll instances", MethodPost, "/instances/keyroll"},
		{"can manage registries", MethodPost, "/registries"},
		{"can delete registries", MethodDelete, "/registries/docker.io"},
		{"can update system", MethodPut, "/system"},
		{"can rotate JWT", MethodPut, "/system/jwt/rotate"},
		{"can create JWT tokens", MethodPost, "/system/jwt/token"},
		{"can proxy any service", MethodAny, "/custom/http/proxy/anything"},
		{"can access any registry path", MethodAny, "/v2/anything/else"},
		
		// Wildcard should match everything
		{"can GET anything", MethodGet, "/some/random/path"},
		{"can POST anything", MethodPost, "/another/random/path"},
		{"can DELETE anything", MethodDelete, "/yet/another/path"},
		{"can use any method", "CUSTOM", "/any/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission("admin@test.com", tt.path, tt.method)
			assert.True(t, result, "Admin should have permission for %s %s", tt.method, tt.path)
		})
	}
}

func TestWebSocketPermissions(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add users with different roles
	require.NoError(t, manager.AddUser("viewer@test.com", "Viewer", []string{"viewer"}))
	require.NoError(t, manager.AddUser("ops@test.com", "Ops", []string{"ops"}))

	tests := []struct {
		name     string
		user     string
		path     string
		allowed  bool
	}{
		// Viewer can access log websockets
		{"viewer can access app logs", "viewer@test.com", "/apps/myapp/logs", true},
		{"viewer can access build logs", "viewer@test.com", "/apps/myapp/builds/b123/logs", true},
		{"viewer can access process logs", "viewer@test.com", "/apps/myapp/processes/p123/logs", true},
		{"viewer can access system logs", "viewer@test.com", "/system/logs", true},

		// Viewer cannot access exec/console websockets
		{"viewer cannot exec into process", "viewer@test.com", "/apps/myapp/processes/p123/exec", false},
		{"viewer cannot access resource console", "viewer@test.com", "/apps/myapp/resources/db/console", false},
		{"viewer cannot access instance shell", "viewer@test.com", "/instances/i-123/shell", false},

		// Ops can access exec/console websockets
		{"ops can exec into process", "ops@test.com", "/apps/myapp/processes/p123/exec", true},
		{"ops can access resource console", "ops@test.com", "/apps/myapp/resources/db/console", true},
		{"ops can access instance shell", "ops@test.com", "/instances/i-123/shell", true},
		{"ops can use proxy websocket", "ops@test.com", "/proxy/localhost/8080", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// WebSocket connections are treated as GET for RBAC
			result := manager.CheckPermission(tt.user, tt.path, MethodSocket)
			assert.Equal(t, tt.allowed, result, "WebSocket permission check failed for %s accessing %s", tt.user, tt.path)
		})
	}
}

func TestPathPatternMatching(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add a viewer user and ops user
	err := manager.AddUser("viewer@test.com", "Viewer User", []string{"viewer"})
	require.NoError(t, err)
	err = manager.AddUser("ops@test.com", "Ops User", []string{"ops"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		user     string
		path     string
		method   string
		allowed  bool
	}{
		// Test path parameter matching (viewer)
		{"matches app name parameter", "viewer@test.com", "/apps/production", MethodGet, true},
		{"matches app with special chars", "viewer@test.com", "/apps/my-app-123", MethodGet, true},
		{"matches nested parameters", "viewer@test.com", "/apps/myapp/processes/process-123", MethodGet, true},
		{"matches release ID", "viewer@test.com", "/apps/myapp/releases/RAPI123456", MethodGet, true},
		
		// Test multi-segment wildcards (ops has object access, not viewer)
		{"ops matches object key wildcard", "ops@test.com", "/apps/myapp/objects/path/to/file.txt", MethodGet, true},
		{"ops matches deep object path", "ops@test.com", "/apps/myapp/objects/very/deep/path/to/file.txt", MethodGet, true},
		{"viewer cannot access objects", "viewer@test.com", "/apps/myapp/objects/path/to/file.txt", MethodGet, false},
		
		// Should not match different paths
		{"doesn't match wrong prefix", "viewer@test.com", "/wrongprefix/apps", MethodGet, false},
		{"doesn't match partial path", "viewer@test.com", "/apps", MethodPost, false}, // viewer can GET but not POST
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission(tt.user, tt.path, tt.method)
			assert.Equal(t, tt.allowed, result, "Path pattern matching failed for %s %s %s", tt.user, tt.method, tt.path)
		})
	}
}

func TestExactPathMatching(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add users with different roles
	require.NoError(t, manager.AddUser("viewer@test.com", "Viewer", []string{"viewer"}))
	require.NoError(t, manager.AddUser("ops@test.com", "Ops", []string{"ops"}))

	tests := []struct {
		name     string
		user     string
		path     string
		method   string
		allowed  bool
	}{
		// CRITICAL: /apps/{app}/processes/{pid} should NOT match /apps/{app}/processes/{pid}/exec
		{"viewer can view process", "viewer@test.com", "/apps/myapp/processes/p1", MethodGet, true},
		{"viewer CANNOT exec into process", "viewer@test.com", "/apps/myapp/processes/p1/exec", MethodGet, false},
		{"viewer CANNOT exec with SOCKET", "viewer@test.com", "/apps/myapp/processes/p1/exec", MethodSocket, false},
		
		// But ops can exec
		{"ops can view process", "ops@test.com", "/apps/myapp/processes/p1", MethodGet, true},
		{"ops can exec into process", "ops@test.com", "/apps/myapp/processes/p1/exec", MethodSocket, true},
		
		// Test exact vs prefix matching for other paths
		{"viewer can list apps", "viewer@test.com", "/apps", MethodGet, true},
		{"viewer can view specific app", "viewer@test.com", "/apps/myapp", MethodGet, true},
		{"path /apps doesn't match /apps/extra/path", "viewer@test.com", "/apps/myapp/extra", MethodGet, false},
		
		// Test builds vs builds/import
		{"viewer can view builds", "viewer@test.com", "/apps/myapp/builds", MethodGet, true},
		{"viewer can view specific build", "viewer@test.com", "/apps/myapp/builds/b123", MethodGet, true},
		{"viewer CANNOT import builds", "viewer@test.com", "/apps/myapp/builds/import", MethodPost, false},
		
		// Test logs endpoints (should match exactly)
		{"viewer can view app logs", "viewer@test.com", "/apps/myapp/logs", MethodSocket, true},
		{"viewer can view build logs", "viewer@test.com", "/apps/myapp/builds/b123/logs", MethodSocket, true},
		{"viewer can view process logs", "viewer@test.com", "/apps/myapp/processes/p1/logs", MethodSocket, true},
		
		// Test resources vs resources/console
		{"viewer can view resources", "viewer@test.com", "/apps/myapp/resources", MethodGet, true},
		{"viewer can view specific resource", "viewer@test.com", "/apps/myapp/resources/db", MethodGet, true},
		{"viewer CANNOT access resource console", "viewer@test.com", "/apps/myapp/resources/db/console", MethodSocket, false},
		{"ops can access resource console", "ops@test.com", "/apps/myapp/resources/db/console", MethodSocket, true},
		
		// Test instances vs instances/shell
		{"viewer can list instances", "viewer@test.com", "/instances", MethodGet, true},
		{"viewer CANNOT access instance shell", "viewer@test.com", "/instances/i-123/shell", MethodSocket, false},
		{"ops can access instance shell", "ops@test.com", "/instances/i-123/shell", MethodSocket, true},
		
		// Test files endpoints
		{"viewer CANNOT access files", "viewer@test.com", "/apps/myapp/processes/p1/files", MethodGet, false},
		{"ops can access files", "ops@test.com", "/apps/myapp/processes/p1/files", MethodGet, true},
		{"ops can upload files", "ops@test.com", "/apps/myapp/processes/p1/files", MethodPost, true},
		{"ops can delete files", "ops@test.com", "/apps/myapp/processes/p1/files", MethodDelete, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission(tt.user, tt.path, tt.method)
			assert.Equal(t, tt.allowed, result, "Exact path matching failed for %s %s %s", tt.user, tt.method, tt.path)
		})
	}
}

func TestMultiSegmentWildcards(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add users
	require.NoError(t, manager.AddUser("viewer@test.com", "Viewer", []string{"viewer"}))
	require.NoError(t, manager.AddUser("ops@test.com", "Ops", []string{"ops"}))
	require.NoError(t, manager.AddUser("admin@test.com", "Admin", []string{"admin"}))

	tests := []struct {
		name     string
		user     string
		path     string
		method   string
		allowed  bool
	}{
		// Test object storage paths with multi-segment wildcards
		// Note: ops has read access to objects, not viewer
		{"viewer CANNOT read objects", "viewer@test.com", "/apps/myapp/objects/file.txt", MethodGet, false},
		{"ops can read object", "ops@test.com", "/apps/myapp/objects/file.txt", MethodGet, true},
		{"ops can read nested object", "ops@test.com", "/apps/myapp/objects/path/to/file.txt", MethodGet, true},
		{"ops can read deeply nested object", "ops@test.com", "/apps/myapp/objects/very/deep/path/to/file.txt", MethodGet, true},
		{"ops CANNOT write objects", "ops@test.com", "/apps/myapp/objects/file.txt", MethodPost, false},
		
		// Test registry paths
		{"admin can access registry v2 API", "admin@test.com", "/v2/anything", MethodAny, true},
		{"admin can access nested registry paths", "admin@test.com", "/v2/library/nginx/manifests/latest", MethodAny, true},
		{"viewer CANNOT access registry API", "viewer@test.com", "/v2/anything", MethodGet, false},
		
		// Test custom proxy paths
		{"admin can use custom proxy", "admin@test.com", "/custom/http/proxy/anything", MethodAny, true},
		{"admin can use nested proxy paths", "admin@test.com", "/custom/http/proxy/path/to/service", MethodAny, true},
		{"ops CANNOT use custom proxy", "ops@test.com", "/custom/http/proxy/anything", MethodGet, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission(tt.user, tt.path, tt.method)
			assert.Equal(t, tt.allowed, result, "Multi-segment wildcard failed for %s %s %s", tt.user, tt.method, tt.path)
		})
	}
}

func TestUserPersistence(t *testing.T) {
	tempDir := t.TempDir()
	
	paths := ConfigPaths{
		UsersPath:    filepath.Join(tempDir, "users.yaml"),
		RolesPath:    filepath.Join(tempDir, "roles.yaml"),
		PoliciesPath: filepath.Join(tempDir, "policies.yaml"),
	}

	// Create first manager and add user
	manager1, err := NewManager(paths)
	require.NoError(t, err)

	err = manager1.AddUser("persist@test.com", "Persist User", []string{"deployer"})
	require.NoError(t, err)

	// Create second manager and check user exists
	manager2, err := NewManager(paths)
	require.NoError(t, err)

	users := manager2.GetUsers()
	user, exists := users["persist@test.com"]
	require.True(t, exists, "User should be persisted")
	assert.Equal(t, "Persist User", user.Name)
	assert.Equal(t, []string{"deployer"}, user.Roles)

	// Check that permissions work for persisted user
	allowed := manager2.CheckPermission("persist@test.com", "/apps/myapp/builds", MethodPost)
	assert.True(t, allowed, "Persisted user should retain deployer permissions")
}

func TestMultipleRoles(t *testing.T) {
	manager := setupTestManager(t)
	
	// Add user with multiple roles
	err := manager.AddUser("multi@test.com", "Multi Role User", []string{"viewer", "ops"})
	require.NoError(t, err)

	// Should have combined permissions of both roles
	tests := []struct {
		name     string
		method   string
		path     string
		allowed  bool
	}{
		// Viewer permissions
		{"has viewer read permissions", MethodGet, "/apps", true},
		
		// Ops permissions
		{"has ops process management", MethodDelete, "/apps/myapp/processes/p1", true},
		
		// Should not have deployer permissions
		{"no deployer permissions", MethodPost, "/apps", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.CheckPermission("multi@test.com", tt.path, tt.method)
			assert.Equal(t, tt.allowed, result)
		})
	}
}

func TestUnknownUser(t *testing.T) {
	manager := setupTestManager(t)

	// Unknown users should have no roles
	roles := manager.GetUserRoles("unknown@test.com")
	assert.Equal(t, []string{}, roles)

	// Check that unknown user is blocked from everything
	allowed := manager.CheckPermission("unknown@test.com", "/apps", MethodGet)
	assert.False(t, allowed, "Unknown user should be blocked from all access")

	denied := manager.CheckPermission("unknown@test.com", "/apps", MethodPost)
	assert.False(t, denied, "Unknown user should be blocked from all access")
}

// Helper function to set up a test manager
func setupTestManager(t *testing.T) *Manager {
	tempDir := t.TempDir()
	
	paths := ConfigPaths{
		UsersPath:    filepath.Join(tempDir, "users.yaml"),
		RolesPath:    filepath.Join(tempDir, "roles.yaml"),
		PoliciesPath: filepath.Join(tempDir, "policies.yaml"),
	}

	manager, err := NewManager(paths)
	require.NoError(t, err, "Failed to create RBAC manager")
	
	return manager
}