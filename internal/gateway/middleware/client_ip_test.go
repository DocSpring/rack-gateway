package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

func resolveClientIP(t *testing.T, trustedProxies []string, remoteAddr, forwardedFor string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(trustedProxies))
	router.Use(ClientIP())
	router.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, netutil.ClientIP(c.Request))
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestClientIPBehindTrustedProxyUsesRightMostUntrustedAddress(t *testing.T) {
	trusted := []string{"10.2.0.0/16"}

	// The ingress proxy appends the tailnet client's address; anything the client sent before it is ignored.
	require.Equal(t, "100.85.250.16",
		resolveClientIP(t, trusted, "10.2.87.202:5000", "1.2.3.4, 100.85.250.16"))
	require.Equal(t, "100.85.250.16", resolveClientIP(t, trusted, "10.2.87.202:5000", "100.85.250.16"))
}

func TestClientIPIgnoresForwardingHeadersFromUntrustedPeers(t *testing.T) {
	// A client connecting directly can't claim another address.
	require.Equal(t, "100.85.250.16",
		resolveClientIP(t, []string{"10.2.0.0/16"}, "100.85.250.16:5000", "1.2.3.4"))
	// With no trusted proxies configured, the TCP peer is the client.
	require.Equal(t, "10.2.87.202", resolveClientIP(t, nil, "10.2.87.202:5000", "1.2.3.4"))
}

func TestNetutilClientIPFallsBackToPeerOutsideTheRouter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8::1]:443"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	require.Equal(t, "2001:db8::1", netutil.ClientIP(req))
}
