package auditverify_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// TestVerifyPassesForAttemptsResultsAndThrottledDenials writes entries the way fail-closed requests and
// throttled denials do (an attempt and its linked result, a denial and the entry counting its repeats) and
// checks the chain still verifies: they're ordinary appended events.
func TestVerifyPassesForAttemptsResultsAndThrottledDenials(t *testing.T) {
	database := newAuditDatabase(t)
	logger := audit.NewLogger(database)

	req := httptest.NewRequest(http.MethodDelete, "/apps/myapp", nil)
	ctx, err := logger.RecordAttempt(req, &gwdb.AuditLog{
		UserEmail: "admin@example.com", ActionType: "convox", Action: "app.delete", Resource: "myapp",
	})
	require.NoError(t, err)
	require.NoError(t, logger.LogRequestEntry(ctx, &gwdb.AuditLog{
		UserEmail: "admin@example.com", ActionType: "convox", Action: "app.delete", Resource: "myapp",
		Status: audit.StatusSuccess, HTTPStatus: http.StatusOK,
	}))

	for i := 0; i < 3; i++ {
		logger.RecordDenial(&gwdb.AuditLog{
			UserEmail: "viewer@example.com", ActionType: "gateway", Action: "user.create",
			HTTPStatus: http.StatusForbidden,
		})
	}
	logger.FlushDenials()

	report := verify(t, database, auditverify.Options{})
	requireOK(t, report)
	assert.Equal(t, int64(4), report.EventsChecked, "attempt, result, first denial, repeat count")
}
