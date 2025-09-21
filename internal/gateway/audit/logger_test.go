package audit

import (
	"net/http"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/testutil/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogger(t *testing.T) {
	database := dbtest.NewDatabase(t)

	logger := NewLogger(database)

	t.Run("ParseConvoxAction", func(t *testing.T) {
		tests := []struct {
			path             string
			method           string
			expectedAction   string
			expectedResource string
		}{
			{"/apps", "GET", "app.list", "unknown"},
			{"/apps/myapp", "DELETE", "app.delete", "myapp"},
			{"/apps/myapp/builds", "POST", "build.create", "myapp"},
			{"/apps/myapp/logs", "GET", "log.read", "myapp"},
			{"/apps/myapp/processes", "GET", "process.list", "myapp"},
			{"/apps/myapp/processes/p1", "GET", "process.get", "p1"},
			{"/apps/myapp/processes/p1", "DELETE", "process.terminate", "p1"},
			{"/apps/myapp/releases", "GET", "release.list", "myapp"},
			{"/apps/myapp/releases/REL123/promote", "POST", "release.promote", "REL123"},
		}

		for _, test := range tests {
			action, resource := logger.parseConvoxAction(test.path, test.method)
			assert.Equal(t, test.expectedAction, action, "Path: %s %s", test.method, test.path)
			assert.Equal(t, test.expectedResource, resource, "Path: %s %s", test.method, test.path)
		}
	})

	// Comprehensive coverage of method/path → action mapping
	// Note: exhaustive mapping now lives in internal/gateway/routes and is source of truth.

	// 101 Switching Protocols should be success
	t.Run("Status101IsSuccess", func(t *testing.T) {
		st := logger.mapStatusToString(101, "allow")
		assert.Equal(t, "success", st)
	})

	t.Run("RedactEnvVars_RedactsValues", func(t *testing.T) {
		vars := map[string]string{"FOO": "bar", "SECRET_TOKEN": "abc"}
		red := logger.RedactEnvVars(vars)
		assert.Equal(t, "[REDACTED]", red["FOO"])
		assert.Equal(t, "[REDACTED]", red["SECRET_TOKEN"])
	})

	t.Run("LogRequest", func(t *testing.T) {
		// Create a mock HTTP request
		req, err := http.NewRequest("GET", "/apps/myapp/releases?limit=1", nil)
		require.NoError(t, err)

		req.Header.Set("X-User-Name", "Test User")
		req.RemoteAddr = "192.168.1.1:1234"

		// Log the request
		logger.LogRequest(req, "test@example.com", "production", "allow", 200, 150*time.Millisecond, nil)

		// Verify it was stored in database
		logs, err := database.GetAuditLogs("test@example.com", time.Time{}, 0)
		require.NoError(t, err)
		require.Len(t, logs, 1)

		log := logs[0]
		assert.Equal(t, "test@example.com", log.UserEmail)
		assert.Equal(t, "Test User", log.UserName)
		assert.Equal(t, "convox", log.ActionType)
		assert.Equal(t, "release.list", log.Action)
		assert.Equal(t, "all", log.Resource)
		assert.Equal(t, "success", log.Status)
		assert.Equal(t, 150, log.ResponseTimeMs)
		assert.Equal(t, "192.168.1.1", log.IPAddress)
		assert.Contains(t, log.Details, "GET")
		assert.Contains(t, log.Details, "/apps/myapp/releases")
	})

	t.Run("LogDeniedRequest", func(t *testing.T) {
		req, err := http.NewRequest("DELETE", "/apps/myapp", nil)
		require.NoError(t, err)

		req.Header.Set("X-User-Name", "Viewer User")

		// Log a denied request
		logger.LogRequest(req, "viewer@example.com", "production", "deny", 403, 50*time.Millisecond, nil)

		// Verify it was stored with denied status
		logs, err := database.GetAuditLogs("viewer@example.com", time.Time{}, 0)
		require.NoError(t, err)
		require.Len(t, logs, 1)

		log := logs[0]
		assert.Equal(t, "denied", log.Status)
		assert.Equal(t, "app.delete", log.Action)
		assert.Equal(t, "myapp", log.Resource)
	})

}
