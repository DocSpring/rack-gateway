package rbac

import (
	"path/filepath"
	"testing"
)

func TestRBACManager(t *testing.T) {
	tempDir := t.TempDir()
	
	paths := ConfigPaths{
		UsersPath:    filepath.Join(tempDir, "users.yaml"),
		RolesPath:    filepath.Join(tempDir, "roles.yaml"),
		PoliciesPath: filepath.Join(tempDir, "policies.yaml"),
	}

	manager, err := NewManager(paths)
	if err != nil {
		t.Fatalf("Failed to create RBAC manager: %v", err)
	}

	t.Run("default roles created", func(t *testing.T) {
		roles := manager.GetRoles()
		expectedRoles := []string{"viewer", "ops", "deployer", "admin"}
		
		for _, expected := range expectedRoles {
			if _, exists := roles[expected]; !exists {
				t.Errorf("Expected role %s not found", expected)
			}
		}
	})

	t.Run("add user with roles", func(t *testing.T) {
		err := manager.AddUser("test@example.com", "Test User", []string{"viewer"})
		if err != nil {
			t.Fatalf("Failed to add user: %v", err)
		}

		roles := manager.GetUserRoles("test@example.com")
		if len(roles) != 1 || roles[0] != "viewer" {
			t.Errorf("Expected user to have viewer role, got %v", roles)
		}
	})

	t.Run("permission checks", func(t *testing.T) {
		tests := []struct {
			email    string
			resource string
			action   string
			expected bool
		}{
			{"test@example.com", "/apps", "GET", true},
			{"test@example.com", "/apps", "POST", false},
			{"test@example.com", "/ps", "GET", true},
			{"test@example.com", "/env/set", "POST", false},
		}

		for _, tt := range tests {
			result := manager.CheckPermission(tt.email, tt.resource, tt.action)
			if result != tt.expected {
				t.Errorf("CheckPermission(%s, %s, %s) = %v, expected %v",
					tt.email, tt.resource, tt.action, result, tt.expected)
			}
		}
	})

	t.Run("admin permissions", func(t *testing.T) {
		err := manager.AddUser("admin@example.com", "Admin User", []string{"admin"})
		if err != nil {
			t.Fatalf("Failed to add admin user: %v", err)
		}

		tests := []struct {
			resource string
			action   string
		}{
			{"/apps", "GET"},
			{"/apps", "POST"},
			{"/env/set", "POST"},
			{"/anything", "DELETE"},
		}

		for _, tt := range tests {
			result := manager.CheckPermission("admin@example.com", tt.resource, tt.action)
			if !result {
				t.Errorf("Admin should have permission for %s %s", tt.action, tt.resource)
			}
		}
	})
}

func TestUserPersistence(t *testing.T) {
	tempDir := t.TempDir()
	
	paths := ConfigPaths{
		UsersPath:    filepath.Join(tempDir, "users.yaml"),
		RolesPath:    filepath.Join(tempDir, "roles.yaml"),
		PoliciesPath: filepath.Join(tempDir, "policies.yaml"),
	}

	manager1, err := NewManager(paths)
	if err != nil {
		t.Fatalf("Failed to create first manager: %v", err)
	}

	err = manager1.AddUser("persist@example.com", "Persist User", []string{"deployer"})
	if err != nil {
		t.Fatalf("Failed to add user: %v", err)
	}

	manager2, err := NewManager(paths)
	if err != nil {
		t.Fatalf("Failed to create second manager: %v", err)
	}

	users := manager2.GetUsers()
	if user, exists := users["persist@example.com"]; !exists {
		t.Error("User not persisted")
	} else {
		if user.Name != "Persist User" {
			t.Errorf("User name not persisted correctly: got %s", user.Name)
		}
		if len(user.Roles) != 1 || user.Roles[0] != "deployer" {
			t.Errorf("User roles not persisted correctly: got %v", user.Roles)
		}
	}
}