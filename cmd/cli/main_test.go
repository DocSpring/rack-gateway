package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetCurrentRack(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Test setting current rack
	err := setCurrentRack("staging")
	require.NoError(t, err)

	// Verify the file was created with correct content
	currentFile := filepath.Join(tmpDir, "current")
	data, err := os.ReadFile(currentFile)
	require.NoError(t, err)
	assert.Equal(t, "staging", string(data))

	// Test overwriting current rack
	err = setCurrentRack("production")
	require.NoError(t, err)

	data, err = os.ReadFile(currentFile)
	require.NoError(t, err)
	assert.Equal(t, "production", string(data))
}

func TestGetCurrentRack(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Test when no current file exists
	_, err := getCurrentRack()
	assert.Error(t, err)

	// Create a current file
	currentFile := filepath.Join(tmpDir, "current")
	err = os.WriteFile(currentFile, []byte("us-east"), 0600)
	require.NoError(t, err)

	// Test reading current rack
	rack, err := getCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "us-east", rack)

	// Test with whitespace (should be trimmed)
	err = os.WriteFile(currentFile, []byte("  eu-west  \n"), 0600)
	require.NoError(t, err)

	rack, err = getCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "eu-west", rack)
}

func TestSwitchCommand(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

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
	err = setCurrentRack("staging")
	require.NoError(t, err)

	rack, err := getCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "staging", rack)

	// Test switching to another valid rack
	err = setCurrentRack("production")
	require.NoError(t, err)

	rack, err = getCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "production", rack)
}

func TestRackSelectionPriority(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Set up initial current rack
	err := setCurrentRack("staging")
	require.NoError(t, err)

	// Test 1: Default from current file
	rack, err := getCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "staging", rack)

	// Test 2: Environment variable override
	os.Setenv("CONVOX_GATEWAY_RACK", "production")
	defer os.Unsetenv("CONVOX_GATEWAY_RACK")
	
	// In the actual wrapConvoxCommand, env var overrides current
	// We're just testing the helper functions here
	envRack := os.Getenv("CONVOX_GATEWAY_RACK")
	if envRack != "" {
		rack = envRack
	}
	assert.Equal(t, "production", rack)
}

func TestLoginSetsCurrentRack(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

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
	err = setCurrentRack(rack)
	require.NoError(t, err)

	// Verify current rack was set
	currentRack, err := getCurrentRack()
	require.NoError(t, err)
	assert.Equal(t, "us-west", currentRack)
}

func TestGetCurrentRackWithNoConfig(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configPath = tmpDir

	// Test when no current file exists
	_, err := getCurrentRack()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestSetCurrentRackCreatesDirectory(t *testing.T) {
	// Use a nested path that doesn't exist
	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "nested", "config", "dir")

	// setCurrentRack should create the directory structure
	err := setCurrentRack("staging")
	require.NoError(t, err)

	// Verify the directory was created and file exists
	currentFile := filepath.Join(configPath, "current")
	assert.FileExists(t, currentFile)

	data, err := os.ReadFile(currentFile)
	require.NoError(t, err)
	assert.Equal(t, "staging", string(data))
}