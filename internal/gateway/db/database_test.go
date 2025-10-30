package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestDatabase(t *testing.T) {
	db, err := gwdb.NewFromEnv()
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck // test cleanup
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
			Action:         audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionRead.String()),
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
			Action:         audit.BuildAction(rbac.ResourceUser.String(), rbac.ActionCreate.String()),
			Resource:       "newuser@example.com",
			Status:         "success",
			ResponseTimeMs: 45,
		}

		log3 := &gwdb.AuditLog{
			UserEmail:      "attacker@evil.com",
			ActionType:     "auth",
			Action:         audit.BuildAction(audit.ActionScopeLogin, audit.ActionVerbOAuthFailed),
			Status:         "failed",
			ResponseTimeMs: 5,
		}

		err = db.CreateAuditLog(log1)
		require.NoError(t, err, "Failed to create log1: %v", err)

		// Verify the first log was created by counting
		var count int
		err = db.DB().QueryRow("SELECT COUNT(*) FROM audit.audit_event").Scan(&count)
		require.NoError(t, err)
		t.Logf("After log1: %d audit logs in database", count)

		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		err = db.CreateAuditLog(log2)
		require.NoError(t, err, "Failed to create log2: %v", err)

		time.Sleep(10 * time.Millisecond)
		err = db.CreateAuditLog(log3)
		require.NoError(t, err, "Failed to create log3: %v", err)

		// Count total logs before query
		err = db.DB().QueryRow("SELECT COUNT(*) FROM audit.audit_event").Scan(&count)
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
		assert.Equal(t, audit.BuildAction(rbac.ResourceEnv.String(), rbac.ActionRead.String()), userLogs[0].Action)
		assert.Equal(t, "192.168.1.1", userLogs[0].IPAddress)

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

	t.Run("AuditAggregation", func(t *testing.T) {
		dbtest.Reset(t, db)

		base := &gwdb.AuditLog{
			UserEmail:      "poller@example.com",
			UserName:       "Poller",
			ActionType:     "convox",
			Action:         audit.BuildAction(rbac.ResourceProcess.String(), rbac.ActionRead.String()),
			Resource:       "app-123/process-abc",
			ResourceType:   "process",
			Details:        `{"path":"/apps/app-123/processes/process-abc","request_id":"req-001"}`,
			IPAddress:      "10.0.0.1",
			UserAgent:      "convox-cli/3.0",
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     200,
			ResponseTimeMs: 95,
		}
		require.NoError(t, db.CreateAuditLog(base))

		follow := &gwdb.AuditLog{
			UserEmail:      base.UserEmail,
			UserName:       base.UserName,
			ActionType:     base.ActionType,
			Action:         base.Action,
			Resource:       base.Resource,
			ResourceType:   base.ResourceType,
			Details:        `{"path":"/apps/app-123/processes/process-abc","request_id":"req-002"}`,
			IPAddress:      base.IPAddress,
			UserAgent:      base.UserAgent,
			Status:         base.Status,
			RBACDecision:   base.RBACDecision,
			HTTPStatus:     base.HTTPStatus,
			ResponseTimeMs: 128,
		}
		require.NoError(t, db.CreateAuditLog(follow))

		// Aggregated view should combine events with same fields (different request_id)
		aggs, total, err := db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
			UserEmail: base.UserEmail,
			Limit:     50,
		})
		require.NoError(t, err)
		require.Equal(t, 1, total, "should have 1 aggregated entry")
		require.Len(t, aggs, 1)
		assert.Equal(t, 2, aggs[0].EventCount, "event count should be 2")
		assert.Equal(t, 95, aggs[0].MinResponseTimeMs)
		assert.Equal(t, 128, aggs[0].MaxResponseTimeMs)
		assert.Contains(t, aggs[0].Details, "/apps/app-123/processes/process-abc", "details should contain path")

		// Create event with different resource (should NOT aggregate)
		different := &gwdb.AuditLog{
			UserEmail:      base.UserEmail,
			UserName:       base.UserName,
			ActionType:     base.ActionType,
			Action:         base.Action,
			Resource:       "app-123/process-def", // Different resource
			ResourceType:   base.ResourceType,
			Details:        `{"path":"/apps/app-123/processes/process-def","request_id":"req-003"}`,
			IPAddress:      base.IPAddress,
			UserAgent:      base.UserAgent,
			Status:         base.Status,
			RBACDecision:   base.RBACDecision,
			HTTPStatus:     base.HTTPStatus,
			ResponseTimeMs: 143,
		}
		require.NoError(t, db.CreateAuditLog(different))

		// Should now have 2 aggregated entries
		all, total, err := db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
			UserEmail: base.UserEmail,
			Limit:     50,
		})
		require.NoError(t, err)
		require.Equal(t, 2, total)
		require.Len(t, all, 2)
		// Ordered by last_seen DESC, so newest first
		assert.Equal(t, 1, all[0].EventCount, "different resource should have event count 1")
		assert.Equal(t, 2, all[1].EventCount, "aggregated events should have event count 2")
	})

	t.Run("AuditAggregationNoTimeWindow", func(t *testing.T) {
		dbtest.Reset(t, db)

		base := time.Now().UTC()

		// Create first audit log with old timestamp
		first := &gwdb.AuditLog{
			Timestamp:      base.Add(-15 * time.Second),
			UserEmail:      "test@example.com",
			ActionType:     "convox",
			Action:         audit.BuildAction(rbac.ResourceApp.String(), rbac.ActionList.String()),
			Resource:       "all",
			ResourceType:   "app",
			Status:         "success",
			ResponseTimeMs: 100,
		}
		require.NoError(t, db.CreateAuditLog(first))

		// Create second audit log with current timestamp (same fields, different timestamp)
		second := &gwdb.AuditLog{
			Timestamp:      base,
			UserEmail:      "test@example.com",
			ActionType:     "convox",
			Action:         audit.BuildAction(rbac.ResourceApp.String(), rbac.ActionList.String()),
			Resource:       "all",
			ResourceType:   "app",
			Status:         "success",
			ResponseTimeMs: 105,
		}
		require.NoError(t, db.CreateAuditLog(second))

		// Raw table should have 2 separate entries
		logs, err := db.GetAuditLogs(first.UserEmail, time.Time{}, 0)
		require.NoError(t, err)
		require.Len(t, logs, 2, "raw audit_event table should have 2 entries")
		assert.Equal(t, 1, logs[0].EventCount)
		assert.Equal(t, 1, logs[1].EventCount)

		// Aggregated table should combine them (no time window restriction)
		aggs, total, err := db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
			UserEmail: first.UserEmail,
			Limit:     50,
		})
		require.NoError(t, err)
		require.Equal(t, 1, total, "aggregated table should combine events regardless of timestamp")
		require.Len(t, aggs, 1)
		assert.Equal(t, 2, aggs[0].EventCount)
	})
}

