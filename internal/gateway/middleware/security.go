package middleware

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/ratelimit"
	securemw "github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"
)

// SecurityHeaders configures secure default headers via gin-contrib/secure with project-specific tweaks.
func SecurityHeaders(cfg *config.Config) gin.HandlerFunc {
	isDev := gin.Mode() == gin.DebugMode
	if cfg != nil && cfg.DevMode {
		isDev = true
	}

	connectSrc := "connect-src 'self' ws: wss:"
	if isDev {
		connectSrc = "connect-src 'self' ws: wss: http://localhost:* https://localhost:*"
	}
	csp := "default-src 'self'; " + connectSrc + "; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'"

	secCfg := securemw.Config{
		AllowedHosts: nil,
		SSLRedirect:  false,
		STSSeconds: func() int64 {
			if isDev {
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
		IsDevelopment:         isDev,
		BadHostHandler: func(c *gin.Context) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host"})
			c.Abort()
		},
	}

	return securemw.New(secCfg)
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
func CSRF() gin.HandlerFunc {
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
		headerToken := c.GetHeader("X-CSRF-Token")
		cookieToken, err := c.Cookie("csrf_token")

		if err != nil || headerToken == "" || cookieToken == "" || headerToken != cookieToken {
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			c.Abort()
			return
		}

		c.Next()
	}
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
