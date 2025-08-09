//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mockConvoxPort = "9090"
	gatewayPort    = "8080"
	mockRackToken  = "mock-rack-token-12345"
	testJWTSecret  = "test-secret-key-for-integration-testing"
)

type TestServers struct {
	mockConvoxCmd *exec.Cmd
	gatewayCmd    *exec.Cmd
	client        *http.Client
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if convox CLI is installed - this is a required dependency
	if _, err := exec.LookPath("convox"); err != nil {
		t.Fatal("CRITICAL: Convox CLI is not installed. The convox-gateway requires the convox CLI to be installed. Install it from https://docs.convox.com/installation/cli/")
	}

	// Build binaries - fix paths to be relative to project root
	t.Log("Building binaries...")
	require.NoError(t, buildBinary("../../cmd/mock-convox/main.go", "../../bin/mock-convox"))
	require.NoError(t, buildBinary("../../cmd/api/main.go", "../../bin/convox-gateway-api"))
	require.NoError(t, buildBinary("../../cmd/cli/main.go", "../../bin/convox-gateway"))

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
	waitForServer(t, "http://localhost:"+gatewayPort+"/health", 10*time.Second)

	// Run tests
	t.Run("HealthCheck", func(t *testing.T) {
		testHealthCheck(t, servers)
	})

	t.Run("UnauthorizedAccess", func(t *testing.T) {
		testUnauthorizedAccess(t, servers)
	})

	t.Run("MockConvoxAuth", func(t *testing.T) {
		testMockConvoxAuth(t, servers)
	})

	t.Run("testCLIWrapsConvoxHelpAndVersionCommands", func(t *testing.T) {
		testCLIWrapsConvoxHelpAndVersionCommands(t, servers)
	})

	t.Run("CLIWithInvalidToken", func(t *testing.T) {
		testCLIWithInvalidToken(t, servers)
	})

	t.Run("ProxyE2E", func(t *testing.T) {
		testProxyE2E(t, servers)
	})

	t.Run("OAuthLoginFlow", func(t *testing.T) {
		testOAuthLoginFlow(t, servers)
	})

	t.Run("AdminEndpointProtection", func(t *testing.T) {
		testAdminEndpointProtection(t, servers)
	})
}

