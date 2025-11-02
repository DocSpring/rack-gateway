package proxy

import (
	"encoding/json"
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

	errorResponse := map[string]string{"error": message}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
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
