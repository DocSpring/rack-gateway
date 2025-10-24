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
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/ratelimit"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
	securemw "github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"
)

type ctxKey string

const StyleNonceContextKey ctxKey = "rgw-style-nonce"

// Set any SHA-256 hashes if any libraries need to set inline styles or scripts.
// (This used to be required for goober and react-hot-toast but is no longer needed.)
var defaultStyleHashes = []string{}

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

		scriptDirectives := []string{"'self'"}
		styleDirectives := []string{"'self'"}
		if nonce != "" {
			scriptDirectives = append(scriptDirectives, fmt.Sprintf("'nonce-%s'", nonce))
			styleDirectives = append(styleDirectives, fmt.Sprintf("'nonce-%s'", nonce))
		}
		styleDirectives = append(styleDirectives, defaultStyleHashes...)
		if !isProdLike {
			scriptDirectives = append(scriptDirectives, "'unsafe-inline'")
			styleDirectives = append(styleDirectives, "'unsafe-inline'")
		}
		scriptSrc := "script-src " + strings.Join(scriptDirectives, " ")
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

	group := "rgw-sentry-csp"
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
	allowedHost, isDev := validatorContext(cfg)
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
	allowedHost, isDev := validatorContext(cfg)
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
	if sessionManager == nil {
		// CLI requests use Authorization headers and never rely on cookies,
		// so CSRF offers no protection and may block legitimate automation.
		// Keep middleware as a no-op when session storage is disabled.
		return func(c *gin.Context) {
			c.Next()
		}
	}
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
		if _, err := sessionManager.ValidateSession(trimmedSession, ClientIPFromRequest(c.Request), c.GetHeader("User-Agent")); err != nil {
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

func RequestLogger(logger *audit.Logger, defaultRack string, devMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if logger == nil {
			return
		}
		// Use original path if available (before prefix stripping)
		path := c.Request.Header.Get("X-Original-Path")
		if path == "" {
			path = c.Request.URL.Path
		}
		if path == "/api/v1/health" {
			return
		}
		if strings.HasPrefix(path, "/.well-known/") {
			return
		}
		if path == "/favicon.ico" {
			return
		}
		// Skip noisy Vite dev server requests in development (static assets)
		if devMode {
			if strings.HasPrefix(path, "/api/v1/") {
				// Always log API requests in development
			} else {
				trimmed := strings.TrimPrefix(path, "/")
				// Skip if path contains a file extension or @ (for @vite, @fs, etc.)
				if strings.Contains(trimmed, ".") || strings.Contains(trimmed, "@") {
					return
				}
			}
		}
		// RequestLogger is only for non-proxy routes (UI, auth, etc.)
		// Proxy routes log themselves in proxy/handler.go
		if audit.RequestAlreadyLogged(c.Request) {
			return
		}
		// Skip proxy routes - they handle their own logging
		if strings.HasPrefix(path, "/api/v1/rack-proxy/") {
			return
		}
		userEmail := strings.TrimSpace(c.Request.Header.Get("X-User-Email"))
		if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok && authUser != nil {
			if strings.TrimSpace(authUser.Email) != "" {
				userEmail = authUser.Email
			}
		}
		rack := strings.TrimSpace(defaultRack)
		if rack == "" {
			if alias := strings.TrimSpace(c.Request.Header.Get("X-Rack-Alias")); alias != "" {
				rack = alias
			} else if name := strings.TrimSpace(c.Request.Header.Get("X-Rack-Name")); name != "" {
				rack = name
			}
		}
		rbacDecision := strings.TrimSpace(c.Request.Header.Get("X-RBAC-Decision"))
		if rbacDecision == "" {
			rbacDecision = "allow"
		}
		logger.LogRequest(c.Request, userEmail, rack, rbacDecision, c.Writer.Status(), time.Since(start), nil)
	}
}

// RateLimit creates rate limiting middleware
func RateLimit(cfg *config.Config, securityNotifier *security.Notifier) gin.HandlerFunc {
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

		// If rate limited, abort the Gin chain and notify
		if writer.statusCode == http.StatusTooManyRequests {
			// Extract user info from context if authenticated
			userEmail := ""
			userName := ""
			if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok && authUser != nil {
				userEmail = authUser.Email
				userName = authUser.Name
			}

			// Notify about rate limit exceeded
			if securityNotifier != nil {
				securityNotifier.RateLimitExceeded(userEmail, userName, c.Request.URL.Path, clientIP, c.GetHeader("User-Agent"))
			}

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

func validatorContext(cfg *config.Config) (allowedHost string, isDev bool) {
	isDev = gin.Mode() == gin.DebugMode
	if cfg != nil && cfg.DevMode {
		isDev = true
	}
	if cfg != nil {
		allowedHost = canonicalHost(cfg.Domain)
	}
	return allowedHost, isDev
}
