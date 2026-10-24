package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// commandRoutes are the rack requests that run a command: a process start (`convox run --detach`) and an exec.
var commandRoutes = []struct{ method, path string }{
	{http.MethodPost, "/apps/myapp/services/web/processes"},
	{http.MethodGet, "/apps/myapp/processes/p1/exec"},
}

// auditDetailsFor returns the details of userEmail's latest audit entry for path with status.
func auditDetailsFor(t *testing.T, database *db.Database, userEmail, path, status string) map[string]interface{} {
	t.Helper()
	logs, err := database.GetAuditLogs(userEmail, time.Time{}, 100)
	require.NoError(t, err)
	for _, entry := range logs {
		var details map[string]interface{}
		if json.Unmarshal([]byte(entry.Details), &details) == nil && entry.Status == status && details["path"] == path {
			return details
		}
	}
	t.Fatalf("no %s audit entry for %s", status, path)
	return nil
}

// A command longer than the audit log can keep in full is refused before it reaches the rack, whoever sends it: an
// entry cut short would hide what ran. The refusal is audited as a denial.
func TestCommandsTooLongToAuditAreRefused(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	// The audit log records each < as <, six bytes.
	command := "echo " + strings.Repeat("<", audit.MaxRecordedCommandBytes/6)
	for _, route := range commandRoutes {
		req := withPath(requestAs(t, database, "admin@test.com"), route.method, route.path)
		req.Header.Set("Command", command)
		before := rack.count()
		rr := httptest.NewRecorder()
		h.ProxyToRack(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code, route.path)
		require.Contains(t, rr.Body.String(), "too long for the audit log", route.path)
		require.Equal(t, before, rack.count(), "%s must not reach the rack", route.path)

		denial := auditDetailsFor(t, database, "admin@test.com", route.path, audit.StatusDenied)
		require.Contains(t, denial["reason"], "too long for the audit log", route.path)
	}
}

// A command right at the limit runs, and its audit entry keeps all of it.
func TestCommandsAtTheLimitAreRecordedInFull(t *testing.T) {
	h, database, rack := newRecordingProxy(t)
	// Recorded as a JSON string: the command between two quotes.
	command := "echo " + strings.Repeat("x", audit.MaxRecordedCommandBytes-len(`"echo "`))
	for _, route := range commandRoutes {
		req := withPath(requestAs(t, database, "admin@test.com"), route.method, route.path)
		req.Header.Set("Command", command)
		rr := httptest.NewRecorder()
		h.ProxyToRack(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.Equal(t, command, rack.last(t).Header.Get("Command"), route.path)

		attempt := auditDetailsFor(t, database, "admin@test.com", route.path, audit.StatusAttempted)
		require.Equal(t, command, attempt["command"], route.path)
	}
}
