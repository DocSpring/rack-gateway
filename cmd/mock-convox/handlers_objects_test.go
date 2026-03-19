package main

import (
	"path/filepath"
	"testing"
)

func TestValidateSafePath(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "storage")

	t.Run("allows descendants", func(t *testing.T) {
		childPath := filepath.Join(baseDir, "app", "tmp", "object.txt")
		if err := validateSafePath(baseDir, childPath); err != nil {
			t.Fatalf("expected descendant path to be allowed, got %v", err)
		}
	})

	t.Run("rejects prefix-based escape", func(t *testing.T) {
		escapedPath := filepath.Join(baseDir+"-evil", "object.txt")
		if err := validateSafePath(baseDir, escapedPath); err == nil {
			t.Fatal("expected prefix-based escape to be rejected")
		}
	})
}
