package convoxguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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

// errUnsafeRun means the tests were started with 'go test' rather than through the safe-test.sh wrapper.
var errUnsafeRun = errors.New("CRITICAL SAFETY VIOLATION: tests must be run through the safe wrapper script")

const unsafeRunHelp = `You attempted to run tests directly with 'go test',
which could destroy your production Convox configuration.

ALWAYS use one of these commands:
  task test              - Run all tests (web + go + e2e)
  task go:test           - Run Go tests safely
  ./scripts/safe-test.sh - Run tests with custom flags

DO NOT run 'go test' directly!`

// Run runs the package's tests (call it from TestMain) once it has checked they were started through the
// safe-test.sh wrapper, which backs up and restores the real Convox config around them. It returns the exit code.
func Run(m *testing.M) int {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, errUnsafeRun) {
			fmt.Fprintln(os.Stderr, unsafeRunHelp)
		}
		return 1
	}
	return m.Run()
}

func check() error {
	// Skip all protection in CI environments
	if os.Getenv("CI") != "" {
		return nil
	}
	// ENFORCE: Tests MUST be run through safe-test.sh wrapper
	if os.Getenv("GATEWAY_SAFE_TEST") != "1" {
		return errUnsafeRun
	}

	// Verify the wrapper has set up the test environment correctly
	cfg, err := configDir()
	if err != nil {
		return err
	}
	guardFile := filepath.Join(cfg, "GUARD_ACTIVE")
	if _, err := os.Stat(guardFile); err != nil {
		return fmt.Errorf("CRITICAL: Guard file not found at %s - safe wrapper may have failed", guardFile)
	}
	return nil
}
