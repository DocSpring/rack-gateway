package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStripInternalHeadersRemovesClientCopies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(StripInternalHeaders())

	var seen http.Header
	router.GET("/x", func(c *gin.Context) {
		seen = c.Request.Header.Clone()
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	for _, name := range gatewayInternalHeaders {
		req.Header.Set(name, "spoofed")
	}
	req.Header.Set("X-Request-ID", "kept")
	router.ServeHTTP(httptest.NewRecorder(), req)

	for _, name := range gatewayInternalHeaders {
		require.Emptyf(t, seen.Get(name), "%s must be stripped", name)
	}
	require.Equal(t, "kept", seen.Get("X-Request-ID"))
}
