package handlers

import (
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/sentryutil"
	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// CaptureError captures an error to Sentry with gin context.
// This should be called for all 500-level errors before returning to the client.
func CaptureError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	hub := sentrygin.GetHubFromContext(c)
	if hub == nil {
		// Fallback to global hub if gin middleware isn't available
		hub = sentry.CurrentHub()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		if c.Request != nil {
			scope.SetRequest(c.Request)
			scope.SetTag("http_method", c.Request.Method)
			scope.SetTag("http_path", c.Request.URL.Path)
		}

		// Add user context if available
		if email := c.GetString("user_email"); email != "" {
			scope.SetUser(sentry.User{Email: email})
		}

		hub.CaptureException(err)
	})
}

// CaptureErrorWithStatus captures an error to Sentry and returns a JSON error response.
// This is a convenience function that combines error capture and response.
func CaptureErrorWithStatus(c *gin.Context, err error, status int, message string) {
	// Only capture to Sentry for 500-level errors
	if status >= 500 && status < 600 {
		CaptureError(c, err)
	}

	c.JSON(status, gin.H{"error": message})
}

// CaptureHTTPError captures an error to Sentry from a raw *http.Request.
// Use this for non-gin handlers that work directly with http.ResponseWriter/Request.
func CaptureHTTPError(r *http.Request, err error, userEmail string) {
	if err == nil {
		return
	}

	sentryutil.WithHTTPRequestScope(r, userEmail, nil, func() {
		sentry.CaptureException(err)
	})
}