func buildBinary(source, output string) error {
	cmd := exec.Command("go", "build", "-o", output, source)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (s *TestServers) startMockConvox(t *testing.T) {
	s.mockConvoxCmd = exec.Command("../../bin/mock-convox")
	s.mockConvoxCmd.Env = append(os.Environ(), "MOCK_CONVOX_PORT="+mockConvoxPort)

	// Capture output for debugging
	s.mockConvoxCmd.Stdout = os.Stdout
	s.mockConvoxCmd.Stderr = os.Stderr

	require.NoError(t, s.mockConvoxCmd.Start())
}

func (s *TestServers) startGateway(t *testing.T) {
	s.gatewayCmd = exec.Command("../../bin/convox-gateway-api")
	s.gatewayCmd.Env = append(os.Environ(),
		"PORT="+gatewayPort,
		"APP_ENV=test",
		"APP_JWT_KEY=test-secret-key-for-integration-testing",
		"GOOGLE_CLIENT_ID=test-client-id",
		"GOOGLE_CLIENT_SECRET=test-client-secret",
		"GOOGLE_ALLOWED_DOMAIN=example.com",
		// CRITICAL: Only use localhost for testing - NEVER production URLs
		"RACK_HOST=http://localhost:"+mockConvoxPort,
		"RACK_TOKEN="+mockRackToken,
		"RACK_USERNAME=convox",
	)

	// Capture output for debugging
	s.gatewayCmd.Stdout = os.Stdout
	s.gatewayCmd.Stderr = os.Stderr

	require.NoError(t, s.gatewayCmd.Start())
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
	resp, err := s.client.Get("http://localhost:" + gatewayPort + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "healthy", result["status"])
}

func testUnauthorizedAccess(t *testing.T, s *TestServers) {
	resp, err := s.client.Get("http://localhost:" + gatewayPort + "/.gateway/me")
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
	resp, err := s.client.Post("http://localhost:"+gatewayPort+"/.gateway/login/start", "application/json", nil)
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

// Test that our CLI has proper commands
func testCLIWrapsConvoxHelpAndVersionCommands(t *testing.T, s *TestServers) {
	// Create a test config directory with valid config
	configDir := t.TempDir()
	configFile := fmt.Sprintf("%s/config.json", configDir)

	// Create config with gateway URL and token
	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url": "http://localhost:" + gatewayPort,
			},
		},
		"tokens": map[string]interface{}{
			"staging": map[string]interface{}{
				"token":      "test-jwt-token",
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configData, 0600))
	
	// Set current rack
	currentFile := filepath.Join(configDir, "current")
	require.NoError(t, os.WriteFile(currentFile, []byte("staging"), 0600))

	// Test version command shows gateway version
	t.Run("version", func(t *testing.T) {
		cmd := exec.Command("../../bin/convox-gateway", "version")
		cmd.Env = append(os.Environ(),
			"CONVOX_GATEWAY_CONFIG="+configDir,
		)

		output, _ := cmd.CombinedOutput()
		// Should show convox-gateway version
		assert.Contains(t, string(output), "convox-gateway version")
	})

	// Test convox command passes through to real convox CLI
	t.Run("convox", func(t *testing.T) {
		cmd := exec.Command("../../bin/convox-gateway", "convox", "help")
		cmd.Env = append(os.Environ(),
			"CONVOX_GATEWAY_CONFIG="+configDir,
		)

		output, _ := cmd.CombinedOutput()
		// Should contain convox commands from real CLI
		assert.Contains(t, string(output), "convox apps")
		assert.Contains(t, string(output), "convox ps")
	})
	
	// Test rack command shows current rack
	t.Run("rack", func(t *testing.T) {
		cmd := exec.Command("../../bin/convox-gateway", "rack")
		cmd.Env = append(os.Environ(),
			"CONVOX_GATEWAY_CONFIG="+configDir,
		)

		output, _ := cmd.CombinedOutput()
		// Should show current rack info
		assert.Contains(t, string(output), "Current rack: staging")
		assert.Contains(t, string(output), "Gateway URL: http://localhost:" + gatewayPort)
	})
	
	// Test racks command lists configured racks
	t.Run("racks", func(t *testing.T) {
		cmd := exec.Command("../../bin/convox-gateway", "racks")
		cmd.Env = append(os.Environ(),
			"CONVOX_GATEWAY_CONFIG="+configDir,
		)

		output, _ := cmd.CombinedOutput()
		// Should list configured racks
		assert.Contains(t, string(output), "staging")
		assert.Contains(t, string(output), "http://localhost:" + gatewayPort)
	})
}

// Test that invalid tokens are rejected
func testCLIWithInvalidToken(t *testing.T, s *TestServers) {
	// Create a test token file with an invalid token
	configDir := t.TempDir()
	tokenFile := fmt.Sprintf("%s/config.json", configDir)

	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url": "http://localhost:" + gatewayPort,
			},
		},
		"tokens": map[string]interface{}{
			"staging": map[string]interface{}{
				"token":      "invalid-jwt-token",
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokenFile, configData, 0600))
	
	// Set current rack
	currentFile := filepath.Join(configDir, "current")
	require.NoError(t, os.WriteFile(currentFile, []byte("staging"), 0600))

	// Try to run a command with invalid token
	cmd := exec.Command("../../bin/convox-gateway", "convox", "apps")
	cmd.Env = append(os.Environ(),
		"CONVOX_GATEWAY_CONFIG="+configDir,
	)

	output, err := cmd.CombinedOutput()
	
	// Should fail with authentication error
	require.Error(t, err, "Command should fail with invalid token")
	assert.Contains(t, string(output), "ERROR")
	assert.Contains(t, string(output), "invalid token")
}

