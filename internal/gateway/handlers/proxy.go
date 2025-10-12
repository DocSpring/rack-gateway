package handlers

import (
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/proxy"
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

// ProxyStripPrefix strips the /api/v1/rack-proxy prefix and proxies to rack
func (h *ProxyHandler) ProxyStripPrefix(c *gin.Context) {
	// Strip prefix from path (supports both old and new routing)
	// This is used for CLI commands and safe GET-only endpoints exposed to the web UI
	originalPath := c.Request.URL.Path
	if strings.HasPrefix(originalPath, "/api/v1/rack-proxy") {
		trimmed := strings.TrimPrefix(originalPath, "/api/v1/rack-proxy")
		if trimmed == "" || trimmed[0] != '/' {
			trimmed = "/" + strings.TrimPrefix(trimmed, "/")
		}
		c.Request = c.Request.Clone(c.Request.Context())
		c.Request.Header.Set("X-Original-Path", originalPath)
		c.Request.URL.Path = trimmed
	}

	// Proxy the request
	h.proxy.ProxyToRack(c.Writer, c.Request)
}
