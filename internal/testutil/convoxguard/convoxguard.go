package convoxguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Preferences", "convox"), nil
	case "linux":
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		return filepath.Join(xdg, "convox"), nil
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA not set")
		}
		return filepath.Join(base, "convox"), nil
	default:
		return "", fmt.Errorf("unsupported GOOS=%s", runtime.GOOS)
	}
}

// Setup enforces that tests must be run through safe-test.sh wrapper
// The wrapper handles all backup/restore, we just verify it's being used
func Setup() (func() error, error) {
	// Skip all protection in CI environments
	if os.Getenv("CI") != "" {
		return func() error { return nil }, nil
	}
	
	// ENFORCE: Tests MUST be run through safe-test.sh wrapper
	if os.Getenv("CONVOX_GATEWAY_SAFE_TEST") != "1" {
		panic(`
CRITICAL SAFETY VIOLATION: Tests must be run through the safe wrapper script!

You attempted to run tests directly with 'go test' which could destroy your production Convox configuration.

ALWAYS use one of these commands:
  make test              - Run all tests safely
  make test-unit         - Run unit tests safely  
  make test-integration  - Run integration tests safely
  ./scripts/safe-test.sh - Run tests with custom flags

DO NOT run 'go test' directly!
`)
	}
	
	// Verify the wrapper has set up the test environment correctly
	cfg, err := configDir()
	if err != nil {
		return nil, err
	}
	
	guardFile := filepath.Join(cfg, "GUARD_ACTIVE")
	if _, err := os.Stat(guardFile); err != nil {
		return nil, fmt.Errorf("CRITICAL: Guard file not found at %s - safe wrapper may have failed", guardFile)
	}
	
	// No cleanup needed - wrapper handles everything
	return func() error { return nil }, nil
}