package middleware

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/ratelimit"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
)

func RateLimit(cfg *config.Config, securityNotifier *security.Notifier) gin.HandlerFunc {
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

		handler := rateLimiter.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		writer := &responseWriter{ResponseWriter: c.Writer, statusCode: http.StatusOK}
		handler.ServeHTTP(writer, c.Request)

		if writer.statusCode == http.StatusTooManyRequests {
			userEmail := ""
			userName := ""
			if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok && authUser != nil {
				userEmail = authUser.Email
				userName = authUser.Name
			}

			if securityNotifier != nil {
				securityNotifier.RateLimitExceeded(
					userEmail,
					userName,
					c.Request.URL.Path,
					clientIP,
					c.GetHeader("User-Agent"),
				)
			}

			c.Abort()
		}
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}
