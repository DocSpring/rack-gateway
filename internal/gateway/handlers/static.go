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

	"github.com/gin-gonic/gin"
	"github.com/rickb777/servefiles/v3/gin_adapter"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/middleware"
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
	sh.setupDevProxyIfNeeded(cfg)
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
	//nolint:gosec // G304: Path is app-controlled distRoot + hardcoded filename
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

	result := h.injectScriptBlock(content, r)
	result = h.injectCSRFToken(result, r)
	result = h.injectSentryConfig(result)

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

func (h *StaticHandler) setupDevProxyIfNeeded(cfg *config.Config) {
	if cfg == nil || !cfg.DevMode {
		return
	}

	raw := os.Getenv("WEB_DEV_SERVER_URL")
	if raw == "" {
		return
	}

	target, err := url.Parse(raw)
	if err != nil {
		return
	}

	h.devProxy = h.createDevProxy(target)
}

func (h *StaticHandler) createDevProxy(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = createProxyDirector(target)
	proxy.ModifyResponse = h.createProxyResponseModifier()
	proxy.ErrorHandler = createProxyErrorHandler()
	return proxy
}

func createProxyDirector(target *url.URL) func(*http.Request) {
	return func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = target.Path + req.URL.Path
		req.Header.Set("X-Gateway-Proxy", "true")
	}
}

func (h *StaticHandler) createProxyResponseModifier() func(*http.Response) error {
	return func(resp *http.Response) error {
		if resp == nil || resp.Request == nil {
			return nil
		}

		if !isHTMLResponse(resp) {
			return nil
		}

		return h.modifyHTMLResponse(resp)
	}
}

func isHTMLResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(ct, "text/html")
}

func (h *StaticHandler) modifyHTMLResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := resp.Body.Close(); err != nil {
		return err
	}

	updated := h.injectRuntimeTokens(body, resp.Request)
	resp.Body = io.NopCloser(bytes.NewReader(updated))
	resp.Header.Set("Cache-Control", "no-store")
	resp.Header.Del("Content-Length")
	resp.Header.Set("Content-Length", strconv.Itoa(len(updated)))
	return nil
}

func createProxyErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, _ *http.Request, err error) {
		if err != nil {
			http.Error(w, fmt.Sprintf("dev proxy error: %v", err), http.StatusBadGateway)
		}
	}
}

func (h *StaticHandler) injectScriptBlock(content []byte, r *http.Request) []byte {
	nonce := middleware.StyleNonceFromContext(r.Context())
	e2eScript := getE2EScript()
	scriptBlock := buildScriptBlock(nonce, e2eScript)
	return bytes.ReplaceAll(content, []byte("{{RGW_SCRIPT_PLACEHOLDER}}"), []byte(scriptBlock))
}

func getE2EScript() string {
	if os.Getenv("E2E_TEST_MODE") == "true" {
		return "window.__e2e_test_mode__=true;"
	}
	return ""
}

func buildScriptBlock(nonce string, e2eScript string) string {
	if nonce != "" {
		return fmt.Sprintf(
			`<script nonce="%s">window.__nonce__="%s";window.__webpack_nonce__="%s";%s</script>`,
			nonce,
			nonce,
			nonce,
			e2eScript,
		)
	}
	return fmt.Sprintf(
		`<script>window.__nonce__="";window.__webpack_nonce__="";%s</script>`,
		e2eScript,
	)
}

func (h *StaticHandler) injectCSRFToken(content []byte, r *http.Request) []byte {
	if h.sessions == nil {
		return content
	}

	csrfToken := h.extractCSRFToken(r)
	if csrfToken == "" {
		return content
	}

	return replacePlaceholder(content, "RGW_CSRF_TOKEN", csrfToken)
}

func (h *StaticHandler) extractCSRFToken(r *http.Request) string {
	sessionCookie, err := r.Cookie("session_token")
	if err != nil {
		return ""
	}

	sessionToken := strings.TrimSpace(sessionCookie.Value)
	if sessionToken == "" {
		return ""
	}

	clientIP := middleware.ClientIPFromRequest(r)
	_, err = h.sessions.ValidateSession(sessionToken, clientIP, r.UserAgent())
	if err != nil {
		return ""
	}

	csrfToken, err := h.sessions.DeriveCSRFToken(sessionToken)
	if err != nil || csrfToken == "" {
		return ""
	}

	return csrfToken
}

func (h *StaticHandler) injectSentryConfig(content []byte) []byte {
	dsn, env, release, sample := h.getSentryConfig()

	result := replacePlaceholder(content, "RGW_SENTRY_DSN", dsn)
	result = replacePlaceholder(result, "RGW_SENTRY_ENVIRONMENT", env)
	result = replacePlaceholder(result, "RGW_SENTRY_RELEASE", release)
	result = replacePlaceholder(result, "RGW_SENTRY_TRACES_SAMPLE_RATE", sample)

	return result
}

func (h *StaticHandler) getSentryConfig() (string, string, string, string) {
	if h.cfg == nil {
		return "", "", "", ""
	}

	dsn := strings.TrimSpace(h.cfg.SentryJSDsn)
	env := h.getSentryEnvironment()
	release := strings.TrimSpace(h.cfg.SentryRelease)
	sample := strings.TrimSpace(h.cfg.SentryJSTracesRate)

	return dsn, env, release, sample
}

func (h *StaticHandler) getSentryEnvironment() string {
	env := strings.TrimSpace(h.cfg.SentryEnvironment)
	if env != "" {
		return env
	}

	if h.cfg.DevMode {
		return "development"
	}
	return "production"
}
