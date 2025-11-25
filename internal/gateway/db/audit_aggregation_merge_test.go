package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

// TestAuditAggregationMergeProcessExec verifies that process.exec.start and process.exec
// are merged into a single aggregated entry.
func TestAuditAggregationMergeProcessExec(t *testing.T) {
	db := dbtest.NewDatabase(t)
	defer db.Close() //nolint:errcheck,gosec // G104: test cleanup
	dbtest.Reset(t, db)

	// 1. Log process.exec.start (Visibility)
	startLog := &gwdb.AuditLog{
		UserEmail:      "dev@example.com",
		ActionType:     "convox",
		Action:         "process.exec.start",
		Resource:       "cmd-123",
		Command:        "rails console",
		IPAddress:      "1.2.3.4",
		UserAgent:      "test-agent",
		Status:         "success",
		ResponseTimeMs: 0,
	}
	require.NoError(t, db.CreateAuditLog(startLog))

	// Verify it exists as process.exec.start
	aggs, total, err := db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
		UserEmail: "dev@example.com",
	})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	assert.Equal(t, "process.exec.start", aggs[0].Action)
	assert.Equal(t, 0, aggs[0].AvgResponseTimeMs)

	// Simulate time passing
	time.Sleep(100 * time.Millisecond)

	// 2. Log process.exec (Completion)
	// Must match key fields (resource, command, ip, user agent)
	execLog := &gwdb.AuditLog{
		UserEmail:      "dev@example.com",
		ActionType:     "convox",
		Action:         "process.exec",
		Resource:       "cmd-123",
		Command:        "rails console",
		IPAddress:      "1.2.3.4",
		UserAgent:      "test-agent",
		Status:         "success",
		ResponseTimeMs: 5000, // 5 seconds duration
	}
	require.NoError(t, db.CreateAuditLog(execLog))

	// 3. Verify they merged into a single process.exec entry
	aggs, total, err = db.GetAuditLogsAggregated(gwdb.AuditLogFilters{
		UserEmail: "dev@example.com",
	})
	require.NoError(t, err)
	
	// Should still be 1 total aggregated entry
	require.Equal(t, 1, total, "process.exec should have merged into process.exec.start")
	require.Len(t, aggs, 1)

	agg := aggs[0]
	assert.Equal(t, "process.exec", agg.Action, "Action should be updated to process.exec")
	assert.Equal(t, 5000, agg.AvgResponseTimeMs, "Response time should be updated")
	assert.Equal(t, 1, agg.EventCount, "Event count should be reset to 1")
	
	// Timestamps check
	// FirstSeen should come from startLog (older)
	// LastSeen should come from execLog (newer)
	assert.Equal(t, startLog.Timestamp.UTC().Format(time.RFC3339), agg.FirstSeen.UTC().Format(time.RFC3339))
	assert.Equal(t, execLog.Timestamp.UTC().Format(time.RFC3339), agg.LastSeen.UTC().Format(time.RFC3339))
}
