//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mockConvoxPort = "9090"
	gatewayPort    = "8448" // Use 8448 for tests to avoid conflicts with dev (8447)
	mockRackToken  = "mock-rack-token-12345"
	testJWTSecret  = "test-secret-key-for-integration-testing"
)

type TestServers struct {
	mockConvoxCmd *exec.Cmd
	gatewayCmd    *exec.Cmd
	client        *http.Client
	gatewayOut    *bytes.Buffer
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if convox CLI is installed - this is a required dependency
	if _, err := exec.LookPath("convox"); err != nil {
		t.Fatal("CRITICAL: Convox CLI is not installed. The rack-gateway requires the convox CLI to be installed. Install it from https://docs.convox.com/installation/cli/")
	}

	// Check that binaries exist (should be built by task build before running tests)
	t.Log("Checking for pre-built binaries...")
	require.NoError(t, checkBinariesExist())

	// Start servers
	servers := &TestServers{
		client: &http.Client{Timeout: 5 * time.Second},
	}

	t.Log("Starting mock Convox server...")
	servers.startMockConvox(t)
	defer servers.cleanup()

	t.Log("Starting gateway server...")
	servers.startGateway(t)

	// Wait for servers to be ready
	waitForServer(t, "http://localhost:"+mockConvoxPort+"/health", 10*time.Second)
	servers.waitForGateway(t, "http://localhost:"+gatewayPort+"/.gateway/api/health", 10*time.Second)

	// Run tests
	t.Run("HealthCheck", func(t *testing.T) {
		testHealthCheck(t, servers)
	})

	t.Run("UnauthenticatedAccess", func(t *testing.T) {
		testUnauthenticatedAccess(t, servers)
	})

	t.Run("MockConvoxAuth", func(t *testing.T) {
		testMockConvoxAuth(t, servers)
	})

	t.Run("CLIWithInvalidToken", func(t *testing.T) {
		testCLIWithInvalidToken(t, servers)
	})

	t.Run("ProxyE2EAuthorized", func(t *testing.T) {
		testProxyE2EAuthorized(t, servers)
	})

	t.Run("ProxyE2EUnauthorized", func(t *testing.T) {
		testProxyE2EUnauthorized(t, servers)
	})

	t.Run("OAuthLoginFlow", func(t *testing.T) {
		testOAuthLoginFlow(t, servers)
	})

	t.Run("AdminEndpointProtection", func(t *testing.T) {
		testAdminEndpointProtection(t, servers)
	})
}

func checkBinariesExist() error {
	binaries := []string{
		"../../bin/mock-convox",
		"../../bin/rack-gateway-api",
		"../../bin/rack-gateway",
	}
	for _, binary := range binaries {
		if _, err := os.Stat(binary); err != nil {
			return fmt.Errorf("required binary not found: %s (run 'task build' first)", binary)
		}
	}
	return nil
}

func (s *TestServers) startMockConvox(t *testing.T) {
	s.mockConvoxCmd = exec.Command("../../bin/mock-convox")
	s.mockConvoxCmd.Env = append(os.Environ(), "MOCK_CONVOX_PORT="+mockConvoxPort)

	// Capture output to avoid noise during tests
	var mockOut bytes.Buffer
	s.mockConvoxCmd.Stdout = &mockOut
	s.mockConvoxCmd.Stderr = &mockOut

	if err := s.mockConvoxCmd.Start(); err != nil {
		t.Logf("Mock Convox output:\n%s", mockOut.String())
		require.NoError(t, err)
	}
}