func TestGetAuditLogsPaged(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck // test cleanup
	dbtest.Reset(t, db)

	base := time.Now().UTC()

	logs := []*gwdb.AuditLog{
		{
			Timestamp:      base.Add(-48 * time.Hour),
			UserEmail:      "admin@example.com",
			UserName:       "Admin User",
			ActionType:     "convox",
			Action:         audit.BuildAction(rbac.ResourceApp.String(), rbac.ActionList.String()),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     200,
			ResponseTimeMs: 10,
		},
		{
			Timestamp:      base.Add(-2 * time.Hour),
			UserEmail:      "viewer@example.com",
			UserName:       "Viewer User",
			ActionType:     "tokens",
			Action:         audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionCreate.String()),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     201,
			Resource:       "token123",
			Details:        `{"name":"Example"}`,
			ResponseTimeMs: 20,
		},
		{
			Timestamp:      base.Add(-1 * time.Hour),
			UserEmail:      "viewer@example.com",
			UserName:       "Viewer User",
			ActionType:     "tokens",
			Action:         audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionDelete.String()),
			Status:         "success",
			RBACDecision:   "allow",
			HTTPStatus:     200,
			Resource:       "token123",
			Details:        `{"name":"Example"}`,
			ResponseTimeMs: 15,
		},
	}

	for _, log := range logs {
		require.NoError(t, db.CreateAuditLog(log))
	}

	filters := gwdb.AuditLogFilters{
		Status:     "success",
		ActionType: "tokens",
		Since:      base.Add(-3 * time.Hour),
		Limit:      50,
	}
	paged, total, err := db.GetAuditLogsPaged(filters)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, paged, 2)

	filters.Search = "Example"
	_, total, err = db.GetAuditLogsPaged(filters)
	require.NoError(t, err)
	assert.Equal(t, 2, total)

	filters.Search = "Nonexistent"
	paged, total, err = db.GetAuditLogsPaged(filters)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Len(t, paged, 0)

	timeFiltered, timeTotal, err := db.GetAuditLogsPaged(gwdb.AuditLogFilters{
		Since: base.Add(-3 * time.Hour),
		Until: base.Add(-90 * time.Minute),
		Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, timeTotal)
	require.Len(t, timeFiltered, 1)
	assert.Equal(t, audit.BuildAction(rbac.ResourceAPIToken.String(), rbac.ActionCreate.String()), timeFiltered[0].Action)
}

