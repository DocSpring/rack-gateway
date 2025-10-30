package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

// CSRF validates CSRF tokens for state-changing requests.
func CSRF(sessionManager *auth.SessionManager) gin.HandlerFunc {
	if sessionManager == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") != "" {
			c.Next()
			return
		}

		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

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

		trimmedSession := strings.TrimSpace(sessionToken)
		clientIP := ClientIPFromRequest(c.Request)
		userAgent := c.GetHeader("User-Agent")
		if _, err := sessionManager.ValidateSession(trimmedSession, clientIP, userAgent); err != nil {
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
