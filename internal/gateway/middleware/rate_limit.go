package middleware

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/ratelimit"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
)

// RateLimit applies rate limiting per client IP address and notifies on violations.
func RateLimit(_ *config.Config, securityNotifier *security.Notifier) gin.HandlerFunc {
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

	rateLimiter := ratelimit.NewRateLimiter(rps, burst)

	return func(c *gin.Context) {
		clientIP := extractClientIP(c)
		setClientIPHeaders(c, clientIP)

		handler := rateLimiter.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		writer := &responseWriter{ResponseWriter: c.Writer, statusCode: http.StatusOK}
		handler.ServeHTTP(writer, c.Request)

		if writer.statusCode == http.StatusTooManyRequests {
			userEmail, userName := getUserInfo(c)
			notifyRateLimitExceeded(
				securityNotifier,
				userEmail,
				userName,
				c.Request.URL.Path,
				clientIP,
				c.GetHeader("User-Agent"),
			)
			c.Abort()
		}
	}
}
