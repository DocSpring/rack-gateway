package routes

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// publicAPIRoutePrefixes are the /api/v1 routes registered outside the authenticated group. They
// authenticate in their own way (OAuth state, CLI login state, or CLIOnly + the proxy's own RBAC),
// so they are not in rbac's gateway route table. Adding a route here needs a security review.
var publicAPIRoutePrefixes = []string{
	"/api/v1/auth/cli/",
	"/api/v1/auth/web/",
	"/api/v1/rack-proxy/",
}

var publicAPIRoutes = map[string]struct{}{
	"GET /api/v1/health": {},
}

func isPublicAPIRoute(method, path string) bool {
	if _, ok := publicAPIRoutes[method+" "+path]; ok {
		return true
	}
	for _, prefix := range publicAPIRoutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// TestEveryRegisteredRouteHasAPolicy walks the real router: every /api/v1 route is either a known
// public route or declared in rbac's gateway route table, and every declared policy belongs to a
// registered route. A new route registered without a policy, or outside the authenticated group,
// fails here instead of shipping as 403 (or worse, unauthorized).
func TestEveryRegisteredRouteHasAPolicy(t *testing.T) {
	e := newAuthzEnv(t)

	registered := map[string]struct{}{}
	var unmapped []string
	for _, route := range e.router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/") {
			continue
		}
		key := route.Method + " " + route.Path
		registered[key] = struct{}{}
		if isPublicAPIRoute(route.Method, route.Path) {
			continue
		}
		if _, ok := rbac.LookupHTTPRoute(route.Method, route.Path); !ok {
			unmapped = append(unmapped, key)
		}
	}
	sort.Strings(unmapped)
	require.Empty(t, unmapped, "routes without an authorization policy in rbac/gateway_routes.go")

	var stale []string
	for _, spec := range rbac.HTTPRouteSpecs() {
		key := spec.Method + " " + spec.Pattern
		if _, ok := registered[key]; !ok {
			stale = append(stale, key)
		}
		if isPublicAPIRoute(spec.Method, spec.Pattern) {
			t.Errorf("%s is declared in the policy table but treated as public", key)
		}
	}
	sort.Strings(stale)
	require.Empty(t, stale, "policies for routes that are not registered")
}

// TestUnmappedRouteIsDenied checks the Authorize middleware fails closed for a route that is
// registered in the authenticated group but missing from the policy table.
func TestUnmappedRouteIsDenied(t *testing.T) {
	e := newAuthzEnv(t)
	router := gin.New()
	authenticated := router.Group("/api/v1")
	authenticated.Use(middleware.Authenticated(e.authService), middleware.Authorize(e.rbac))
	authenticated.GET("/unmapped-test", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/unmapped-test", nil)
	req.Host = "gateway.example.com"
	for k, v := range e.webSession(t, "admin@example.com") {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "no authorization policy for this endpoint")
}

func TestSelfRoutesNeverAcceptAPITokens(t *testing.T) {
	for _, spec := range rbac.HTTPRouteSpecs() {
		if spec.SelfParam == "" {
			continue
		}
		require.Falsef(t, spec.AllowAPIToken, "%s %s has a self-service rule and must not accept API tokens",
			spec.Method, spec.Pattern)
		require.Containsf(t, spec.Pattern, ":"+spec.SelfParam, "%s %s self param is not in the path",
			spec.Method, spec.Pattern)
		require.NotEmptyf(t, spec.Permissions, "%s %s needs permissions for other users' accounts",
			spec.Method, spec.Pattern)
	}
}
