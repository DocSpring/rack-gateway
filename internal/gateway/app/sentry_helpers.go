package app

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// CaptureError captures an error to Sentry if enabled, with HTTP request context.
// This should be called for all 500-level errors that represent unexpected failures.
// Use this from within gin handlers.
func CaptureError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	withGinScope(c, true, func(scope *sentry.Scope, hub *sentry.Hub) {
		hub.CaptureException(err)
	})
}

// CaptureHTTPError captures an HTTP error to Sentry with request context.
// Use this from non-gin handlers that have direct access to *http.Request.
func CaptureHTTPError(r *http.Request, err error) {
	if err == nil {
		return
	}

	withHTTPScope(r, func() {
		sentry.CaptureException(err)
	})
}

// CaptureMessage captures a message to Sentry (for cases where you don't have an error object).
func CaptureMessage(c *gin.Context, message string) {
	withGinScope(c, false, func(scope *sentry.Scope, hub *sentry.Hub) {
		hub.CaptureMessage(message)
	})
}

// CaptureHTTPMessage captures a message to Sentry with HTTP request context.
func CaptureHTTPMessage(r *http.Request, message string) {
	withHTTPScope(r, func() {
		sentry.CaptureMessage(message)
	})
}

// CreateErrorForCapture creates a standardized error for Sentry capture.
func CreateErrorForCapture(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

func withGinScope(c *gin.Context, fallbackToCurrent bool, capture func(scope *sentry.Scope, hub *sentry.Hub)) {
	if c == nil || capture == nil {
		return
	}

	hub := sentrygin.GetHubFromContext(c)
	if hub == nil && fallbackToCurrent {
		hub = sentry.CurrentHub()
	}
	if hub == nil {
		return
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		if c.Request != nil {
			scope.SetRequest(c.Request)
		}
		capture(scope, hub)
	})
}

func withHTTPScope(r *http.Request, capture func()) {
	if capture == nil {
		return
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		if r != nil {
			scope.SetRequest(r)
		}
		capture()
	})
}
