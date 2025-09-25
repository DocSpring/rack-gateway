package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/auth"
	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/ratelimit"
	securemw "github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"
)

type ctxKey string

const StyleNonceContextKey ctxKey = "cgw-style-nonce"

// Some inline styles injected by frameworks/runtime
var defaultStyleHashes = []string{
	"'sha256-441zG27rExd4/il+NvIqyL8zFx5XmyNQtE381kSkUJk='",
	"'sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU='",
}

// SecurityHeaders configures secure default headers via gin-contrib/secure with project-specific tweaks.

func SecurityHeaders(cfg *config.Config) gin.HandlerFunc {
	var sentryCfg *sentrySecurityConfig
	if cfg != nil && strings.TrimSpace(cfg.SentryJSDsn) != "" {
		if parsed, err := buildSentrySecurityConfig(cfg); err != nil {
			log.Printf("security: invalid SENTRY_JS_DSN: %v", err)
		} else {
			sentryCfg = parsed
		}
	}

	return func(c *gin.Context) {
		isProdLike := true
		if cfg != nil && cfg.DevMode {
			if os.Getenv("FORCE_CSP_IN_DEV") != "true" {
				isProdLike = false
			}
		}

		nonce := generateNonce()
		if nonce != "" {
			c.Set(string(StyleNonceContextKey), nonce)
			ctx := context.WithValue(c.Request.Context(), StyleNonceContextKey, nonce)
			c.Request = c.Request.WithContext(ctx)
		}

		connectDirectives := []string{"'self'", "ws:", "wss:"}
		if !isProdLike {
			connectDirectives = append(connectDirectives, "http://localhost:*", "https://localhost:*")
		}
		if sentryCfg != nil && sentryCfg.ConnectOrigin != "" {
			connectDirectives = append(connectDirectives, sentryCfg.ConnectOrigin)
		}
		connectSrc := "connect-src " + strings.Join(connectDirectives, " ")

		imageDirectives := []string{"'self'", "data:"}
		if !isProdLike {
			imageDirectives = append(imageDirectives, "http://localhost:*", "https://localhost:*")
		}
		if sentryCfg != nil && sentryCfg.ConnectOrigin != "" {
			imageDirectives = append(imageDirectives, sentryCfg.ConnectOrigin)
		}
		imgSrc := "img-src " + strings.Join(imageDirectives, " ")

		scriptSrc := "script-src 'self'"
		styleDirectives := []string{"'self'"}
		if nonce != "" {
			styleDirectives = append(styleDirectives, fmt.Sprintf("'nonce-%s'", nonce))
		}
		styleDirectives = append(styleDirectives, defaultStyleHashes...)
		if !isProdLike {
			scriptSrc += " 'unsafe-inline'"
			styleDirectives = append(styleDirectives, "'unsafe-inline'")
		}
		styleSrc := "style-src " + strings.Join(styleDirectives, " ")

		cspParts := []string{
			"default-src 'self'",
			connectSrc,
			scriptSrc,
			styleSrc,
			imgSrc,
		}
		if sentryCfg != nil {
			cspParts = append(cspParts,
				fmt.Sprintf("report-uri %s", sentryCfg.ReportURL),
				fmt.Sprintf("report-to %s", sentryCfg.ReportGroup),
			)
		}
		csp := strings.Join(cspParts, "; ")

		secCfg := securemw.Config{
			AllowedHosts: nil,
			SSLRedirect:  false,
			STSSeconds: func() int64 {
				if !isProdLike {
					return 0
				}
				return 63072000
			}(),
			STSIncludeSubdomains:  false,
			FrameDeny:             true,
			ContentTypeNosniff:    true,
			BrowserXssFilter:      true,
			ContentSecurityPolicy: csp,
			ReferrerPolicy:        "strict-origin-when-cross-origin",
			SSLProxyHeaders:       map[string]string{"X-Forwarded-Proto": "https"},
			IsDevelopment:         !isProdLike,
			BadHostHandler: func(c *gin.Context) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host"})
				c.Abort()
			},
		}

		securemw.New(secCfg)(c)
		if sentryCfg != nil {
			c.Header("Report-To", sentryCfg.ReportToHeader)
			c.Header("Reporting-Endpoints", sentryCfg.ReportingEndpointsHeader)
		}
	}
}

