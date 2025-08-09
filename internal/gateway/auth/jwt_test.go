package auth

import (
	"testing"
	"time"
)

func TestJWTManager(t *testing.T) {
	secret := "test-secret-key-for-testing"
	expiry := 1 * time.Hour
	manager := NewJWTManager(secret, expiry)

	t.Run("create and validate token", func(t *testing.T) {
		email := "test@example.com"
		name := "Test User"

		token, err := manager.CreateToken(email, name)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		if token == "" {
			t.Error("Expected non-empty token")
		}

		claims, err := manager.ValidateToken(token)
		if err != nil {
			t.Fatalf("Failed to validate token: %v", err)
		}

		if claims.Email != email {
			t.Errorf("Expected email %s, got %s", email, claims.Email)
		}

		if claims.Name != name {
			t.Errorf("Expected name %s, got %s", name, claims.Name)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := manager.ValidateToken("invalid.token.here")
		if err == nil {
			t.Error("Expected error for invalid token")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		shortManager := NewJWTManager(secret, 1*time.Nanosecond)

		token, err := shortManager.CreateToken("test@example.com", "Test User")
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		_, err = shortManager.ValidateToken(token)
		if err == nil {
			t.Error("Expected error for expired token")
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		token, err := manager.CreateToken("test@example.com", "Test User")
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		wrongManager := NewJWTManager("wrong-secret", expiry)
		_, err = wrongManager.ValidateToken(token)
		if err == nil {
			t.Error("Expected error for token with wrong secret")
		}
	})
}