// Test end-to-end proxy functionality with real commands
func testProxyE2E(t *testing.T, s *TestServers) {
	// Create a valid JWT token that the gateway will accept
	configDir := t.TempDir()
	configFile := fmt.Sprintf("%s/config.json", configDir)
	
	validJWT := createTestJWT(t, "test@example.com", 24*time.Hour)
	
	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url": "http://localhost:" + gatewayPort,
			},
		},
		"tokens": map[string]interface{}{
			"staging": map[string]interface{}{
				"token":      validJWT,
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configData, 0600))
	
	// Set current rack
	currentFile := filepath.Join(configDir, "current")
	require.NoError(t, os.WriteFile(currentFile, []byte("staging"), 0600))

	// Test 1: convox ps (lists processes)
	t.Run("convox_ps", func(t *testing.T) {
		cmd := exec.Command("../../bin/convox-gateway", "convox", "ps", "-a", "myapp")
		cmd.Env = append(os.Environ(),
			"CONVOX_GATEWAY_CONFIG="+configDir,
		)

		output, err := cmd.CombinedOutput()
		t.Logf("convox ps output: %s", output)
		
		require.NoError(t, err, "convox ps should succeed")
		// Should see processes from mock server
		assert.Contains(t, string(output), "web")
		assert.Contains(t, string(output), "worker")
		assert.Contains(t, string(output), "running")
	})

	// Test 2: convox rack (shows rack info)
	t.Run("convox_rack", func(t *testing.T) {
		cmd := exec.Command("../../bin/convox-gateway", "convox", "rack")
		cmd.Env = append(os.Environ(),
			"CONVOX_GATEWAY_CONFIG="+configDir,
		)

		output, err := cmd.CombinedOutput()
		t.Logf("convox rack output: %s", output)
		
		require.NoError(t, err, "convox rack should succeed")
		// Should see rack info from mock server
		assert.Contains(t, string(output), "mock-rack")
		assert.Contains(t, string(output), "running")
	})
}

// Test the actual convox CLI wrapper functionality
func testConvoxCLIWrapper(t *testing.T, s *TestServers) {
	// Test that convox-gateway passes through commands to convox
	// when given a valid rack configuration

	// First, simulate a successful login by creating a token file
	configDir := t.TempDir()
	configFile := fmt.Sprintf("%s/config.json", configDir)

	// Create a valid JWT token that the gateway will accept
	validJWT := createTestJWT(t, "test@example.com", 24*time.Hour)
	
	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url": "http://localhost:" + gatewayPort,
			},
		},
		"tokens": map[string]interface{}{
			"staging": map[string]interface{}{
				"token":      validJWT, // Use a valid JWT that the gateway will accept
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configData, 0600))
	
	// Set current rack
	currentFile := filepath.Join(configDir, "current")
	require.NoError(t, os.WriteFile(currentFile, []byte("staging"), 0600))

	// Test various convox commands through our wrapper
	testCases := []struct {
		name     string
		args     []string
		checkOut func(t *testing.T, output []byte)
	}{
		{
			name: "apps_list",
			args: []string{"apps"},
			checkOut: func(t *testing.T, output []byte) {
				// Should see apps from our mock server
				assert.Contains(t, string(output), "api")
				assert.Contains(t, string(output), "web")
			},
		},
		{
			name: "rack_info",
			args: []string{"rack"},
			checkOut: func(t *testing.T, output []byte) {
				// Should see rack info from mock server
				assert.Contains(t, string(output), "mock-rack")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Prepend "convox" to the args since all convox commands go through that subcommand now
			args := append([]string{"convox"}, tc.args...)
			cmd := exec.Command("../../bin/convox-gateway", args...)
			cmd.Env = append(os.Environ(),
				"CONVOX_GATEWAY_CONFIG="+configDir,
				// Let convox-gateway build the RACK_URL pointing to the gateway
			)

			output, err := cmd.CombinedOutput()
			t.Logf("Command %v output: %s", tc.args, output)

			if err != nil {
				t.Logf("Command failed: %v", err)
			}

			if tc.checkOut != nil {
				tc.checkOut(t, output)
			}
		})
	}
}
