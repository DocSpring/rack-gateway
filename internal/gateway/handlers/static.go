package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/gin-gonic/gin"
	"github.com/rickb777/servefiles/v3/gin_adapter"
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

// StaticHandler serves the UI either via the Vite dev proxy or the compiled dist assets.
type StaticHandler struct {
	devProxy http.Handler
	distRoot string
	assets   *gin_adapter.GinAssets
}

const defaultDistDir = "web/dist"

// NewStaticHandler creates a new static handler
func NewStaticHandler(cfg *config.Config) *StaticHandler {
	sh := &StaticHandler{distRoot: defaultDistDir}

	if cfg != nil && cfg.DevMode {
		if raw := os.Getenv("WEB_DEV_SERVER_URL"); raw != "" {
			if target, err := url.Parse(raw); err == nil {
				sh.devProxy = httputil.NewSingleHostReverseProxy(target)
			}
		}
	}

	sh.configureAssets()

	return sh
}

func (h *StaticHandler) configureAssets() {
	assets := gin_adapter.NewAssetHandler(h.distRoot)
	assets = assets.WithMaxAge(365 * 24 * time.Hour)
	assets = assets.WithNotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.serveIndex(w, r)
	}))
	assets.DisableDirListing = true
	h.assets = assets
}

// ServeStatic serves static files from the web dist directory, or proxies to the dev server.
func (h *StaticHandler) ServeStatic(c *gin.Context) {
	if shouldRedirectToRack(c.Request) {
		c.Redirect(http.StatusTemporaryRedirect, "/.gateway/web/rack")
		return
	}

	if h.devProxy != nil {
		h.devProxy.ServeHTTP(c.Writer, c.Request)
		return
	}

	if h.assets == nil {
		h.serveIndex(c.Writer, c.Request)
		return
	}

	param := c.Param("filepath")
	if param == "" || param == "/" {
		h.serveIndex(c.Writer, c.Request)
		return
	}

	h.assets.HandlerFunc("filepath")(c)
}

func (h *StaticHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.NotFound(w, r)
		return
	}

	indexPath := filepath.Join(h.distRoot, "index.html")
	file, err := os.Open(indexPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// index.html should always be revalidated so new deployments propagate quickly
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "index.html", info.ModTime(), file)
}

func shouldRedirectToRack(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.URL.Path != "/.gateway/web/" {
		return false
	}
	if r.URL.RawQuery != "" {
		return false
	}
	conn := strings.ToLower(r.Header.Get("Connection"))
	upgrade := strings.ToLower(r.Header.Get("Upgrade"))
	if strings.Contains(conn, "upgrade") && upgrade == "websocket" {
		return false
	}
	return true
}
