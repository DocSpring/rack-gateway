package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func attemptLog(requestID string) *gwdb.AuditLog {
	return &gwdb.AuditLog{
		UserEmail:    "admin@example.com",
		ActionType:   "gateway",
		Action:       "user.create",
		ResourceType: "user",
		Resource:     "/users",
		Details:      `{"method":"POST","route":"/api/v1/users"}`,
		RequestID:    requestID,
		Status:       "attempted",
		RBACDecision: "allow",
	}
}

// TestAggregationReplacesAnAttemptWithItsResult checks the UI's aggregated view shows a request's attempt until
// its first result arrives, then the result in the attempt's row, even with other events in between.
func TestAggregationReplacesAnAttemptWithItsResult(t *testing.T) {
	database := dbtest.NewDatabase(t)

	attempt := attemptLog("req-1")
	require.NoError(t, database.CreateAuditLog(attempt))

	aggs, total, err := database.GetAuditLogsAggregated(gwdb.AuditLogFilters{UserEmail: "admin@example.com"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	assert.Equal(t, "attempted", aggs[0].Status)

	other := &gwdb.AuditLog{
		UserEmail: "admin@example.com", ActionType: "auth", Action: "login.complete", Status: "success",
	}
	require.NoError(t, database.CreateAuditLog(other))

	result := &gwdb.AuditLog{
		UserEmail:      "admin@example.com",
		ActionType:     "users",
		Action:         "user.create",
		ResourceType:   "user",
		Resource:       "new@example.com",
		RequestID:      "req-1",
		Status:         "success",
		RBACDecision:   "allow",
		HTTPStatus:     201,
		ResponseTimeMs: 42,
	}
	require.NoError(t, database.CreateAuditLog(result))

	aggs, total, err = database.GetAuditLogsAggregated(gwdb.AuditLogFilters{UserEmail: "admin@example.com"})
	require.NoError(t, err)
	require.Equal(t, 2, total, "the result takes over the attempt's row")
	byAction := map[string]*gwdb.AuditLogAggregated{}
	for _, agg := range aggs {
		byAction[agg.Action] = agg
	}
	merged := byAction["user.create"]
	require.NotNil(t, merged)
	assert.Equal(t, "success", merged.Status)
	assert.Equal(t, "new@example.com", merged.Resource)
	assert.Equal(t, 201, merged.HTTPStatus)
	assert.Equal(t, attempt.ID, merged.FirstEventID)
	assert.Equal(t, result.ID, merged.LastEventID)
	assert.Equal(t, 1, merged.EventCount)

	// A second result of the same request gets a row of its own.
	second := &gwdb.AuditLog{
		UserEmail:  "admin@example.com",
		ActionType: "users",
		Action:     "user.update_roles",
		RequestID:  "req-1",
		Status:     "success",
	}
	require.NoError(t, database.CreateAuditLog(second))
	_, total, err = database.GetAuditLogsAggregated(gwdb.AuditLogFilters{UserEmail: "admin@example.com"})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
}

// TestAggregationKeepsAttemptsApart checks identical consecutive attempts aren't collapsed into one row: each
// needs its own row for its result to take over.
func TestAggregationKeepsAttemptsApart(t *testing.T) {
	database := dbtest.NewDatabase(t)

	require.NoError(t, database.CreateAuditLog(attemptLog("req-1")))
	require.NoError(t, database.CreateAuditLog(attemptLog("req-2")))

	_, total, err := database.GetAuditLogsAggregated(gwdb.AuditLogFilters{UserEmail: "admin@example.com"})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
}

// TestAggregationAddsEventCounts checks a throttled denial entry that stands for several denials counts as
// that many in the aggregated view.
func TestAggregationAddsEventCounts(t *testing.T) {
	database := dbtest.NewDatabase(t)

	denial := func(count int) *gwdb.AuditLog {
		return &gwdb.AuditLog{
			UserEmail:    "viewer@example.com",
			ActionType:   "gateway",
			Action:       "user.create",
			Status:       "denied",
			RBACDecision: "deny",
			HTTPStatus:   403,
			EventCount:   count,
		}
	}
	require.NoError(t, database.CreateAuditLog(denial(4)))

	aggs, _, err := database.GetAuditLogsAggregated(gwdb.AuditLogFilters{UserEmail: "viewer@example.com"})
	require.NoError(t, err)
	require.Len(t, aggs, 1)
	assert.Equal(t, 4, aggs[0].EventCount)

	require.NoError(t, database.CreateAuditLog(denial(3)))
	aggs, _, err = database.GetAuditLogsAggregated(gwdb.AuditLogFilters{UserEmail: "viewer@example.com"})
	require.NoError(t, err)
	require.Len(t, aggs, 1, "identical consecutive entries still share a row")
	assert.Equal(t, 7, aggs[0].EventCount)
}
