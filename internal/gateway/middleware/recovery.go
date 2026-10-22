package middleware

import (
	"errors"
	"net/http"
	"runtime/debug"
	"syscall"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"

	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// Recovery answers 500 for a request whose handler panicked, and reports the panic: logged with its stack, and sent
// to Sentry (when it's enabled). A panic from a connection that's gone (or http.ErrAbortHandler) only ends the
// request, since nothing can be written to it.
//
// It replaces gin.Recovery, whose recovered panics never reach Sentry, and which logs the request's headers
// (cookies and MFA codes among them) when a connection breaks.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if connectionGone(recovered) {
				gtwlog.Warnf("request %s %s ended: %v", c.Request.Method, c.Request.URL.Path, recovered)
				c.Abort()
				return
			}
			gtwlog.Errorf("panic serving %s %s: %v\n%s", c.Request.Method, c.Request.URL.Path, recovered, debug.Stack())
			reportPanic(c, recovered)
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}

// connectionGone reports whether a panic means the client's connection is gone, or the handler aborted it.
func connectionGone(recovered interface{}) bool {
	err, ok := recovered.(error)
	return ok && (errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, http.ErrAbortHandler))
}

// reportPanic sends a recovered panic to Sentry, with the request's scope.
func reportPanic(c *gin.Context, recovered interface{}) {
	hub := sentrygin.GetHubFromContext(c)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.RecoverWithContext(c.Request.Context(), recovered)
}
