package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/ratelimit"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
)

// RateLimit applies rate limiting per client IP address and notifies on violations.
func RateLimit(_ *config.Config, securityNotifier *security.Notifier) gin.HandlerFunc {
	rateLimiter := ratelimit.NewRateLimiter(
		ratelimit.PositiveEnv("RATE_LIMIT_RPS", 10),
		int(ratelimit.PositiveEnv("RATE_LIMIT_BURST", 20)),
	)

	return func(c *gin.Context) {
		clientIP := netutil.ClientIP(c.Request)

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
