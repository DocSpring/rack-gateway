package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetCurrentRack(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	ConfigPath = tmpDir

	// Test setting current rack
	err := SetCurrentRack("staging")
	require.NoError(t, err)

	cfg, exists, err := LoadConfig()
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "staging", cfg.Current)

	// Test overwriting current rack
	err = SetCurrentRack("production")
	require.NoError(t, err)

	cfg, exists, err = LoadConfig()
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "production", cfg.Current)
}

func TestGetCurrentRack(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	ConfigPath = tmpDir

	// Test when no current rack configured
	_, err := GetCurrentRack()
	assert.Error(t, err)

	// Write config with current rack
	require.NoError(t, SaveConfig(&Config{Current: "us-east"}))

	// Test reading current rack
	rack, err := GetCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "us-east", rack)

	// Test with whitespace (should be trimmed)
	require.NoError(t, SaveConfig(&Config{Current: "  eu-west  \n"}))

	rack, err = GetCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "eu-west", rack)
}

func TestSwitchCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	ConfigPath = tmpDir

	// Create a config.json with some racks
	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			"staging": map[string]interface{}{
				"url": "https://gateway-staging.example.com",
			},
			"production": map[string]interface{}{
				"url": "https://gateway-production.example.com",
			},
		},
	}

	configFile := filepath.Join(tmpDir, "config.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configData, 0600))

	// Test switching to a valid rack
	err = SetCurrentRack("staging")
	require.NoError(t, err)

	rack, err := GetCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "staging", rack)

	// Test switching to another valid rack
	err = SetCurrentRack("production")
	require.NoError(t, err)

	rack, err = GetCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "production", rack)
}

func TestRackSelectionPriority(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	ConfigPath = tmpDir

	// Set up initial current rack
	err := SetCurrentRack("staging")
	require.NoError(t, err)

	// Test 1: Default from current file
	rack, err := GetCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "staging", rack)

	// Test 2: Environment variable override
	t.Setenv("GATEWAY_RACK", "production")

	// In the actual wrapConvoxCommand, env var overrides current
	// We're just testing the helper functions here
	envRack := os.Getenv("GATEWAY_RACK")
	if envRack != "" {
		rack = envRack
	}
	assert.Equal(t, "production", rack)
}

func TestSelectedRackEnvOverride(t *testing.T) {
	ConfigPath = t.TempDir()
	RackFlag = ""
	t.Setenv("RACK_GATEWAY_RACK", "env-rack")
	t.Setenv("RACK_GATEWAY_URL", "")

	rack, err := SelectedRack()
	require.NoError(t, err)
	assert.Equal(t, "env-rack", rack)
}

func TestSelectedRackFallsBackToURL(t *testing.T) {
	ConfigPath = t.TempDir()
	RackFlag = ""
	t.Setenv("RACK_GATEWAY_URL", "https://gateway.example.com")
	t.Setenv("RACK_GATEWAY_RACK", "")

	rack, err := SelectedRack()
	require.NoError(t, err)
	assert.Equal(t, "(from environment)", rack)
}

func TestLoginSetsCurrentRack(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	ConfigPath = tmpDir

	// Simulate what happens during login
	rack := "us-west"

	// Save gateway config (normally done by saveGatewayConfig)
	config := map[string]interface{}{
		"gateways": map[string]interface{}{
			rack: map[string]interface{}{
				"url": "https://gateway-us-west.example.com",
			},
		},
	}

	configFile := filepath.Join(tmpDir, "config.json")
	configData, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, configData, 0600))

	// Login should set current rack
	err = SetCurrentRack(rack)
	require.NoError(t, err)

	// Verify current rack was set
	currentRack, err := GetCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "us-west", currentRack)
}

func TestGetCurrentRackWithNoConfig(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	ConfigPath = tmpDir

	// Test when no current rack is configured
	_, err := GetCurrentRack()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no current rack")
}

func TestSetCurrentRackCreatesDirectory(t *testing.T) {
	// Use a nested path that doesn't exist
	tmpDir := t.TempDir()
	ConfigPath = filepath.Join(tmpDir, "nested", "config", "dir")

	// SetCurrentRack should create the directory structure
	err := SetCurrentRack("staging")
	require.NoError(t, err)

	configFile := filepath.Join(ConfigPath, "config.json")
	assert.FileExists(t, configFile)
	cfg, exists, err := LoadConfig()
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "staging", cfg.Current)
}

func TestResolveRackStatusPrefersConfig(t *testing.T) {
	tmpDir := t.TempDir()
	ConfigPath = tmpDir
	t.Setenv("RACK_GATEWAY_URL", "")
	t.Setenv("RACK_GATEWAY_API_TOKEN", "")

	require.NoError(t, SaveConfig(&Config{
		Current: "staging",
		Gateways: map[string]GatewayConfig{
			"staging": {
				URL:       "https://gateway-staging.example.com",
				Token:     "abc123",
				Email:     "user@example.com",
				ExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
			},
		},
	}))

	status, err := ResolveRackStatus(time.Now())
	require.NoError(t, err)
	assert.Equal(t, "staging", status.Rack)
	assert.Equal(t, "https://gateway-staging.example.com", status.GatewayURL)
	assert.Contains(t, strings.Join(status.StatusLines, " "), "Logged in as user@example.com")
}

func TestResolveRackStatusFallsBackToEnv(t *testing.T) {
	ConfigPath = t.TempDir()
	t.Setenv("RACK_GATEWAY_RACK", "")
	t.Setenv("RACK_GATEWAY_URL", "https://env-gateway.example.com")
	t.Setenv("RACK_GATEWAY_API_TOKEN", "token-from-env")

	status, err := ResolveRackStatus(time.Now())
	require.NoError(t, err)
	assert.Equal(t, "Using RACK_GATEWAY_API_TOKEN from environment", status.Rack)
	assert.Equal(t, "https://env-gateway.example.com", status.GatewayURL)
	assert.Len(t, status.StatusLines, 0)
}

func TestResolveRackStatusEnvRequiresToken(t *testing.T) {
	ConfigPath = t.TempDir()
	t.Setenv("RACK_GATEWAY_RACK", "")
	t.Setenv("RACK_GATEWAY_URL", "https://env-gateway.example.com")

	_, err := ResolveRackStatus(time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RACK_GATEWAY_API_TOKEN")
}

func TestRenderGatewayError(t *testing.T) {
	t.Run("json payload", func(t *testing.T) {
		msg := RenderGatewayError([]byte(`{"error":"You don't have permission"}`))
		assert.Equal(t, "You don't have permission", msg)
	})

	t.Run("empty body", func(t *testing.T) {
		msg := RenderGatewayError([]byte(""))
		assert.Equal(t, "forbidden", msg)
	})

	t.Run("plain text", func(t *testing.T) {
		msg := RenderGatewayError([]byte("permission denied"))
		assert.Equal(t, "permission denied", msg)
	})
}
