package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
)

func extractClientIP(c *gin.Context) string {
	clientIP := strings.TrimSpace(c.ClientIP())
	if clientIP == "" {
		if host, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil && host != "" {
			clientIP = host
		} else {
			clientIP = strings.TrimSpace(c.Request.RemoteAddr)
		}
	}
	return clientIP
}

func setClientIPHeaders(c *gin.Context, clientIP string) {
	if clientIP != "" {
		c.Request.Header.Set("X-Forwarded-For", clientIP)
		c.Request.Header.Set("X-Real-IP", clientIP)
	}
}

func getUserInfo(c *gin.Context) (email, name string) {
	if authUser, ok := auth.GetAuthUser(c.Request.Context()); ok && authUser != nil {
		return authUser.Email, authUser.Name
	}
	return "", ""
}

func notifyRateLimitExceeded(
	securityNotifier *security.Notifier,
	userEmail, userName, path, clientIP, userAgent string,
) {
	if securityNotifier != nil {
		securityNotifier.RateLimitExceeded(userEmail, userName, path, clientIP, userAgent)
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
