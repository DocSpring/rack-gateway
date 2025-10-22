package handlers

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/rickb777/servefiles/v3/gin_adapter"
)

// RootRedirect handles the root path redirect
func RootRedirect(c *gin.Context) {
	c.Redirect(http.StatusTemporaryRedirect, DefaultWebRoute)
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
	cfg      *config.Config
}

const defaultDistDir = "web/dist"

// NewStaticHandler creates a new static handler
func NewStaticHandler(cfg *config.Config, sessions *auth.SessionManager) *StaticHandler {
	sh := &StaticHandler{distRoot: defaultDistDir, sessions: sessions, cfg: cfg}

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
					if err := resp.Body.Close(); err != nil {
						return err
					}

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
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("static: failed to close %s: %v", indexPath, err)
		}
	}()

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
		// Build script block with nonce
		isE2E := os.Getenv("E2E_TEST_MODE") == "true"
		e2eScript := ""
		if isE2E {
			e2eScript = "window.__e2e_test_mode__=true;"
		}
		scriptBlock := fmt.Sprintf(`<script nonce="%s">window.__nonce__="%s";window.__webpack_nonce__="%s";%s</script>`, nonce, nonce, nonce, e2eScript)
		result = bytes.ReplaceAll(result, []byte("{{RGW_SCRIPT_PLACEHOLDER}}"), []byte(scriptBlock))
	}

	if h.sessions != nil {
		if sessionCookie, err := r.Cookie("session_token"); err == nil {
			sessionToken := strings.TrimSpace(sessionCookie.Value)
			if sessionToken != "" {
				if _, err := h.sessions.ValidateSession(sessionToken, middleware.ClientIPFromRequest(r), r.UserAgent()); err == nil {
					if csrfToken, err := h.sessions.DeriveCSRFToken(sessionToken); err == nil && csrfToken != "" {
						result = replacePlaceholder(result, "RGW_CSRF_TOKEN", csrfToken)
					}
				}
			}
		}
	}

	var (
		dsn     string
		env     string
		release string
		sample  string
	)

	if h.cfg != nil {
		dsn = strings.TrimSpace(h.cfg.SentryJSDsn)
		env = strings.TrimSpace(h.cfg.SentryEnvironment)
		if env == "" {
			if h.cfg.DevMode {
				env = "development"
			} else {
				env = "production"
			}
		}
		release = strings.TrimSpace(h.cfg.SentryRelease)
		sample = strings.TrimSpace(h.cfg.SentryJSTracesRate)
	}

	result = replacePlaceholder(result, "RGW_SENTRY_DSN", dsn)
	result = replacePlaceholder(result, "RGW_SENTRY_ENVIRONMENT", env)
	result = replacePlaceholder(result, "RGW_SENTRY_RELEASE", release)
	result = replacePlaceholder(result, "RGW_SENTRY_TRACES_SAMPLE_RATE", sample)

	return result
}

func replacePlaceholder(result []byte, placeholder string, value string) []byte {
	pl := []byte(placeholder)
	if !bytes.Contains(result, pl) {
		return result
	}
	return bytes.ReplaceAll(result, pl, []byte(value))
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
