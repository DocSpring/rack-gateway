package db_test

import (
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

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
