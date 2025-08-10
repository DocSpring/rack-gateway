package audit

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogger(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.New(dbPath)
	require.NoError(t, err)
	defer database.Close()

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
		}

		for _, test := range tests {
			action, resource := logger.parseConvoxAction(test.path, test.method)
			assert.Equal(t, test.expectedAction, action, "Path: %s %s", test.method, test.path)
			assert.Equal(t, test.expectedResource, resource, "Path: %s %s", test.method, test.path)
		}
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
		assert.Equal(t, "convox_api", log.ActionType)
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
