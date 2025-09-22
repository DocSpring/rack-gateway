package handlers

import (
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/proxy"
	"github.com/gin-gonic/gin"
)

// ProxyHandler handles proxying requests to Convox
type ProxyHandler struct {
	proxy *proxy.Handler
}

// NewProxyHandler creates a new proxy handler
func NewProxyHandler(proxy *proxy.Handler) *ProxyHandler {
	return &ProxyHandler{
		proxy: proxy,
	}
}

// ProxyToRack proxies requests to the Convox rack
func (h *ProxyHandler) ProxyToRack(c *gin.Context) {
	// Convert Gin context to standard http.ResponseWriter and *http.Request
	h.proxy.ProxyToRack(c.Writer, c.Request)
}

// ProxyStripPrefix strips the /.gateway/api/convox prefix and proxies to rack
func (h *ProxyHandler) ProxyStripPrefix(c *gin.Context) {
	// Strip the /.gateway/api/convox prefix from path
	// This is used for safe GET-only endpoints exposed to the web UI
	originalPath := c.Request.URL.Path
	if strings.HasPrefix(originalPath, "/.gateway/api/convox") {
		c.Request.URL.Path = strings.TrimPrefix(originalPath, "/.gateway/api/convox")
	}

	// Proxy the request
	h.proxy.ProxyToRack(c.Writer, c.Request)
}
