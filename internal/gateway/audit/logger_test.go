package audit

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func TestParseConvoxAction(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)

	for _, spec := range rbac.RackRouteSpecs() {
		path := rbac.RackRouteExample(spec)
		method := spec.Method
		if method == "SOCKET" {
			method = http.MethodGet
		}
		action, resource := logger.ParseConvoxAction(path, method, "")
		expectedAction := fmt.Sprintf("%s.%s", spec.Resource, spec.Action)
		expectedResource := resourceInstance(path, spec.Resource.String(), spec.Action.String())
		assert.Equal(t, expectedAction, action, "pattern %s %s", spec.Method, spec.Pattern)
		assert.Equal(t, expectedResource, resource, "pattern %s %s", spec.Method, spec.Pattern)
	}
}

func TestMapHttpStatusToStatus(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)

	t.Run("Status101IsSuccess", func(t *testing.T) {
		st := logger.MapHttpStatusToStatus(101)
		assert.Equal(t, "success", st)
	})

	t.Run("Status302IsSuccess", func(t *testing.T) {
		st := logger.MapHttpStatusToStatus(302)
		assert.Equal(t, "success", st)
	})

	t.Run("UnknownStatusTreatedAsError", func(t *testing.T) {
		st := logger.MapHttpStatusToStatus(-1)
		assert.Equal(t, "error", st)
	})
}

func TestRedactEnvVars(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)

	vars := map[string]string{"FOO": "bar", "SECRET_TOKEN": "abc"}
	red := logger.RedactEnvVars(vars)
	assert.Equal(t, "[REDACTED]", red["FOO"])
	assert.Equal(t, "[REDACTED]", red["SECRET_TOKEN"])
}

func TestLogRequest(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)

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
}

func TestParseConvoxActionListEndpoints(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)

	// Test that all list endpoints return proper action and resource
	testCases := []struct {
		method           string
		path             string
		expectedAction   string
		expectedResource string
	}{
		{"GET", "/apps", BuildAction(rbac.ResourceApp.String(), rbac.ActionList.String()), "all"},
		{"GET", "/instances", BuildAction(rbac.ResourceInstance.String(), rbac.ActionList.String()), "all"},
		{
			"GET",
			"/apps/myapp/processes",
			BuildAction(rbac.ResourceProcess.String(), rbac.ActionList.String()),
			"all",
		},
		{"GET", "/apps/myapp/builds", BuildAction(rbac.ResourceBuild.String(), rbac.ActionList.String()), "all"},
		{
			"GET",
			"/apps/myapp/releases",
			BuildAction(rbac.ResourceRelease.String(), rbac.ActionList.String()),
			"all",
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			action, resource := logger.ParseConvoxAction(tc.path, tc.method, "")
			assert.Equal(t, tc.expectedAction, action, "action mismatch for %s %s", tc.method, tc.path)
			assert.Equal(t, tc.expectedResource, resource, "resource mismatch for %s %s", tc.method, tc.path)
			assert.NotEqual(t, "unknown", action, "action should not be unknown")
			assert.NotEqual(t, "unknown", resource, "resource should not be unknown")
		})
	}
}

func TestInferResourceType(t *testing.T) {
	database := dbtest.NewDatabase(t)
	logger := NewLogger(database)

	t.Run("CommonPaths", func(t *testing.T) {
		testCases := []struct {
			path         string
			action       string
			expectedType string
		}{
			{"/apps", BuildAction(rbac.ResourceApp.String(), rbac.ActionList.String()), "app"},
			{"/apps/myapp", BuildAction(rbac.ResourceApp.String(), rbac.ActionRead.String()), "app"},
			{"/instances", BuildAction(rbac.ResourceInstance.String(), rbac.ActionList.String()), "instance"},
			{"/apps/myapp/processes", BuildAction(rbac.ResourceProcess.String(), rbac.ActionList.String()), "process"},
			{"/apps/myapp/builds", BuildAction(rbac.ResourceBuild.String(), rbac.ActionList.String()), "build"},
			{"/apps/myapp/releases", BuildAction(rbac.ResourceRelease.String(), rbac.ActionList.String()), "release"},
			{
				"/system",
				BuildAction(rbac.ResourceRack.String(), rbac.ActionRead.String()),
				"rack",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.path, func(t *testing.T) {
				resourceType := logger.InferResourceType(tc.path, tc.action)
				assert.Equal(t, tc.expectedType, resourceType, "resource type mismatch for %s", tc.path)
				assert.NotEqual(t, "unknown", resourceType, "resource type should not be unknown")
			})
		}
	})

	t.Run("Fallback", func(t *testing.T) {
		resourceType := logger.InferResourceType("/totally/new/path", "")
		assert.Equal(t, "totally/new/path", resourceType)

		resourceType = logger.InferResourceType("", "")
		assert.Equal(t, "unknown", resourceType)
	})
}
