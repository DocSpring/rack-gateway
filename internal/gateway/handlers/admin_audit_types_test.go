package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// An admin handler's entries have the action type of their request's attempt entry: its route's permission scope.
func TestAuditEntryTypesFollowTheRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const tlsRefresh = "/api/v1/settings/rack-tls-cert/refresh"
	cases := []struct {
		method, pattern, path, action string
		actionType, resourceType      string
	}{
		{http.MethodPost, "/api/v1/users/:email/lock", "/api/v1/users/a@b.co/lock", "user.lock", "gateway", "user"},
		{http.MethodPost, "/api/v1/api-tokens", "/api/v1/api-tokens", "api_token.create", "gateway", "api_token"},
		{http.MethodPost, tlsRefresh, tlsRefresh, "rack_tls_cert.refresh", "security", "rack_tls_cert"},
		// A request outside the route table (handlers called directly) is a gateway entry.
		{http.MethodGet, "/unrouted", "/unrouted", "audit.export", "gateway", "audit"},
	}
	for _, tc := range cases {
		router := gin.New()
		var actionType, resourceType string
		router.Handle(tc.method, tc.pattern, func(c *gin.Context) {
			actionType, resourceType = auditEntryTypes(c, tc.action)
		})
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, tc.actionType, actionType, tc.action)
		require.Equal(t, tc.resourceType, resourceType, tc.action)
	}
}