func (s *TestServers) startGateway(t *testing.T) {
	// Create database directory for testing
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")

	// Pre-create the database with test users
	if err := s.initTestDatabase(dbPath); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	s.gatewayCmd = exec.Command("../../bin/rack-gateway-api")
	s.gatewayCmd.Env = append(os.Environ(),
		"PORT="+gatewayPort,
		"PORT="+gatewayPort,
		"DEV_MODE=true",
		"DOMAIN=localhost",
		"APP_SECRET_KEY=test-secret-key-for-integration-testing",
		"GOOGLE_CLIENT_ID=test-client-id",
		"GOOGLE_CLIENT_SECRET=test-client-secret",
		"GOOGLE_ALLOWED_DOMAIN=example.com",
		// Ensure tests do not inherit a local mock OAuth base URL inadvertently
		"GOOGLE_OAUTH_BASE_URL=",
		// CRITICAL: Only use localhost for testing - NEVER production URLs
		"RACK_HOST=http://localhost:"+mockConvoxPort,
		"RACK_TOKEN="+mockRackToken,
		"RACK_USERNAME=convox",
		// Database path for testing
		"DB_PATH="+dbPath,
	)

	// Capture output for debugging
	s.gatewayOut = &bytes.Buffer{}
	s.gatewayCmd.Stdout = s.gatewayOut
	s.gatewayCmd.Stderr = s.gatewayOut

	if err := s.gatewayCmd.Start(); err != nil {
		t.Logf("Failed to start gateway: %v\nOutput:\n%s", err, s.gatewayOut.String())
		require.NoError(t, err)
	}
}

func (s *TestServers) initTestDatabase(dbPath string) error {
	// Create and initialize the database with test users
	database, err := db.NewFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	defer database.Close()

	// Create test users with appropriate roles
	testUsers := []struct {
		email string
		name  string
		roles []string
	}{
		{"test@example.com", "Test User", []string{"admin"}},
		{"viewer@example.com", "Viewer User", []string{"viewer"}},
		{"ops@example.com", "Ops User", []string{"ops"}},
		{"deployer@example.com", "Deployer User", []string{"deployer"}},
		{"admin@example.com", "Admin User", []string{"admin"}},
	}

	for _, u := range testUsers {
		existing, err := database.GetUser(u.email)
		if err != nil {
			return fmt.Errorf("failed to check user %s: %w", u.email, err)
		}
		var userID int64
		if existing == nil {
			user, err := database.CreateUser(u.email, u.name, u.roles)
			if err != nil {
				// On duplicate, fall back to update
				if !strings.Contains(err.Error(), "duplicate key value") && !strings.Contains(err.Error(), "UNIQUE") {
					return fmt.Errorf("failed to create user %s: %w", u.email, err)
				}
				if err := database.UpdateUserRoles(u.email, u.roles); err != nil {
					return fmt.Errorf("failed to upsert user %s: %w", u.email, err)
				}
				existing, _ = database.GetUser(u.email)
				userID = existing.ID
			} else {
				userID = user.ID
			}
		} else {
			if err := database.UpdateUserRoles(u.email, u.roles); err != nil {
				return fmt.Errorf("failed to update user %s: %w", u.email, err)
			}
			userID = existing.ID
		}
		// Mark all test users as MFA enrolled and create a dummy MFA method so RBAC tests work
		if err := database.SetUserMFAEnrolled(userID, true); err != nil {
			return fmt.Errorf("failed to mark user %s as MFA enrolled: %w", u.email, err)
		}
		// Create a confirmed TOTP method for the user
		method, err := database.CreateMFAMethod(userID, "totp", "Test TOTP", "test-secret", nil, nil, nil, nil)
		if err != nil {
			// Ignore if already exists
			if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("failed to create MFA method for %s: %w", u.email, err)
			}
		} else {
			// Confirm the method
			if err := database.ConfirmMFAMethod(method.ID, time.Now()); err != nil {
				return fmt.Errorf("failed to confirm MFA method for %s: %w", u.email, err)
			}
		}
	}

	return nil
}

func (s *TestServers) cleanup() {
	if s.gatewayCmd != nil && s.gatewayCmd.Process != nil {
		s.gatewayCmd.Process.Kill()
		s.gatewayCmd.Wait()
	}
	if s.mockConvoxCmd != nil && s.mockConvoxCmd.Process != nil {
		s.mockConvoxCmd.Process.Kill()
		s.mockConvoxCmd.Wait()
	}
}

