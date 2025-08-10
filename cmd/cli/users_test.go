package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListUsersCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/.gateway/admin/users", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Return mock users
		users := []User{
			{
				Email:     "admin@example.com",
				Name:      "Admin User",
				Roles:     []string{"admin"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
				Suspended: false,
			},
			{
				Email:     "viewer@example.com",
				Name:      "Viewer User",
				Roles:     []string{"viewer"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
				Suspended: true,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	listCmd, _, err := cmd.Find([]string{"list"})
	require.NoError(t, err)

	// Execute command
	err = listCmd.RunE(listCmd, []string{})
	assert.NoError(t, err)
}

func TestListUsersUnauthorized(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server that returns unauthorized
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "invalid-token")

	// Create command
	cmd := createUsersCmd()
	listCmd, _, err := cmd.Find([]string{"list"})
	require.NoError(t, err)

	// Execute command - should fail
	err = listCmd.RunE(listCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized - admin role required")
}

func TestAddUserCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/.gateway/admin/users", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Verify request body
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "newuser@example.com", reqBody["email"])
		assert.Equal(t, "New User", reqBody["name"])
		assert.Equal(t, []interface{}{"viewer", "ops"}, reqBody["roles"])

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	addCmd, _, err := cmd.Find([]string{"add"})
	require.NoError(t, err)

	// Execute command
	err = addCmd.RunE(addCmd, []string{"newuser@example.com", "New User", "viewer,ops"})
	assert.NoError(t, err)
}

func TestAddUserInvalidRole(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Setup config (server not needed as validation happens client-side)
	setupTestConfig(t, tmpDir, "http://localhost", "test-token")

	// Create command
	cmd := createUsersCmd()
	addCmd, _, err := cmd.Find([]string{"add"})
	require.NoError(t, err)

	// Execute command with invalid role
	err = addCmd.RunE(addCmd, []string{"newuser@example.com", "New User", "invalid-role"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid role: invalid-role")
}

func TestAddUserAlreadyExists(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	addCmd, _, err := cmd.Find([]string{"add"})
	require.NoError(t, err)

	// Execute command
	err = addCmd.RunE(addCmd, []string{"existing@example.com", "Existing User", "viewer"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user existing@example.com already exists")
}

func TestRemoveUserCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/.gateway/admin/users/user@example.com", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	removeCmd, _, err := cmd.Find([]string{"remove"})
	require.NoError(t, err)

	// Execute command
	err = removeCmd.RunE(removeCmd, []string{"user@example.com"})
	assert.NoError(t, err)
}

func TestRemoveUserNotFound(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	removeCmd, _, err := cmd.Find([]string{"remove"})
	require.NoError(t, err)

	// Execute command
	err = removeCmd.RunE(removeCmd, []string{"nonexistent@example.com"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user nonexistent@example.com not found")
}

func TestSetUserRolesCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/.gateway/admin/users/user@example.com/roles", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Verify request body
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, []interface{}{"admin", "deployer"}, reqBody["roles"])

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	setRolesCmd, _, err := cmd.Find([]string{"set-roles"})
	require.NoError(t, err)

	// Execute command
	err = setRolesCmd.RunE(setRolesCmd, []string{"user@example.com", "admin,deployer"})
	assert.NoError(t, err)
}

func TestSetUserRolesInvalidRole(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Setup config (server not needed as validation happens client-side)
	setupTestConfig(t, tmpDir, "http://localhost", "test-token")

	// Create command
	cmd := createUsersCmd()
	setRolesCmd, _, err := cmd.Find([]string{"set-roles"})
	require.NoError(t, err)

	// Execute command with invalid role
	err = setRolesCmd.RunE(setRolesCmd, []string{"user@example.com", "superuser"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid role: superuser")
}

func TestSuspendUserCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/.gateway/admin/users/user@example.com/suspend", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Verify request body
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, true, reqBody["suspended"])

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	suspendCmd, _, err := cmd.Find([]string{"suspend"})
	require.NoError(t, err)

	// Execute command
	err = suspendCmd.RunE(suspendCmd, []string{"user@example.com"})
	assert.NoError(t, err)
}

func TestUnsuspendUserCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/.gateway/admin/users/user@example.com/suspend", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Verify request body
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, false, reqBody["suspended"])

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Setup config
	setupTestConfig(t, tmpDir, server.URL, "test-token")

	// Create command
	cmd := createUsersCmd()
	unsuspendCmd, _, err := cmd.Find([]string{"unsuspend"})
	require.NoError(t, err)

	// Execute command
	err = unsuspendCmd.RunE(unsuspendCmd, []string{"user@example.com"})
	assert.NoError(t, err)
}

func TestNoRackSelected(t *testing.T) {
	// Create a temporary config directory without setting up a current rack
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Create command
	cmd := createUsersCmd()
	listCmd, _, err := cmd.Find([]string{"list"})
	require.NoError(t, err)

	// Execute command - should fail
	err = listCmd.RunE(listCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no rack selected")
}

func TestNotLoggedIn(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Set current rack but no token
	err := setCurrentRack("staging")
	require.NoError(t, err)

	// Save gateway config without token
	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url": "http://localhost",
			},
		},
	}
	configFile := filepath.Join(tmpDir, "config.json")
	configData, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(configFile, configData, 0600)

	// Create command
	cmd := createUsersCmd()
	listCmd, _, err := cmd.Find([]string{"list"})
	require.NoError(t, err)

	// Execute command - should fail
	err = listCmd.RunE(listCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not logged in")
}

// Helper function to setup test config
func setupTestConfig(t *testing.T, tmpDir, gatewayURL, token string) {
	// Set current rack
	err := setCurrentRack("staging")
	require.NoError(t, err)

	// Save gateway config
	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url": gatewayURL,
			},
		},
		"tokens": map[string]interface{}{
			"staging": map[string]interface{}{
				"token":      token,
				"email":      "admin@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	configFile := filepath.Join(tmpDir, "config.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configData, 0600))
}
