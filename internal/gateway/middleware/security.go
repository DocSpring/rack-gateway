package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/DocSpring/convox-gateway/internal/gateway/config"
	"github.com/DocSpring/convox-gateway/internal/gateway/ratelimit"
	"github.com/gin-gonic/gin"
)

// SecurityHeaders adds security headers to responses
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// CSP allows connections to localhost in dev
		isDev := gin.Mode() == gin.DebugMode
		connectSrc := "connect-src 'self' ws: wss:"
		if isDev {
			connectSrc = "connect-src 'self' ws: wss: http://localhost:* https://localhost:*"
		}

		csp := "default-src 'self'; " + connectSrc + "; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'"
		c.Header("Content-Security-Policy", csp)

		c.Next()
	}
}

// HostValidator validates Host and Origin headers
func HostValidator(allowedDomain string) gin.HandlerFunc {
	return func(c *gin.Context) {
		host := c.Request.Host
		origin := c.GetHeader("Origin")

		// In development, allow localhost
		if gin.Mode() == gin.DebugMode {
			if strings.Contains(host, "localhost") || strings.Contains(host, "127.0.0.1") {
				c.Next()
				return
			}
		}

		// Validate host if configured
		if allowedDomain != "" && !strings.Contains(host, allowedDomain) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host"})
			c.Abort()
			return
		}

		// Validate origin if present
		if origin != "" && allowedDomain != "" && !strings.Contains(origin, allowedDomain) {
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
	return gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/.gateway/api/health"},
		Formatter: func(params gin.LogFormatterParams) string {
			// Custom log format
			return fmt.Sprintf("%s %s %d %s %s\n",
				params.Method,
				params.Path,
				params.StatusCode,
				params.Latency,
				params.ClientIP,
			)
		},
	})
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