// StyleNonceFromContext extracts the per-request style nonce from a context if present.
func StyleNonceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value := ctx.Value(StyleNonceContextKey); value != nil {
		if nonce, ok := value.(string); ok {
			return nonce
		}
	}
	return ""
}

func generateNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(buf)
}

type sentrySecurityConfig struct {
	ReportURL                string
	ReportGroup              string
	ReportToHeader           string
	ReportingEndpointsHeader string
	ConnectOrigin            string
}

func buildSentrySecurityConfig(cfg *config.Config) (*sentrySecurityConfig, error) {
	dsn := strings.TrimSpace(cfg.SentryJSDsn)
	if dsn == "" {
		return nil, fmt.Errorf("empty DSN")
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	publicKey := strings.TrimSpace(parsed.User.Username())
	if publicKey == "" {
		return nil, fmt.Errorf("DSN missing public key")
	}

	trimmedPath := strings.Trim(parsed.Path, "/")
	if trimmedPath == "" {
		return nil, fmt.Errorf("DSN missing project identifier")
	}
	segments := strings.Split(trimmedPath, "/")
	projectID := segments[len(segments)-1]
	pathPrefix := ""
	if len(segments) > 1 {
		pathPrefix = strings.Join(segments[:len(segments)-1], "/")
	}

	var apiPath strings.Builder
	apiPath.WriteString("/")
	if pathPrefix != "" {
		apiPath.WriteString(pathPrefix)
		apiPath.WriteString("/")
	}
	apiPath.WriteString("api/")
	apiPath.WriteString(projectID)
	apiPath.WriteString("/security/")

	baseURL := fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, apiPath.String())
	query := url.Values{}
	query.Set("sentry_key", publicKey)
	if env := strings.TrimSpace(cfg.SentryEnvironment); env != "" {
		query.Set("sentry_environment", env)
	}
	if release := strings.TrimSpace(cfg.SentryRelease); release != "" {
		query.Set("sentry_release", release)
	}
	reportURL := baseURL + "?" + query.Encode()

	group := "cgw-sentry-csp"
	payload := map[string]any{
		"group":              group,
		"max_age":            10886400,
		"endpoints":          []map[string]string{{"url": reportURL}},
		"include_subdomains": true,
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal report-to payload: %w", err)
	}

	connectOrigin := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	return &sentrySecurityConfig{
		ReportURL:                reportURL,
		ReportGroup:              group,
		ReportToHeader:           string(serialized),
		ReportingEndpointsHeader: fmt.Sprintf("%s=\"%s\"", group, reportURL),
		ConnectOrigin:            connectOrigin,
	}, nil
}

// HostValidator enforces that requests are sent to the configured domain while
// permitting internal health probes and localhost access.
func HostValidator(cfg *config.Config) gin.HandlerFunc {
	isDev := gin.Mode() == gin.DebugMode
	if cfg != nil && cfg.DevMode {
		isDev = true
	}

	allowedHost := ""
	if cfg != nil {
		allowedHost = canonicalHost(cfg.Domain)
	}

	return func(c *gin.Context) {
		ua := strings.ToLower(strings.TrimSpace(c.GetHeader("User-Agent")))
		if strings.Contains(ua, "kube-probe") {
			c.Next()
			return
		}

		host := canonicalHost(c.Request.Host)
		if host == "" && c.Request.URL != nil {
			host = canonicalHost(c.Request.URL.Host)
		}

		if host == "localhost" || host == "127.0.0.1" || host == "" {
			c.Next()
			return
		}

		if allowedHost == "" {
			c.Next()
			return
		}

		if strings.EqualFold(host, allowedHost) {
			c.Next()
			return
		}

		if isDev && isLocalHost(host) {
			c.Next()
			return
		}

		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host"})
		c.Abort()
	}
}

