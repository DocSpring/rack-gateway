package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
)

// cspDirectives requests a page through SecurityHeaders and returns its Content-Security-Policy's directives,
// by name.
func cspDirectives(t *testing.T, cfg *config.Config) map[string]string {
	t.Helper()
	originalMode := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })

	router := gin.New()
	router.Use(SecurityHeaders(cfg))
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := resp.Header().Get("Content-Security-Policy")
	require.NotEmpty(t, csp)
	directives := map[string]string{}
	for _, directive := range strings.Split(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), " ")
		directives[name] = value
	}
	return directives
}

// default-src doesn't cover <base>, form targets or framing, so each needs its own directive.
func TestCSPRestrictsBaseURIFormTargetsAndFraming(t *testing.T) {
	csp := cspDirectives(t, &config.Config{})

	require.Equal(t, "'self'", csp["base-uri"])
	require.Equal(t, "'self'", csp["form-action"])
	require.Equal(t, "'none'", csp["frame-ancestors"])
}

// The SPA only connects to the gateway, so a script that got onto a page can't send what it reads to another host,
// over HTTP or a WebSocket.
func TestCSPOnlyAllowsConnectionsToTheGateway(t *testing.T) {
	require.Equal(t, "'self'", cspDirectives(t, &config.Config{})["connect-src"])

	// The E2E and preview stacks run in dev mode with the CSP forced on: they get the same policy.
	t.Setenv("FORCE_CSP_IN_DEV", "true")
	csp := cspDirectives(t, &config.Config{DevMode: true})
	require.Equal(t, "'self'", csp["connect-src"])
	require.Equal(t, "'self' data:", csp["img-src"])
	require.NotContains(t, csp["script-src"], "unsafe-inline")
}
