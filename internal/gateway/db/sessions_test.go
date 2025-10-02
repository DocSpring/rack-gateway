package db_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestUpdateSessionMetadata(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	// Create test user
	user, err := database.CreateUser("session@example.com", "Session Test", []string{"ops"})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Create a session
	tokenHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" // 64 chars
	expiresAt := time.Now().Add(1 * time.Hour)
	initialMeta := map[string]interface{}{
		"initial_key": "initial_value",
	}

	session, err := database.CreateUserSession(
		user.ID,
		tokenHash,
		expiresAt,
		"web",
		"", // Empty device ID (not a UUID)
		"Test Device",
		"192.168.1.1",
		"Mozilla/5.0",
		initialMeta,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	t.Run("merges new metadata with existing", func(t *testing.T) {
		newMeta := map[string]interface{}{
			"new_key": "new_value",
			"number":  42,
		}

		err := database.UpdateSessionMetadata(session.ID, newMeta)
		if err != nil {
			t.Fatalf("failed to update metadata: %v", err)
		}

		// Retrieve and verify
		updated, err := database.GetSessionByID(session.ID)
		if err != nil {
			t.Fatalf("failed to get updated session: %v", err)
		}

		var meta map[string]interface{}
		if err := json.Unmarshal(updated.Metadata, &meta); err != nil {
			t.Fatalf("failed to unmarshal metadata: %v", err)
		}

		// Should have both initial and new keys
		if meta["initial_key"] != "initial_value" {
			t.Error("initial_key should be preserved")
		}
		if meta["new_key"] != "new_value" {
			t.Error("new_key should be added")
		}
		if meta["number"] != float64(42) { // JSON numbers unmarshal as float64
			t.Errorf("number should be 42, got %v", meta["number"])
		}
	})

	t.Run("overwrites existing keys", func(t *testing.T) {
		updateMeta := map[string]interface{}{
			"initial_key": "updated_value",
		}

		err := database.UpdateSessionMetadata(session.ID, updateMeta)
		if err != nil {
			t.Fatalf("failed to update metadata: %v", err)
		}

		updated, _ := database.GetSessionByID(session.ID)
		var meta map[string]interface{}
		_ = json.Unmarshal(updated.Metadata, &meta)

		if meta["initial_key"] != "updated_value" {
			t.Errorf("initial_key should be overwritten, got %v", meta["initial_key"])
		}
	})

	t.Run("handles nil metadata gracefully", func(t *testing.T) {
		err := database.UpdateSessionMetadata(session.ID, nil)
		if err != nil {
			t.Error("should not error on nil metadata")
		}
	})

	t.Run("handles empty metadata gracefully", func(t *testing.T) {
		err := database.UpdateSessionMetadata(session.ID, map[string]interface{}{})
		if err != nil {
			t.Error("should not error on empty metadata")
		}
	})

	t.Run("fails for non-existent session", func(t *testing.T) {
		err := database.UpdateSessionMetadata(99999, map[string]interface{}{"key": "value"})
		if err == nil {
			t.Error("expected error for non-existent session")
		}
	})
}

func TestGetSessionByID(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	user, _ := database.CreateUser("getsession@example.com", "Get Test", []string{"ops"})

	session, _ := database.CreateUserSession(
		user.ID,
		"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", // 64 chars
		time.Now().Add(1*time.Hour),
		"web",
		"",
		"",
		"",
		"",
		nil,
		nil,
	)

	t.Run("retrieves session by ID", func(t *testing.T) {
		retrieved, err := database.GetSessionByID(session.ID)
		if err != nil {
			t.Fatalf("failed to get session: %v", err)
		}

		if retrieved == nil {
			t.Fatal("expected session to be found")
		}

		if retrieved.ID != session.ID {
			t.Errorf("expected ID %d, got %d", session.ID, retrieved.ID)
		}

		expectedHash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
		if retrieved.TokenHash != expectedHash {
			t.Errorf("expected token hash '%s', got '%s'", expectedHash, retrieved.TokenHash)
		}
	})

	t.Run("returns nil for non-existent session", func(t *testing.T) {
		retrieved, err := database.GetSessionByID(99999)
		if err != nil {
			t.Fatalf("should not error, got: %v", err)
		}
		if retrieved != nil {
			t.Error("expected nil for non-existent session")
		}
	})
}

func TestSessionMetadataWithWebAuthn(t *testing.T) {
	t.Parallel()

	database := dbtest.NewDatabase(t)

	user, _ := database.CreateUser("webauthn-session@example.com", "WebAuthn Session Test", []string{"ops"})

	session, _ := database.CreateUserSession(
		user.ID,
		"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210", // 64 chars
		time.Now().Add(1*time.Hour),
		"web",
		"",
		"",
		"",
		"",
		nil,
		nil,
	)

	t.Run("stores and retrieves WebAuthn enrollment session", func(t *testing.T) {
		webauthnSession := map[string]interface{}{
			"webauthn_enrollment_session": `{"challenge":"abc123","user_id":"1"}`,
			"webauthn_enrollment_expires": time.Now().Add(5 * time.Minute).Unix(),
		}

		err := database.UpdateSessionMetadata(session.ID, webauthnSession)
		if err != nil {
			t.Fatalf("failed to store WebAuthn session: %v", err)
		}

		// Retrieve and verify
		retrieved, err := database.GetSessionByID(session.ID)
		if err != nil {
			t.Fatalf("failed to get session: %v", err)
		}
		if retrieved == nil {
			t.Fatal("session should exist")
		}
		var meta map[string]interface{}
		if err := json.Unmarshal(retrieved.Metadata, &meta); err != nil {
			t.Fatalf("failed to unmarshal metadata: %v", err)
		}

		sessionData, ok := meta["webauthn_enrollment_session"].(string)
		if !ok {
			t.Fatal("webauthn_enrollment_session should be a string")
		}

		if sessionData != `{"challenge":"abc123","user_id":"1"}` {
			t.Error("WebAuthn session data mismatch")
		}

		expiresFloat, ok := meta["webauthn_enrollment_expires"].(float64)
		if !ok {
			t.Fatal("webauthn_enrollment_expires should be a number")
		}

		if expiresFloat <= 0 {
			t.Error("WebAuthn expiration should be positive")
		}
	})

	t.Run("can overwrite WebAuthn session in metadata", func(t *testing.T) {
		// First store WebAuthn session
		webauthnSession := map[string]interface{}{
			"webauthn_enrollment_session": "data",
			"webauthn_enrollment_expires": 12345,
		}
		_ = database.UpdateSessionMetadata(session.ID, webauthnSession)

		// Verify it was stored
		retrieved, _ := database.GetSessionByID(session.ID)
		var meta map[string]interface{}
		_ = json.Unmarshal(retrieved.Metadata, &meta)

		if _, exists := meta["webauthn_enrollment_session"]; !exists {
			t.Error("webauthn_enrollment_session should exist")
		}

		// Now overwrite with different data
		newSession := map[string]interface{}{
			"webauthn_enrollment_session": "new_data",
			"webauthn_enrollment_expires": 67890,
		}
		err := database.UpdateSessionMetadata(session.ID, newSession)
		if err != nil {
			t.Fatalf("failed to update metadata: %v", err)
		}

		// Verify it was overwritten
		updated, _ := database.GetSessionByID(session.ID)
		var updatedMeta map[string]interface{}
		_ = json.Unmarshal(updated.Metadata, &updatedMeta)

		if updatedMeta["webauthn_enrollment_session"] != "new_data" {
			t.Error("webauthn_enrollment_session should be overwritten")
		}
		if updatedMeta["webauthn_enrollment_expires"] != float64(67890) {
			t.Error("webauthn_enrollment_expires should be overwritten")
		}
	})
}
