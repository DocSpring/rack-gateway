package middleware

import (
	"fmt"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// sentryResponseWriter wraps gin.ResponseWriter to capture the status code
type sentryResponseWriter struct {
	gin.ResponseWriter
	statusCode int
}

func (w *sentryResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// SentryErrorCapture is a middleware that automatically captures 500-level errors to Sentry.
// This ensures ALL internal server errors are reported, not just panics.
func SentryErrorCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Wrap the response writer to capture status code
		wrapper := &sentryResponseWriter{
			ResponseWriter: c.Writer,
			statusCode:     200, // default
		}
		c.Writer = wrapper

		// Process request
		c.Next()

		// After request completes, check if we have a 500-level error
		if wrapper.statusCode >= 500 && wrapper.statusCode < 600 {
			captureSentryError(c, wrapper.statusCode)
		}
	}
}

func captureSentryError(c *gin.Context, statusCode int) {
	hub := sentrygin.GetHubFromContext(c)
	if hub == nil {
		// Fallback to global hub if middleware isn't available
		hub = sentry.CurrentHub()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)

		// Add HTTP context
		if c.Request != nil {
			scope.SetRequest(c.Request)
			scope.SetTag("http.method", c.Request.Method)
			scope.SetTag("http.path", c.Request.URL.Path)
			scope.SetTag("http.status_code", fmt.Sprintf("%d", statusCode))
		}

		// Add request ID for correlation with CloudWatch logs
		if requestID := c.Writer.Header().Get("X-Request-ID"); requestID != "" {
			scope.SetTag("request_id", requestID)
		}

		// Add user context if available
		if email := c.GetString("user_email"); email != "" {
			scope.SetUser(sentry.User{
				Email:    email,
				Username: c.GetString("user_name"),
			})
		}

		// Add API token context if present
		if tokenName := c.GetString("api_token_name"); tokenName != "" {
			scope.SetTag("api_token", tokenName)
		}

		// Add rack context
		scope.SetTag("component", "gateway")

		// Add route info if available
		if c.HandlerName() != "" {
			scope.SetTag("handler", c.HandlerName())
		}

		// Create error message from status code
		errorMsg := fmt.Sprintf("HTTP %d error on %s %s", statusCode, c.Request.Method, c.Request.URL.Path)

		hub.CaptureMessage(errorMsg)
	})
}