// OriginValidator validates the Origin header for cross-origin requests.
func OriginValidator(cfg *config.Config) gin.HandlerFunc {
	isDev := gin.Mode() == gin.DebugMode
	if cfg != nil && cfg.DevMode {
		isDev = true
	}

	allowedHost := ""
	if cfg != nil {
		allowedHost = canonicalHost(cfg.Domain)
	}

	return func(c *gin.Context) {
		// Only enforce for browser-originated requests. Probes and internal clients usually
		// omit typical browser headers, so skip origin validation for them.
		if allowedHost == "" || (c.GetHeader("User-Agent") == "" && c.GetHeader("Sec-Fetch-Site") == "") {
			c.Next()
			return
		}

		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		originURL, err := url.Parse(origin)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid origin"})
			c.Abort()
			return
		}

		originHost := canonicalHost(originURL.Host)
		if isDev && isLocalHost(originHost) {
			c.Next()
			return
		}
		if originHost == "" || !strings.EqualFold(originHost, allowedHost) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid origin"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// CSRF validates CSRF tokens for state-changing requests
func CSRF(sessionManager *auth.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip if has Authorization header (API token)
		if c.GetHeader("Authorization") != "" {
			c.Next()
			return
		}

		// Safe methods don't need CSRF
		if c.Request.Method == http.MethodGet ||
			c.Request.Method == http.MethodHead ||
			c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		// Check CSRF token
		headerToken := strings.TrimSpace(c.GetHeader("X-CSRF-Token"))
		if headerToken == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			c.Abort()
			return
		}

		sessionToken, err := c.Cookie("session_token")
		if err != nil || strings.TrimSpace(sessionToken) == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			c.Abort()
			return
		}

		if sessionManager == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "CSRF validation unavailable"})
			c.Abort()
			return
		}

		trimmedSession := strings.TrimSpace(sessionToken)
		if _, err := sessionManager.ValidateSession(trimmedSession, clientIPFromRequest(c.Request), c.GetHeader("User-Agent")); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			c.Abort()
			return
		}

		if !sessionManager.ValidateCSRFToken(trimmedSession, headerToken) {
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			c.Abort()
			return
		}

		c.Next()
	}
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

// FilteredLogger creates a logger that suppresses health check logs
func FilteredLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// RateLimit creates rate limiting middleware
func RateLimit(cfg *config.Config) gin.HandlerFunc {
	// Read rate limit config from environment, same as original main.go
	// Default: 10 req/s with burst of 20 for production
	rps := 10.0
	burst := 20
	if rpsEnv := os.Getenv("RATE_LIMIT_RPS"); rpsEnv != "" {
		if parsed, err := strconv.ParseFloat(rpsEnv, 64); err == nil {
			rps = parsed
		}
	}
	if burstEnv := os.Getenv("RATE_LIMIT_BURST"); burstEnv != "" {
		if parsed, err := strconv.Atoi(burstEnv); err == nil {
			burst = parsed
		}
	}

	// Use the existing in-memory rate limiter
	rateLimiter := ratelimit.NewRateLimiter(rps, burst)

	return func(c *gin.Context) {
		clientIP := strings.TrimSpace(c.ClientIP())
		if clientIP == "" {
			if host, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil && host != "" {
				clientIP = host
			} else {
				clientIP = strings.TrimSpace(c.Request.RemoteAddr)
			}
		}
		if clientIP != "" {
			c.Request.Header.Set("X-Forwarded-For", clientIP)
			c.Request.Header.Set("X-Real-IP", clientIP)
		}

		// Create a wrapper handler that will continue or abort
		handler := rateLimiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If we get here, rate limit passed
		}))

		// Create a custom ResponseWriter to capture the status
		writer := &responseWriter{ResponseWriter: c.Writer, statusCode: 200}

		// Run the rate limiter
		handler.ServeHTTP(writer, c.Request)

		// If rate limited, abort the Gin chain
		if writer.statusCode == http.StatusTooManyRequests {
			c.Abort()
		}
	}
}

// responseWriter wraps gin's ResponseWriter to capture status codes
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func canonicalHost(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ".")
	raw = strings.ToLower(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "[") {
		if end := strings.Index(raw, "]"); end != -1 {
			inner := raw[1:end]
			return strings.TrimSpace(inner)
		}
	}
	if idx := strings.LastIndex(raw, ":"); idx != -1 && !strings.Contains(raw[idx+1:], ":") {
		raw = raw[:idx]
	}
	return raw
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1"
}
