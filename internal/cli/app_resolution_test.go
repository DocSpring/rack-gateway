package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	return func() { _ = os.Chdir(cwd) }
}

func TestResolveApp_FlagPrecedence(t *testing.T) {
	app, err := ResolveApp("from-flag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app != "from-flag" {
		t.Fatalf("want from-flag, got %s", app)
	}
}

func TestResolveApp_EnvVar(t *testing.T) {
	t.Setenv("CONVOX_APP", "from-env")
	app, err := ResolveApp("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app != "from-env" {
		t.Fatalf("want from-env, got %s", app)
	}
}

func TestResolveApp_DotConvoxFile_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".convox"), 0o750); err != nil {
		t.Fatalf("mkdir .convox: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".convox", "app"), []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write app file: %v", err)
	}
	back := chdir(t, dir)
	defer back()

	// Clear env to ensure file is used
	t.Setenv("CONVOX_APP", "")

	app, err := ResolveApp("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app != "from-file" {
		t.Fatalf("want from-file, got %s", app)
	}
}

func TestResolveApp_DotConvoxFile_ParentDir(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".convox"), 0o750); err != nil {
		t.Fatalf("mkdir .convox: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".convox", "app"), []byte("from-parent\n"), 0o600); err != nil {
		t.Fatalf("write app file: %v", err)
	}
	child := filepath.Join(parent, "child", "deeper")
	if err := os.MkdirAll(child, 0o750); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	back := chdir(t, child)
	defer back()

	t.Setenv("CONVOX_APP", "")

	app, err := ResolveApp("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app != "from-parent" {
		t.Fatalf("want from-parent, got %s", app)
	}
}

func TestResolveApp_DotConvoxFile_WithHyphens(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".convox"), 0o750); err != nil {
		t.Fatalf("mkdir .convox: %v", err)
	}
	// Test that hyphenated app names are preserved exactly
	if err := os.WriteFile(filepath.Join(dir, ".convox", "app"), []byte("api-proxy\n"), 0o600); err != nil {
		t.Fatalf("write app file: %v", err)
	}
	back := chdir(t, dir)
	defer back()

	t.Setenv("CONVOX_APP", "")

	app, err := ResolveApp("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app != "api-proxy" {
		t.Fatalf("want api-proxy, got %s (hyphen should be preserved, not converted to underscore)", app)
	}
}

func TestResolveAppFlag_IntegrationWithDotConvoxFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".convox"), 0o750); err != nil {
		t.Fatalf("mkdir .convox: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".convox", "app"), []byte("api-proxy\n"), 0o600); err != nil {
		t.Fatalf("write app file: %v", err)
	}
	back := chdir(t, dir)
	defer back()

	t.Setenv("CONVOX_APP", "")

	// Create a mock command with an app flag (like deploy command)
	cmd := DeployCommand()

	// Call resolveAppFlag like the deploy command does
	if err := resolveAppFlag(cmd); err != nil {
		t.Fatalf("resolveAppFlag failed: %v", err)
	}

	// Verify the app flag was set correctly
	appFlag := cmd.Flags().Lookup("app")
	if appFlag == nil {
		t.Fatal("app flag not found")
	}

	if !appFlag.Changed {
		t.Fatal("app flag should be marked as Changed after resolveAppFlag, but it wasn't")
	}

	appValue, err := cmd.Flags().GetString("app")
	if err != nil {
		t.Fatalf("failed to get app flag value: %v", err)
	}

	if appValue != "api-proxy" {
		t.Fatalf("want app flag value 'api-proxy', got '%s'", appValue)
	}
}

func TestResolveApp_FallbackBasename(t *testing.T) {
	// Create a temp dir with a stable base name
	base := "cg-app-base"
	parent := t.TempDir()
	dir := filepath.Join(parent, base)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	back := chdir(t, dir)
	defer back()

	t.Setenv("CONVOX_APP", "")

	app, err := ResolveApp("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// On Windows, temp paths may include volume info; basename should still be base
	if app != base {
		t.Fatalf("want %s, got %s (GOOS=%s)", base, app, runtime.GOOS)
	}
}