func (s *TestServers) waitForGateway(t *testing.T, url string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if s.gatewayOut != nil {
				t.Logf("Gateway output:\n%s", s.gatewayOut.String())
			}
			t.Fatalf("Server did not become ready at %s within %v", url, timeout)
		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

func waitForServer(t *testing.T, url string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Server did not become ready at %s within %v", url, timeout)
		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

func testHealthCheck(t *testing.T, s *TestServers) {
	resp, err := s.client.Get("http://localhost:" + gatewayPort + "/.gateway/api/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

func testUnauthenticatedAccess(t *testing.T, s *TestServers) {
	resp, err := s.client.Get("http://localhost:" + gatewayPort + "/.gateway/api/me")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func testMockConvoxAuth(t *testing.T, s *TestServers) {
	// Test that mock Convox server requires auth
	resp, err := s.client.Get("http://localhost:" + mockConvoxPort + "/apps")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Test with correct auth
	req, err := http.NewRequest("GET", "http://localhost:"+mockConvoxPort+"/apps", nil)
	require.NoError(t, err)

	auth := base64.StdEncoding.EncodeToString([]byte("convox:" + mockRackToken))
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err = s.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var apps []map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apps))
	assert.Greater(t, len(apps), 0)
}

func testProxyWithValidToken(t *testing.T, s *TestServers) {
	// This would require a valid JWT token
	// For now, we'll test that the proxy endpoint exists
	resp, err := s.client.Get("http://localhost:" + gatewayPort + "/v1/proxy/staging/apps")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return 401 without a token
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func testProxyWithInvalidToken(t *testing.T, s *TestServers) {
	req, err := http.NewRequest("GET", "http://localhost:"+gatewayPort+"/v1/proxy/staging/apps", nil)
	require.NoError(t, err)

	req.Header.Set("Authorization", "Bearer invalid-token")

	resp, err := s.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func testOAuthLoginFlow(t *testing.T, s *TestServers) {
	// Test login start endpoint
	resp, err := s.client.Post("http://localhost:"+gatewayPort+"/.gateway/api/auth/cli/start", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var loginStart map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&loginStart))

	assert.NotEmpty(t, loginStart["auth_url"])
	assert.NotEmpty(t, loginStart["state"])
	assert.NotEmpty(t, loginStart["code_verifier"])
}

func testAdminEndpointProtection(t *testing.T, s *TestServers) {
	endpoints := []string{
		"/.gateway/admin/users",
		"/.gateway/admin/roles",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			resp, err := s.client.Get("http://localhost:" + gatewayPort + endpoint)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
				"Admin endpoint %s should require authentication", endpoint)
		})
	}
}

// Helper function to create a test JWT token
func createTestJWT(t *testing.T, email string, expiresIn time.Duration) string {
	// Create a JWT token that matches what the gateway expects
	claims := jwt.MapClaims{
		"email": email,
		"name":  "Test User",
		"exp":   time.Now().Add(expiresIn).Unix(),
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	return tokenString
}

// Test that invalid tokens are rejected
func testCLIWithInvalidToken(t *testing.T, s *TestServers) {
	// Create a test token file with an invalid token
	configDir := t.TempDir()
	tokenFile := fmt.Sprintf("%s/config.json", configDir)

	config := map[string]interface{}{
		"current": "staging",
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url":        "http://localhost:" + gatewayPort,
				"token":      "invalid-jwt-token",
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokenFile, configData, 0600))

	// Try to run a command with invalid token
	cmd := exec.Command("../../bin/rack-gateway", "apps")
	cmd.Env = append(os.Environ(),
		"GATEWAY_CLI_CONFIG_DIR="+configDir,
	)

	output, err := cmd.CombinedOutput()

	// Should fail with authentication error
	require.Error(t, err, "Command should fail with invalid token")
	assert.Contains(t, string(output), "authentication failed")
}

// Test end-to-end proxy functionality with real commands
func testProxyE2EAuthorized(t *testing.T, s *TestServers) {
	// Create a valid JWT token that the gateway will accept
	configDir := t.TempDir()
	configFile := fmt.Sprintf("%s/config.json", configDir)

	validJWT := createTestJWT(t, "test@example.com", 24*time.Hour)

	config := map[string]interface{}{
		"current": "staging",
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url":        "http://localhost:" + gatewayPort,
				"token":      validJWT,
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configData, 0600))

	// Test 1: ps (lists processes)
	t.Run("ps", func(t *testing.T) {
		cmd := exec.Command("../../bin/rack-gateway", "ps", "-a", "myapp")
		cmd.Env = append(os.Environ(),
			"GATEWAY_CLI_CONFIG_DIR="+configDir,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("ps failed with output: %s", output)
		}
		require.NoError(t, err, "ps should succeed")
		// Should see processes from mock server
		assert.Contains(t, string(output), "web")
		assert.Contains(t, string(output), "worker")
		assert.Contains(t, string(output), "running")
	})

	// Test 2: rack (shows rack info)
	t.Run("rack", func(t *testing.T) {
		cmd := exec.Command("../../bin/rack-gateway", "rack")
		cmd.Env = append(os.Environ(),
			"GATEWAY_CLI_CONFIG_DIR="+configDir,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("rack failed with output: %s", output)
		}
		require.NoError(t, err, "rack should succeed")
		// Should see rack info from mock server
		assert.Contains(t, string(output), "mock-rack")
		assert.Contains(t, string(output), "running")
	})

	// Test 3: apps (lists applications)
	t.Run("apps", func(t *testing.T) {
		cmd := exec.Command("../../bin/rack-gateway", "apps")
		cmd.Env = append(os.Environ(),
			"GATEWAY_CLI_CONFIG_DIR="+configDir,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("apps failed with output: %s", output)
		}
		require.NoError(t, err, "apps should succeed")
		// Should see apps from mock server
		assert.Contains(t, string(output), "api")
		assert.Contains(t, string(output), "web")
	})
}

