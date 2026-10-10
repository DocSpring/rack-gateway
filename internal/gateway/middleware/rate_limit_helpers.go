package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/security"
)

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
