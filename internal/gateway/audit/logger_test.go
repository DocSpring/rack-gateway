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
			{"/apps/myapp/env", "GET", "env.get", "myapp"},
			{"/apps/myapp/env", "POST", "env.set", "myapp"},
			{"/apps", "GET", "apps.list", "unknown"},
			{"/apps/myapp", "DELETE", "apps.delete", "myapp"},
			{"/apps/myapp/builds", "POST", "builds.create", "myapp"},
			{"/apps/myapp/run", "POST", "run.command", "myapp"},
			{"/apps/myapp/ps", "GET", "ps.list", "myapp"},
			{"/unknown/path", "GET", "unknown.get", "path"},
			{"/apps/myapp/processes/p1", "GET", "process.get", "p1"},
			{"/apps/myapp/processes/p1", "DELETE", "process.terminate", "p1"},
		}

		for _, test := range tests {
			action, resource := logger.parseConvoxAction(test.path, test.method)
			assert.Equal(t, test.expectedAction, action, "Path: %s %s", test.method, test.path)
			assert.Equal(t, test.expectedResource, resource, "Path: %s %s", test.method, test.path)
		}
	})

	// Comprehensive coverage of method/path → action mapping
	t.Run("ParseConvoxAction_Comprehensive", func(t *testing.T) {
		cases := []struct {
			m, p, a, r string
		}{
			// env
			{"GET", "/apps/myapp/env", "env.get", "myapp"},
			{"POST", "/apps/myapp/env", "env.set", "myapp"},
			// builds
			{"GET", "/apps/myapp/builds", "builds.list", "myapp"},
			{"POST", "/apps/myapp/builds", "builds.create", "myapp"},
			// releases: list, get, create, promote
			{"GET", "/apps/myapp/releases", "releases.list", "myapp"},
			{"GET", "/apps/myapp/releases/RAPI123", "releases.get", "myapp"},
			{"POST", "/apps/myapp/releases", "releases.create", "myapp"},
			{"POST", "/apps/myapp/releases/RAPI123/promote", "releases.promote", "myapp"},
			// run
			{"POST", "/apps/myapp/run", "run.command", "myapp"},
			// ps
			{"GET", "/apps/myapp/ps", "ps.list", "myapp"},
			{"POST", "/apps/myapp/ps", "ps.manage", "myapp"},
			// services processes
			{"GET", "/apps/app1/services/web/processes", "process.list", "app1/web"},
			{"POST", "/apps/app1/services/web/processes", "process.start", "app1/web"},
			// processes
			{"GET", "/apps/myapp/processes/p1", "process.get", "p1"},
			{"DELETE", "/apps/myapp/processes/p1", "process.terminate", "p1"},
			{"PUT", "/apps/myapp/processes/p1", "process.manage", "p1"},
			// exec
			{"GET", "/apps/myapp/processes/p1/exec", "process.exec", "p1"},
			// logs
			{"GET", "/apps/myapp/logs", "logs.view", "myapp"},
			// apps root
			{"GET", "/apps", "apps.list", "unknown"},
			{"GET", "/apps/thing", "apps.get", "thing"},
			{"POST", "/apps", "apps.create", "unknown"},
			{"DELETE", "/apps/thing", "apps.delete", "thing"},
			// racks collection
			{"GET", "/racks", "racks.list", "unknown"},
			// default fallthrough
			{"GET", "/system", "system.get", "unknown"},
			{"PATCH", "/unknown/path", "unknown.patch", "path"},
		}

		for _, c := range cases {
			a, r := logger.parseConvoxAction(c.p, c.m)
			assert.Equal(t, c.a, a, "action mismatch for %s %s", c.m, c.p)
			assert.Equal(t, c.r, r, "resource mismatch for %s %s", c.m, c.p)
		}
	})

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
		req, err := http.NewRequest("GET", "/apps/myapp/env?key=SECRET_TOKEN", nil)
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
		assert.Equal(t, "env.get", log.Action)
		assert.Equal(t, "myapp", log.Resource)
		assert.Equal(t, "success", log.Status)
		assert.Equal(t, 150, log.ResponseTimeMs)
		assert.Equal(t, "192.168.1.1", log.IPAddress)
		assert.Contains(t, log.Details, "GET")
		assert.Contains(t, log.Details, "/apps/myapp/env")
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
		assert.Equal(t, "apps.delete", log.Action)
		assert.Equal(t, "myapp", log.Resource)
	})

	t.Run("RedactionWorks", func(t *testing.T) {
		logger := NewLogger(nil) // No database for this test

		// Test path redaction
		redacted := logger.redactPath("/apps/myapp/env/SECRET_TOKEN")
		assert.Contains(t, redacted, "[REDACTED]")

		// Test query param redaction
		redacted = logger.redactQueryParams("key=SECRET_TOKEN&other=value")
		assert.Contains(t, redacted, "key=[REDACTED]")
		assert.Contains(t, redacted, "other=[REDACTED]")
	})
}