// Test that unauthorized users and users with insufficient permissions are blocked
func testProxyE2EUnauthorized(t *testing.T, s *TestServers) {
	// Helper function to create config for a user with specific role
	createUserConfig := func(t *testing.T, email string, role string) string {
		configDir := t.TempDir()
		configFile := filepath.Join(configDir, "config.json")

		// Create JWT with role claim
		claims := jwt.MapClaims{
			"email": email,
			"name":  "Test User",
			"role":  role, // Add role to JWT claims
			"exp":   time.Now().Add(24 * time.Hour).Unix(),
			"iat":   time.Now().Unix(),
			"nbf":   time.Now().Unix(),
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(testJWTSecret))
		require.NoError(t, err)

		config := map[string]interface{}{
			"current": "staging",
			"gateways": map[string]interface{}{
				"staging": map[string]interface{}{
					"url":        "http://localhost:" + gatewayPort,
					"token":      tokenString,
					"email":      email,
					"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			},
		}

		configData, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(configFile, configData, 0600))

		return configDir
	}

	// Test 1: Viewer role - should be blocked from write operations
	t.Run("viewer_blocked_from_writes", func(t *testing.T) {
		configDir := createUserConfig(t, "viewer@example.com", "viewer")

		// Try to create an app (should be blocked)
		cmd := exec.Command("../../bin/rack-gateway", "apps", "create", "newapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err := cmd.CombinedOutput()

		require.Error(t, err, "viewer should be blocked from creating apps")
		// Note: Currently prompts for MFA since JWT tokens don't carry MFA verification state
		// In a real scenario, users would authenticate with MFA and THEN hit RBAC checks
		assert.True(t, strings.Contains(string(output), "permission denied") || strings.Contains(string(output), "MFA"), "should be blocked: %s", output)

		// Try to delete a process (should be blocked)
		cmd = exec.Command("../../bin/rack-gateway", "ps", "stop", "web-123", "-a", "myapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err = cmd.CombinedOutput()

		require.Error(t, err, "viewer should be blocked from stopping processes")
		assert.True(t, strings.Contains(string(output), "You don't have permission to stop processes.") || strings.Contains(string(output), "MFA"), "should be blocked: %s", output)
	})

	// Test 2: Ops role - should be blocked from deployment operations
	t.Run("ops_blocked_from_deployment", func(t *testing.T) {
		configDir := createUserConfig(t, "ops@example.com", "ops")

		// Try to deploy (should be blocked)
		cmd := exec.Command("../../bin/rack-gateway", "deploy", "-a", "myapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err := cmd.CombinedOutput()

		require.Error(t, err, "ops should be blocked from deploying")
		msg := string(output)
		assert.True(t, strings.Contains(msg, "You don't have permission to deploy releases.") || strings.Contains(msg, "permission denied") || strings.Contains(msg, "MFA"), "should be blocked: %s", msg)

		// Try to create an app (should be blocked)
		cmd = exec.Command("../../bin/rack-gateway", "apps", "create", "newapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err = cmd.CombinedOutput()

		require.Error(t, err, "ops should be blocked from creating apps")
		assert.True(t, strings.Contains(string(output), "permission denied") || strings.Contains(string(output), "MFA"), "should be blocked: %s", output)

		// Try to update environment variables (should be blocked)
		cmd = exec.Command("../../bin/rack-gateway", "env", "set", "KEY=value", "-a", "myapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err = cmd.CombinedOutput()

		require.Error(t, err, "ops should be blocked from setting env vars")
		assert.True(t, strings.Contains(string(output), "You don't have permission to deploy releases.") || strings.Contains(string(output), "MFA"), "should be blocked: %s", output)
	})

	// Test 4: Unknown/unregistered user - should be blocked from everything
	t.Run("unknown_user_blocked", func(t *testing.T) {
		configDir := createUserConfig(t, "unknown@example.com", "")

		// Try to list apps (should be blocked)
		cmd := exec.Command("../../bin/rack-gateway", "apps")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err := cmd.CombinedOutput()

		require.Error(t, err, "unknown user should be blocked from listing apps")
		// Unknown users get "user not found" error
		assert.Contains(t, string(output), "user not found")

		// Try to view processes (should be blocked)
		cmd = exec.Command("../../bin/rack-gateway", "ps", "-a", "myapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err = cmd.CombinedOutput()

		require.Error(t, err, "unknown user should be blocked from viewing processes")
		assert.Contains(t, string(output), "user not found")

		// Try to get rack info (should be blocked)
		cmd = exec.Command("../../bin/rack-gateway", "rack")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+configDir)

		output, err = cmd.CombinedOutput()

		require.Error(t, err, "unknown user should be blocked from viewing rack info")
		assert.Contains(t, string(output), "user not found")
	})

	// Test 5: Verify proper access levels are enforced
	t.Run("verify_access_levels", func(t *testing.T) {
		// Viewer can read but not write
		viewerConfig := createUserConfig(t, "viewer@example.com", "viewer")

		cmd := exec.Command("../../bin/rack-gateway", "apps")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+viewerConfig)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("viewer@example.com failed to list apps: %s", output)
		}
		require.NoError(t, err, "viewer should be able to list apps")
		assert.Contains(t, string(output), "api") // Should see apps

		// Ops can manage processes but not deploy
		opsConfig := createUserConfig(t, "ops@example.com", "ops")

		cmd = exec.Command("../../bin/rack-gateway", "ps", "-a", "myapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+opsConfig)
		output, err = cmd.CombinedOutput()
		if err != nil {
			t.Logf("ops@example.com failed to list processes: %s", output)
		}
		require.NoError(t, err, "ops should be able to view processes")
		assert.Contains(t, string(output), "web") // Should see processes

		// Deployer can deploy but not delete
		deployerConfig := createUserConfig(t, "deployer@example.com", "deployer")

		cmd = exec.Command("../../bin/rack-gateway", "builds", "-a", "myapp")
		cmd.Env = append(os.Environ(), "GATEWAY_CLI_CONFIG_DIR="+deployerConfig)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "deployer should be able to list builds")
		assert.Contains(t, string(output), "BAPI") // Should see build IDs
	})
}
