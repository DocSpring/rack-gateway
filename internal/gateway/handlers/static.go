package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/rickb777/servefiles/v3/gin_adapter"
)

// RootRedirect handles the root path redirect
func RootRedirect(c *gin.Context) {
	userAgent := c.GetHeader("User-Agent")
	accept := c.GetHeader("Accept")

	// Redirect browsers to web UI
	if strings.Contains(accept, "text/html") || strings.Contains(userAgent, "Mozilla") {
		c.Redirect(http.StatusTemporaryRedirect, WebRoute("/"))
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
	c.Redirect(http.StatusTemporaryRedirect, DefaultWebRoute)
}

// StaticHandler serves the UI either via the Vite dev proxy or the compiled dist assets.
type StaticHandler struct {
	devProxy http.Handler
	distRoot string
	assets   *gin_adapter.GinAssets
	sessions *auth.SessionManager
}

const defaultDistDir = "web/dist"

// NewStaticHandler creates a new static handler
func NewStaticHandler(cfg *config.Config, sessions *auth.SessionManager) *StaticHandler {
	sh := &StaticHandler{distRoot: defaultDistDir, sessions: sessions}

	if cfg != nil && cfg.DevMode {
		if raw := os.Getenv("WEB_DEV_SERVER_URL"); raw != "" {
			if target, err := url.Parse(raw); err == nil {
				proxy := httputil.NewSingleHostReverseProxy(target)
				proxy.ModifyResponse = func(resp *http.Response) error {
					if resp == nil || resp.Request == nil {
						return nil
					}
					ct := resp.Header.Get("Content-Type")
					if !strings.Contains(ct, "text/html") {
						return nil
					}
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						return err
					}
					_ = resp.Body.Close()

					updated := sh.injectRuntimeTokens(body, resp.Request)
					resp.Body = io.NopCloser(bytes.NewReader(updated))
					resp.Header.Set("Cache-Control", "no-store")
					resp.Header.Del("Content-Length")
					resp.Header.Set("Content-Length", strconv.Itoa(len(updated)))
					return nil
				}
				proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
					if err != nil {
						http.Error(w, fmt.Sprintf("dev proxy error: %v", err), http.StatusBadGateway)
					}
				}
				sh.devProxy = proxy
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
	if shouldRedirectToDefault(c.Request) {
		c.Redirect(http.StatusTemporaryRedirect, DefaultWebRoute)
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

	data, err := io.ReadAll(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	content := data
	content = h.injectRuntimeTokens(content, r)

	reader := bytes.NewReader(content)

	// index.html should always be revalidated so new deployments propagate quickly
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "index.html", info.ModTime(), reader)
}

func (h *StaticHandler) injectRuntimeTokens(content []byte, r *http.Request) []byte {
	if len(content) == 0 || r == nil {
		return content
	}

	result := content

	if nonce := middleware.StyleNonceFromContext(r.Context()); nonce != "" {
		const placeholder = "CGW_STYLE_NONCE"
		placeholderBytes := []byte(placeholder)
		if bytes.Contains(result, placeholderBytes) {
			result = bytes.ReplaceAll(result, placeholderBytes, []byte(nonce))
		}
	}

	if h.sessions != nil {
		if sessionCookie, err := r.Cookie("session_token"); err == nil {
			sessionToken := strings.TrimSpace(sessionCookie.Value)
			if sessionToken != "" {
				if _, err := h.sessions.ValidateSession(sessionToken, clientIPFromRequest(r), r.UserAgent()); err == nil {
					if csrfToken, err := h.sessions.DeriveCSRFToken(sessionToken); err == nil && csrfToken != "" {
						const csrfPlaceholder = "CGW_CSRF_TOKEN"
						placeholderBytes := []byte(csrfPlaceholder)
						if bytes.Contains(result, placeholderBytes) {
							result = bytes.ReplaceAll(result, placeholderBytes, []byte(csrfToken))
						}
					}
				}
			}
		}
	}

	return result
}

func shouldRedirectToDefault(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.URL.Path != WebRoute("/") {
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

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" {
				return candidate
			}
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
