package db_test

import (
	"testing"
	"time"

	gwdb "github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/testutil/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabase(t *testing.T) {
	db, err := gwdb.NewFromEnv()
	require.NoError(t, err)
	defer db.Close()
	dbtest.Reset(t, db)

	t.Run("InitializeAdmin", func(t *testing.T) {
		// First initialization should succeed
		err := db.InitializeAdmin("admin@example.com", "Admin User")
		assert.NoError(t, err)

		// Second initialization should be a no-op (users exist)
		err = db.InitializeAdmin("other@example.com", "Other User")
		assert.NoError(t, err)

		// Admin user should exist
		user, err := db.GetUser("admin@example.com")
		require.NoError(t, err)
		assert.Equal(t, "admin@example.com", user.Email)
		assert.Equal(t, "Admin User", user.Name)
		assert.Equal(t, []string{"admin"}, user.Roles)

		// Other user should not exist
		user, err = db.GetUser("other@example.com")
		assert.NoError(t, err)
		assert.Nil(t, user)
	})

	t.Run("UserCRUD", func(t *testing.T) {
		// Create user
		user, err := db.CreateUser("test@example.com", "Test User", []string{"viewer", "ops"})
		require.NoError(t, err)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Equal(t, []string{"viewer", "ops"}, user.Roles)

		// Get user
		retrieved, err := db.GetUser("test@example.com")
		require.NoError(t, err)
		assert.Equal(t, user.Email, retrieved.Email)
		assert.Equal(t, user.Roles, retrieved.Roles)

		// Update roles
		err = db.UpdateUserRoles("test@example.com", []string{"admin"})
		require.NoError(t, err)

		// Verify update
		updated, err := db.GetUser("test@example.com")
		require.NoError(t, err)
		assert.Equal(t, []string{"admin"}, updated.Roles)

		// List users
		users, err := db.ListUsers()
		require.NoError(t, err)
		assert.Len(t, users, 2) // admin + test user

		// Delete user
		err = db.DeleteUser("test@example.com")
		require.NoError(t, err)

		// Verify deletion
		deleted, err := db.GetUser("test@example.com")
		assert.NoError(t, err)
		assert.Nil(t, deleted)
	})

	t.Run("AuditLogs", func(t *testing.T) {
		// Create audit logs
		log1 := &gwdb.AuditLog{
			UserEmail:      "user1@example.com",
			UserName:       "User One",
			ActionType:     "convox",
			Action:         "env.get",
			Resource:       "myapp",
			Details:        `{"key": "SECRET_TOKEN"}`,
			IPAddress:      "192.168.1.1",
			UserAgent:      "convox-cli/3.0",
			Status:         "success",
			ResponseTimeMs: 123,
		}

		log2 := &gwdb.AuditLog{
			UserEmail:      "user2@example.com",
			UserName:       "User Two",
			ActionType:     "users",
			Action:         "user.create",
			Resource:       "newuser@example.com",
			Status:         "success",
			ResponseTimeMs: 45,
		}

		log3 := &gwdb.AuditLog{
			UserEmail:      "attacker@evil.com",
			ActionType:     "auth",
			Action:         "auth.failed",
			Status:         "blocked",
			ResponseTimeMs: 5,
		}

		err = db.CreateAuditLog(log1)
		require.NoError(t, err, "Failed to create log1: %v", err)

		// Verify the first log was created by counting
		var count int
		err = db.DB().QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&count)
		require.NoError(t, err)
		t.Logf("After log1: %d audit logs in database", count)

		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		err = db.CreateAuditLog(log2)
		require.NoError(t, err, "Failed to create log2: %v", err)

		time.Sleep(10 * time.Millisecond)
		err = db.CreateAuditLog(log3)
		require.NoError(t, err, "Failed to create log3: %v", err)

		// Count total logs before query
		err = db.DB().QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&count)
		require.NoError(t, err)
		t.Logf("Total audit logs before GetAuditLogs: %d", count)

		// Get all logs
		logs, err := db.GetAuditLogs("", time.Time{}, 0)
		require.NoError(t, err, "Failed to get audit logs: %v", err)
		t.Logf("GetAuditLogs returned %d logs", len(logs))
		for i, log := range logs {
			t.Logf("Log %d: %+v", i, log)
		}
		require.Len(t, logs, 3, "Expected 3 audit logs but got %d", len(logs))

		// Filter by user
		userLogs, err := db.GetAuditLogs("user1@example.com", time.Time{}, 0)
		require.NoError(t, err)
		assert.Len(t, userLogs, 1)
		assert.Equal(t, "env.get", userLogs[0].Action)

		// Filter by time (use a time well before we created the logs, in UTC)
		startTime := time.Now().UTC().Add(-5 * time.Minute)
		recentLogs, err := db.GetAuditLogs("", startTime, 0)
		require.NoError(t, err)
		assert.Len(t, recentLogs, 3)

		// Test filtering with future time returns nothing
		futureLogs, err := db.GetAuditLogs("", time.Now().UTC().Add(1*time.Hour), 0)
		require.NoError(t, err)
		assert.Len(t, futureLogs, 0)

		// Limit results
		limitedLogs, err := db.GetAuditLogs("", time.Time{}, 2)
		require.NoError(t, err)
		assert.Len(t, limitedLogs, 2)
	})
}

func TestDatabaseInitialization(t *testing.T) {
	t.Run("ReinitializesCorrectly", func(t *testing.T) {
		db1, err := gwdb.NewFromEnv()
		require.NoError(t, err)
		dbtest.Reset(t, db1)

		err = db1.InitializeAdmin("admin@example.com", "Admin")
		require.NoError(t, err)
		_, err = db1.CreateUser("user@example.com", "User", []string{"viewer"})
		require.NoError(t, err)
		db1.Close()

		db2, err := gwdb.NewFromEnv()
		require.NoError(t, err)
		defer db2.Close()
		users, err := db2.ListUsers()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(users), 2)
	})
}
