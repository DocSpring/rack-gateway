package proxy

import (
	"errors"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
)

// recordAttempt writes the attempt entry of a rack request, reads included, before anything acts on it, and
// refuses the request with 503 when the audit log can't take that entry. A request the gateway API already
// recorded (the web UI's Convox routes) keeps that attempt.
func (h *Handler) recordAttempt(
	w http.ResponseWriter,
	r *http.Request,
	authUser *auth.User,
	rackName string,
	start time.Time,
) (*http.Request, bool) {
	if audit.AttemptFromContext(r.Context()) != nil {
		return r, true
	}

	entry := h.requestAuditEntry(r, authUser, r.URL.Path)
	setDeployApprovalID(r, entry)
	ctx, err := h.auditLogger.RecordAttempt(r, entry)
	if err != nil {
		// RecordAttempt has reported the failure; this is the request log line.
		h.auditLogger.LogRequest(r, authUser.Email, rackName, "error", http.StatusServiceUnavailable,
			time.Since(start), err)
		writeJSONError(w, audit.RefusalMessage(entry.Action), http.StatusServiceUnavailable)
		return r, false
	}
	return r.WithContext(ctx), true
}

// recordFailedResult records the outcome of an attempted request that failed after its attempt was written, so
// the attempt doesn't look like it never finished. Requests without an attempt are logged as before.
func (h *Handler) recordFailedResult(r *http.Request, authUser *auth.User, path string, status int, start time.Time) {
	if audit.AttemptFromContext(r.Context()) == nil {
		return
	}
	h.createAuditLogIfNeeded(r, authUser, path, status, start)
}

// approvalErrorStatus is the status a deploy approval check's error was refused with.
func approvalErrorStatus(err error) int {
	var appErr *deployApprovalError
	if errors.As(err, &appErr) {
		return appErr.status
	}
	return http.StatusInternalServerError
}