func TestGetAuditLogByIDHandlesNulls(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck // test cleanup
	dbtest.Reset(t, db)

	// Create a minimal audit log with several nullable fields omitted
	log := &gwdb.AuditLog{
		UserEmail:      "nulls@example.com",
		ActionType:     "convox",
		Action:         audit.BuildAction(rbac.ResourceApp.String(), rbac.ActionList.String()),
		Status:         "success",
		ResponseTimeMs: 5,
		// Intentionally leave: UserName, Command, Resource, ResourceType, Details,
		// RequestID, IPAddress, UserAgent, RBACDecision, HTTPStatus unset (zero values)
	}
	require.NoError(t, db.CreateAuditLog(log))

	// Fetch by ID and ensure scan succeeds with defaults rather than NULL errors
	fetched, err := db.GetAuditLogByID(log.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, log.ID, fetched.ID)
	assert.Equal(t, "nulls@example.com", fetched.UserEmail)
	// Fields should be empty strings rather than causing scan errors
	assert.Equal(t, "", fetched.UserName)
	assert.Equal(t, "", fetched.Command)
	assert.Equal(t, "", fetched.Resource)
	assert.Equal(t, "", fetched.ResourceType)
	assert.Equal(t, "", fetched.Details)
	assert.Equal(t, "", fetched.RequestID)
	assert.Equal(t, "", fetched.IPAddress)
	assert.Equal(t, "", fetched.UserAgent)
	assert.Equal(t, "", fetched.RBACDecision)
	// HTTPStatus should default to 0 when NULL
	assert.Equal(t, 0, fetched.HTTPStatus)
}

func TestCreateAuditLogHandlesNullThenInet(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck // test cleanup
	dbtest.Reset(t, db)

	initial := &gwdb.AuditLog{
		UserEmail:      "sequence@example.com",
		ActionType:     "auth",
		Action:         audit.BuildAction(audit.ActionScopeLogin, rbac.ActionStart.String()),
		Status:         "success",
		ResponseTimeMs: 1,
		IPAddress:      "",
	}
	require.NoError(t, db.CreateAuditLog(initial))

	withIP := &gwdb.AuditLog{
		UserEmail:      "sequence@example.com",
		ActionType:     "auth",
		Action:         audit.BuildAction(audit.ActionScopeLogin, audit.ActionVerbComplete),
		Status:         "success",
		ResponseTimeMs: 2,
		IPAddress:      "203.0.113.10",
	}

	err := db.CreateAuditLog(withIP)
	require.NoError(t, err, "expected postgres inet column to accept value after NULL initialization")

	var stored string
	require.NoError(t, db.DB().QueryRow(`SELECT host(ip_address) FROM audit.audit_event WHERE action = 'login.complete' AND user_email = 'sequence@example.com'`).Scan(&stored))
	assert.Equal(t, "203.0.113.10", stored)
}

func TestListUsersIncludesCreatorMetadata(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck // test cleanup
	dbtest.Reset(t, db)

	creator, err := db.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	user, err := db.CreateUser("viewer@example.com", "Viewer", []string{"viewer"})
	require.NoError(t, err)
	created, err := db.CreateUserResource(creator.ID, "user", user.Email)
	require.NoError(t, err)
	assert.True(t, created)

	users, err := db.ListUsers()
	require.NoError(t, err)
	var found *gwdb.User
	for _, u := range users {
		if u.Email == user.Email {
			found = u
			break
		}
	}
	require.NotNil(t, found)
	assert.NotNil(t, found.CreatedByUserID)
	assert.Equal(t, creator.Email, found.CreatedByEmail)
	assert.Equal(t, creator.Name, found.CreatedByName)
}

func TestAuditLogIndexes(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck // test cleanup
	dbtest.Reset(t, db)

	indexes := []string{
		"audit.idx_audit_event_status",
		"audit.idx_audit_event_resource_type",
		"audit.idx_audit_event_user_timestamp",
		"audit.idx_audit_event_status_action_resource_ts",
	}

	for _, idx := range indexes {
		var exists bool
		err := db.DB().QueryRow(`SELECT to_regclass($1) IS NOT NULL`, idx).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "expected index %s to exist", idx)
	}
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
		// Close the first connection before creating another instance (connection pool reuse)
		//nolint:errcheck // closing during cleanup; failure handled in subsequent operations
		db1.Close()

		db2, err := gwdb.NewFromEnv()
		require.NoError(t, err)
		defer db2.Close() //nolint:errcheck // test cleanup
		users, err := db2.ListUsers()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(users), 2)
	})
}
