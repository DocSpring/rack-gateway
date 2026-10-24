package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// recordDenial records a request the proxy refused. Like the gateway API's denials, it's throttled (see
// audit.Logger.RecordDenial): the throttle is what keeps a script retrying a refused rack call from flooding the
// log, and proxied denials are the most likely to be retried.
func (h *Handler) recordDenial(r *http.Request, authUser *auth.User, status int, reason string) {
	entry := h.requestAuditEntry(r, authUser, r.URL.Path)
	entry.HTTPStatus = status
	entry.Details = audit.WithDetails(entry.Details, map[string]interface{}{"reason": reason})
	h.auditLogger.RecordDenial(entry)
}

// refuseBeforeRecording answers a request checkBeforeRecording refused. No attempt was recorded for it, so a refusal
// is recorded as a denial; any other error (a rack certificate that doesn't match) is answered as a request the
// gateway couldn't forward.
func (h *Handler) refuseBeforeRecording(
	w http.ResponseWriter,
	r *http.Request,
	authUser *auth.User,
	err error,
	rackName string,
	start time.Time,
) {
	var refusal *refusalError
	if errors.As(err, &refusal) {
		h.recordDenial(r, authUser, refusal.status, refusal.Error())
	}
	h.handleForwardError(w, r, err, rackName, start)
}

// refuseUnauthorized refuses a request RBAC denied.
func (h *Handler) refuseUnauthorized(
	w http.ResponseWriter,
	r *http.Request,
	authUser *auth.User,
	rackName, rackPath string,
	start time.Time,
) {
	resource, action, _ := rbac.MatchRackRoute(h.determineMethod(r), rackPath)
	cause := fmt.Errorf("permission denied for %s %s", r.Method, r.URL.Path)
	h.refuse(w, r, authUser, rackName, start, cause, forbiddenMessage(resource, action))
}

// refuse answers a denied request with 403 and message, and records the denial in the request log (with cause)
// and in the audit log.
func (h *Handler) refuse(
	w http.ResponseWriter,
	r *http.Request,
	authUser *auth.User,
	rackName string,
	start time.Time,
	cause error,
	message string,
) {
	h.auditLogger.LogRequest(r, authUser.Email, rackName, "deny", http.StatusForbidden, time.Since(start), cause)
	h.recordDenial(r, authUser, http.StatusForbidden, message)
	http.Error(w, message, http.StatusForbidden)
}

// recordApprovalDenial records an API token refused by a deploy approval check. An error from the check itself
// (a 500) isn't a denial.
func (h *Handler) recordApprovalDenial(r *http.Request, authUser *auth.User, err error) {
	var appErr *deployApprovalError
	if errors.As(err, &appErr) {
		h.recordDenial(r, authUser, appErr.status, appErr.message)
	}
}
