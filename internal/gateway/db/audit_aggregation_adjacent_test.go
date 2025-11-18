package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// TestAuditAggregationOnlyAdjacentEvents verifies that aggregation ONLY happens
// for adjacent/consecutive events, not all matching events across the log.
func TestAuditAggregationOnlyAdjacentEvents(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck // test cleanup
	dbtest.Reset(t, db)

	// Create a login.start event
	login1 := &gwdb.AuditLog{
		UserEmail:      "user@example.com",
		UserName:       "Test User",
		ActionType:     "auth",
		Action:         "login.start",
		Status:         "success",
		ResponseTimeMs: 50,
	}
	require.NoError(t, db.CreateAuditLog(login1))

	// Create ANOTHER login.start event immediately after (should aggregate)
	login2 := &gwdb.AuditLog{
		UserEmail:      "user@example.com",
		UserName:       "Test User",
		ActionType:     "auth",
		Action:         "login.start",
		Status:         "success",
		ResponseTimeMs: 55,
	}
	require.NoError(t, db.CreateAuditLog(login2))

	// Create a DIFFERENT event (login.complete) that breaks the sequence
	loginComplete := &gwdb.AuditLog{
		UserEmail:      "user@example.com",
		UserName:       "Test User",
		ActionType:     "auth",
		Action:         "login.complete",
		Status:         "success",
		ResponseTimeMs: 100,
	}
	require.NoError(t, db.CreateAuditLog(loginComplete))

	// Create ANOTHER login.start event (should NOT aggregate with the first two)
	login3 := &gwdb.AuditLog{
		UserEmail:      "user@example.com",
		UserName:       "Test User",
		ActionType:     "auth",
		Action:         "login.start",
		Status:         "success",
		ResponseTimeMs: 52,
	}
	require.NoError(t, db.CreateAuditLog(login3))

	// Get aggregated results
	aggs, total, err := db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
		UserEmail: "user@example.com",
		Limit:     50,
	})
	require.NoError(t, err)

	// Should have 3 aggregated entries:
	// 1. login3 (single event)
	// 2. loginComplete (single event)
	// 3. login1+login2 (aggregated - these were adjacent)
	require.Equal(t, 3, total, "should have 3 aggregated entries")
	require.Len(t, aggs, 3)

	// Results are ordered by last_seen DESC (newest first)
	assert.Equal(t, "login.start", aggs[0].Action, "newest should be login3")
	assert.Equal(t, 1, aggs[0].EventCount, "login3 should be standalone")

	assert.Equal(t, "login.complete", aggs[1].Action, "middle should be loginComplete")
	assert.Equal(t, 1, aggs[1].EventCount, "loginComplete should be standalone")

	assert.Equal(t, "login.start", aggs[2].Action, "oldest should be login1+login2")
	assert.Equal(t, 2, aggs[2].EventCount, "login1 and login2 were adjacent and should aggregate")
}

// TestAuditAggregationDifferentActionsBreakSequence verifies that different
// actions create separate aggregated rows even if they're the same action type.
func TestAuditAggregationDifferentActionsBreakSequence(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck // test cleanup
	dbtest.Reset(t, db)

	// Create: rack.read, rack.read, build.create, rack.read
	// Expected aggregation: [rack.read (×1)], [build.create (×1)], [rack.read (×2)]

	read1 := &gwdb.AuditLog{
		UserEmail:      "dev@example.com",
		ActionType:     "convox",
		Action:         audit.BuildAction(rbac.ResourceRack.String(), rbac.ActionRead.String()),
		Resource:       "rack",
		ResourceType:   "rack",
		Status:         "success",
		ResponseTimeMs: 10,
	}
	require.NoError(t, db.CreateAuditLog(read1))

	read2 := &gwdb.AuditLog{
		UserEmail:      "dev@example.com",
		ActionType:     "convox",
		Action:         audit.BuildAction(rbac.ResourceRack.String(), rbac.ActionRead.String()),
		Resource:       "rack",
		ResourceType:   "rack",
		Status:         "success",
		ResponseTimeMs: 12,
	}
	require.NoError(t, db.CreateAuditLog(read2))

	build := &gwdb.AuditLog{
		UserEmail:      "dev@example.com",
		ActionType:     "convox",
		Action:         audit.BuildAction(rbac.ResourceBuild.String(), rbac.ActionCreate.String()),
		Resource:       "BABC123",
		ResourceType:   "build",
		Status:         "success",
		ResponseTimeMs: 250,
	}
	require.NoError(t, db.CreateAuditLog(build))

	read3 := &gwdb.AuditLog{
		UserEmail:      "dev@example.com",
		ActionType:     "convox",
		Action:         audit.BuildAction(rbac.ResourceRack.String(), rbac.ActionRead.String()),
		Resource:       "rack",
		ResourceType:   "rack",
		Status:         "success",
		ResponseTimeMs: 11,
	}
	require.NoError(t, db.CreateAuditLog(read3))

	aggs, total, err := db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
		UserEmail: "dev@example.com",
		Limit:     50,
	})
	require.NoError(t, err)

	// Should have 3 aggregated entries (newest first):
	// 1. read3 (single)
	// 2. build (single)
	// 3. read1+read2 (aggregated - adjacent)
	require.Equal(t, 3, total)
	require.Len(t, aggs, 3)

	assert.Equal(t, "rack.read", aggs[0].Action)
	assert.Equal(t, 1, aggs[0].EventCount, "read3 is standalone")

	assert.Equal(t, "build.create", aggs[1].Action)
	assert.Equal(t, 1, aggs[1].EventCount, "build is standalone")

	assert.Equal(t, "rack.read", aggs[2].Action)
	assert.Equal(t, 2, aggs[2].EventCount, "read1+read2 were adjacent")
}
