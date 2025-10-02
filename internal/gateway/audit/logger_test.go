package audit

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/routematch"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogger(t *testing.T) {
	database := dbtest.NewDatabase(t)

	logger := NewLogger(database)

	t.Run("ParseConvoxAction", func(t *testing.T) {
		for _, spec := range routematch.Specs() {
			if spec.Action == "*" {
				continue
			}
			path := routematch.ExamplePath(spec)
			method := spec.Method
			if method == "SOCKET" {
				method = http.MethodGet
			}
			action, resource := logger.ParseConvoxAction(path, method)
			expectedAction := fmt.Sprintf("%s.%s", spec.Resource, spec.Action)
			expectedResource := resourceInstance(path, spec.Resource, spec.Action)
			assert.Equal(t, expectedAction, action, "pattern %s %s", spec.Method, spec.Pattern)
			assert.Equal(t, expectedResource, resource, "pattern %s %s", spec.Method, spec.Pattern)
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

	t.Run("LogRequest_OnlyLogsToStdout", func(t *testing.T) {
		// LogRequest only logs to stdout now, not database
		req, err := http.NewRequest("GET", "/apps/myapp/releases?limit=1", nil)
		require.NoError(t, err)

		req.Header.Set("X-User-Name", "Test User")
		req.RemoteAddr = "192.168.1.1:1234"

		// This should not panic and should not create database entries
		logger.LogRequest(req, "test@example.com", "production", "allow", 200, 150*time.Millisecond, nil)

		// Verify NO database entry was created by LogRequest
		logs, err := database.GetAuditLogs("test@example.com", time.Time{}, 0)
		require.NoError(t, err)
		require.Len(t, logs, 0, "LogRequest should not create database entries")
	})

	t.Run("ParseConvoxAction_ListEndpoints", func(t *testing.T) {
		// Test that all list endpoints return proper action and resource
		// (should never return "unknown")
		testCases := []struct {
			method           string
			path             string
			expectedAction   string
			expectedResource string
		}{
			{"GET", "/apps", "app.list", "all"},
			{"GET", "/instances", "instance.list", "all"},
			{"GET", "/apps/myapp/processes", "process.list", "all"},
			{"GET", "/apps/myapp/builds", "build.list", "all"},
			{"GET", "/apps/myapp/releases", "release.list", "all"},
		}

		for _, tc := range testCases {
			t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
				action, resource := logger.ParseConvoxAction(tc.path, tc.method)
				assert.Equal(t, tc.expectedAction, action, "action mismatch for %s %s", tc.method, tc.path)
				assert.Equal(t, tc.expectedResource, resource, "resource mismatch for %s %s", tc.method, tc.path)
				assert.NotEqual(t, "unknown", action, "action should not be unknown")
				assert.NotEqual(t, "unknown", resource, "resource should not be unknown")
			})
		}
	})

	t.Run("InferResourceType_NeverUnknown", func(t *testing.T) {
		// Test that InferResourceType returns proper resource type for common paths
		testCases := []struct {
			path         string
			action       string
			expectedType string
		}{
			{"/apps", "app.list", "app"},
			{"/apps/myapp", "app.read", "app"},
			{"/instances", "instance.list", "instance"},
			{"/apps/myapp/processes", "process.list", "process"},
			{"/apps/myapp/builds", "build.list", "build"},
			{"/apps/myapp/releases", "release.list", "release"},
			{"/system", "rack.read", "rack"}, // System endpoints have action "rack.*" so type is "rack"
		}

		for _, tc := range testCases {
			t.Run(tc.path, func(t *testing.T) {
				resourceType := logger.InferResourceType(tc.path, tc.action)
				assert.Equal(t, tc.expectedType, resourceType, "resource type mismatch for %s", tc.path)
				assert.NotEqual(t, "unknown", resourceType, "resource type should not be unknown")
			})
		}
	})

}
