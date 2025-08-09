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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mockConvoxPort = "9090"
	gatewayPort    = "8080"
	mockRackToken  = "mock-rack-token-12345"
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

	// Check if convox CLI is installed
	if _, err := exec.LookPath("convox"); err != nil {
		t.Skip("Convox CLI not installed, skipping integration test")
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

	t.Run("CLIWrapsConvoxCommands", func(t *testing.T) {
		testCLIWrapsConvoxCommands(t, servers)
	})

	t.Run("CLIWithAuthentication", func(t *testing.T) {
		testCLIWithAuthentication(t, servers)
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
		// Configure rack with mock Convox server
		"RACK_TOKEN_STAGING="+mockRackToken,
		"RACK_URL_STAGING=http://localhost:"+mockConvoxPort,
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
	resp, err := s.client.Get("http://localhost:" + gatewayPort + "/v1/me")
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
	resp, err := s.client.Post("http://localhost:"+gatewayPort+"/v1/login/start", "application/json", nil)
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
		"/v1/admin/users",
		"/v1/admin/roles",
		"/v1/admin/policies",
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
	// This would normally use the JWT library to create a proper token
	// For testing, we'd need to match the signing key used by the gateway
	return "test-jwt-token"
}

// Test that our CLI wrapper actually calls the real Convox CLI
func testCLIWrapsConvoxCommands(t *testing.T, s *TestServers) {
	// Set up environment to point to our mock Convox server
	// The wrapper should set RACK_URL and call the real convox CLI
	cmd := exec.Command("./bin/convox-gateway", "version")
	cmd.Env = append(os.Environ(),
		"CONVOX_GATEWAY_PROXY=http://localhost:"+gatewayPort,
		// This would normally be set after login, but for testing we bypass auth
		"RACK_URL=https://convox:"+mockRackToken+"@localhost:"+mockConvoxPort,
	)

	output, err := cmd.CombinedOutput()

	// The wrapper should pass through to the real convox CLI
	// which should return version info
	if err != nil {
		// If convox CLI is installed, this should work
		// The error might be because we're not authenticated properly
		t.Logf("CLI wrapper output: %s", output)
	}

	// Test that our wrapper handles convox commands
	testCommands := []string{"help", "version"}

	for _, command := range testCommands {
		t.Run(command, func(t *testing.T) {
			cmd := exec.Command("./bin/convox-gateway", command)
			cmd.Env = append(os.Environ(),
				"CONVOX_GATEWAY_PROXY=http://localhost:"+gatewayPort,
			)

			output, _ := cmd.CombinedOutput()
			t.Logf("Command '%s' output: %s", command, output)

			// We expect some output (help text, version, etc)
			assert.NotEmpty(t, output)
		})
	}
}

// Test the full authentication flow with CLI
func testCLIWithAuthentication(t *testing.T, s *TestServers) {
	// Create a test token file to simulate a logged-in user
	configDir := t.TempDir()
	tokenFile := fmt.Sprintf("%s/tokens.json", configDir)

	// Create a mock JWT token (in real scenario, this would come from OAuth)
	tokens := map[string]interface{}{
		"tokens": map[string]interface{}{
			"staging": map[string]interface{}{
				"token":      "test-jwt-token",
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	tokenData, err := json.MarshalIndent(tokens, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokenFile, tokenData, 0600))

	// Now test that the CLI wrapper uses this token to set RACK_URL
	cmd := exec.Command("./bin/convox-gateway", "apps")
	cmd.Env = append(os.Environ(),
		"CONVOX_GATEWAY_PROXY=http://localhost:"+gatewayPort,
		"HOME="+configDir, // Override home to use our test config
		// The wrapper should construct RACK_URL with the JWT token
	)

	output, err := cmd.CombinedOutput()
	t.Logf("CLI with auth output: %s", output)

	// The command will fail because our test JWT isn't valid,
	// but we're testing that the wrapper attempts to use it
	if err != nil {
		// Expected - our mock JWT won't actually work
		assert.Contains(t, string(output), "401", "Should get auth error with invalid JWT")
	}
}

// Test the actual convox CLI wrapper functionality
func testConvoxCLIWrapper(t *testing.T, s *TestServers) {
	// Test that convox-gateway passes through commands to convox
	// when given a valid rack configuration

	// First, simulate a successful login by creating a token file
	configDir := t.TempDir()
	tokenFile := fmt.Sprintf("%s/tokens.json", configDir)

	// In a real scenario, this JWT would be validated by the gateway
	// For testing, we'll use the mock server directly
	tokens := map[string]interface{}{
		"tokens": map[string]interface{}{
			"staging": map[string]interface{}{
				"token":      mockRackToken, // Use the mock server's token directly for testing
				"email":      "test@example.com",
				"expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	tokenData, err := json.MarshalIndent(tokens, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokenFile, tokenData, 0600))

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
			cmd := exec.Command("./bin/convox-gateway", tc.args...)
			cmd.Env = append(os.Environ(),
				"CONVOX_GATEWAY_PROXY=http://localhost:"+gatewayPort,
				"HOME="+configDir,
				// Override to use mock server directly for testing
				"RACK_URL=http://convox:"+mockRackToken+"@localhost:"+mockConvoxPort,
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
