package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// TestAuditAggregationTimestamps verifies that aggregated logs have valid timestamps.
func TestAuditAggregationTimestamps(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck,gosec // G104: test cleanup
	dbtest.Reset(t, db)

	// Create a log entry
	log1 := &gwdb.AuditLog{
		UserEmail:      "test@example.com",
		ActionType:     "test",
		Action:         "timestamp.check",
		Status:         "success",
		ResponseTimeMs: 10,
	}
	// Ensure CreateAuditLog sets the timestamp
	require.NoError(t, db.CreateAuditLog(log1))
	assert.False(t, log1.Timestamp.IsZero())

	// Create another one to trigger aggregation
	log2 := &gwdb.AuditLog{
		UserEmail:      "test@example.com",
		ActionType:     "test",
		Action:         "timestamp.check",
		Status:         "success",
		ResponseTimeMs: 10,
	}
	require.NoError(t, db.CreateAuditLog(log2))

	// Get aggregated results
	aggs, total, err := db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
		UserEmail: "test@example.com",
	})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, aggs, 1)

	agg := aggs[0]
	assert.Equal(t, 2, agg.EventCount)

	// Verify timestamps
	assert.False(t, agg.FirstSeen.IsZero(), "FirstSeen should not be zero")
	assert.False(t, agg.LastSeen.IsZero(), "LastSeen should not be zero")

	// Check if they are roughly recent
	assert.WithinDuration(t, time.Now(), agg.FirstSeen, 5*time.Second)
	assert.WithinDuration(t, time.Now(), agg.LastSeen, 5*time.Second)
}
