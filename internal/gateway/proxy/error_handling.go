package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/sentryutil"
)

func (h *Handler) handleError(
	w http.ResponseWriter,
	r *http.Request,
	message string,
	status int,
	rack string,
	start time.Time,
) {
	userEmail := "anonymous"
	if authUser, ok := auth.GetAuthUser(r.Context()); ok {
		userEmail = authUser.Email
	}

	// Capture 500-level errors to Sentry
	if status >= 500 && status < 600 {
		h.captureSentryError(r, fmt.Errorf("%s", message), userEmail)
	}

	if !audit.RequestAlreadyLogged(r) {
		h.auditLogger.LogRequest(r, userEmail, rack, "error", status, time.Since(start), fmt.Errorf("%s", message))
	}

	writeJSONError(w, message, status)
}

// writeJSONError answers with {"error": message}, which the Convox CLI shows.
func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		log.Printf("proxy: failed to encode error response: %v", err)
	}
}

// captureSentryError captures an error to Sentry with request context and user information.
func (h *Handler) captureSentryError(r *http.Request, err error, userEmail string) {
	if err == nil {
		return
	}

	emailForScope := userEmail
	if emailForScope == "anonymous" {
		emailForScope = ""
	}

	sentryutil.WithHTTPRequestScope(r, emailForScope, map[string]string{
		"component": "proxy",
		"rack":      h.rackName,
	}, func() {
		sentry.CaptureException(err)
	})
}

// refusalError is a request the gateway refuses after looking at what it would do on the rack, such as a build
// whose manifest runs an image the app's patterns don't allow. The client gets status, and the audit log a deny.
type refusalError struct {
	status int
	err    error
}

func (e *refusalError) Error() string { return e.err.Error() }

func (e *refusalError) Unwrap() error { return e.err }

// refuse makes err a refusal answered with status.
func refuse(status int, err error) error {
	return &refusalError{status: status, err: err}
}

// handleRefusal answers a request the gateway refused with the refusal's status, and audits it as a deny. It
// reports whether err was a refusal; any other error is left to the caller.
func (h *Handler) handleRefusal(w http.ResponseWriter, r *http.Request, err error, rack string, start time.Time) bool {
	var refusal *refusalError
	if !errors.As(err, &refusal) {
		return false
	}
	userEmail := "anonymous"
	if authUser, ok := auth.GetAuthUser(r.Context()); ok {
		userEmail = authUser.Email
	}
	h.auditLogger.LogRequest(r, userEmail, rack, "deny", refusal.status, time.Since(start), err)
	writeJSONError(w, err.Error(), refusal.status)
	return true
}
