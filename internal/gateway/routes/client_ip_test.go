package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

// The real router resolves the client IP before any handler runs: behind the trusted ingress, a client-supplied
// X-Forwarded-For prefix is ignored and the address the proxy appended wins, for gin handlers and for code that
// only has the *http.Request.
func TestRouterResolvesClientIPBehindTrustedProxy(t *testing.T) {
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies([]string{"10.2.0.0/16"}))
	env := newAuthzEnvWithRouter(t, router)
	env.router.GET("/__test/client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP()+" "+netutil.ClientIP(c.Request))
	})

	req := httptest.NewRequest(http.MethodGet, "/__test/client-ip", nil)
	req.Host = "gateway.example.com"
	req.RemoteAddr = "10.2.87.202:41234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 100.85.250.16")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "100.85.250.16 100.85.250.16", rec.Body.String())
}
