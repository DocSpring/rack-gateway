package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RootRedirect handles the root path redirect
func RootRedirect(c *gin.Context) {
	userAgent := c.GetHeader("User-Agent")
	accept := c.GetHeader("Accept")

	// Redirect browsers to web UI
	if strings.Contains(accept, "text/html") || strings.Contains(userAgent, "Mozilla") {
		c.Redirect(http.StatusTemporaryRedirect, "/.gateway/web/")
		return
	}

	// Return JSON for CLI/API clients
	c.JSON(http.StatusOK, gin.H{
		"service": "convox-gateway",
		"version": "1.0.0",
	})
}

// Favicon serves the actual favicon from web/dist
func Favicon(c *gin.Context) {
	c.File("web/dist/favicon.ico")
}

// Robots returns robots.txt
func Robots(c *gin.Context) {
	c.String(http.StatusOK, "User-agent: *\nDisallow:")
}

// WebRedirect redirects to default web page
func WebRedirect(c *gin.Context) {
	c.Redirect(http.StatusTemporaryRedirect, "/.gateway/web/rack")
}

// StaticHandler serves static files for the web UI
type StaticHandler struct {
	// Could add configuration here
}

// NewStaticHandler creates a new static handler
func NewStaticHandler() *StaticHandler {
	return &StaticHandler{}
}

// ServeStatic serves static files from the web dist directory
func (h *StaticHandler) ServeStatic(c *gin.Context) {
	path := strings.TrimPrefix(c.Param("filepath"), "/")

	// Serve the file from web/dist
	// If the file doesn't exist and it's not an asset, serve index.html for SPA routing
	fullPath := "web/dist/" + path

	// Check if it's an asset file (js, css, images, etc)
	if strings.Contains(path, ".") {
		// Try to serve the actual file
		c.File(fullPath)
		return
	}

	// For SPA routes, always serve index.html
	c.File("web/dist/index.html")
}
