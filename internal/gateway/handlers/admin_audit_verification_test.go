package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func TestGetAuditVerification(t *testing.T) {
	handler, database, _ := newAdminHandler(t)

	c, w := newGinContext(http.MethodGet, "/api/v1/audit-logs/verification", nil)
	handler.GetAuditVerification(c)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":null,"anchors_configured":false}`, w.Body.String(),
		"no status before the first run")

	checked := time.Date(2026, 5, 1, 0, 30, 0, 0, time.UTC)
	require.NoError(t, database.SaveAuditVerificationStatus(context.Background(), &db.AuditVerificationStatus{
		CheckedAt:    checked,
		Result:       db.AuditVerificationFailed,
		FailureCount: 1,
		Summary:      "FAILURES (1):\n  - event 4 (id 5): event_hash doesn't match\n",
		FailingSince: &checked,
	}))

	c, w = newGinContext(http.MethodGet, "/api/v1/audit-logs/verification", nil)
	handler.GetAuditVerification(c)
	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Status map[string]interface{} `json:"status"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, db.AuditVerificationFailed, response.Status["result"])
	assert.Equal(t, float64(1), response.Status["failure_count"])
	assert.Contains(t, response.Status["summary"], "event 4 (id 5)")
	failingSince, ok := response.Status["failing_since"].(string)
	require.True(t, ok)
	parsed, err := time.Parse(time.RFC3339, failingSince)
	require.NoError(t, err)
	assert.True(t, checked.Equal(parsed), failingSince)
	assert.NotContains(t, response.Status, "last_passed_at", "times that aren't known are left out")
}
