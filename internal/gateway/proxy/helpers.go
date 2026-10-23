package proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// logAccumulator accumulates log output with an optional size limit
type logAccumulator struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newLogAccumulator(limit int) *logAccumulator {
	return &logAccumulator{limit: limit}
}

func (l *logAccumulator) Write(p []byte) (int, error) {
	if l.limit <= 0 {
		return l.buf.Write(p)
	}
	remaining := l.limit - l.buf.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		if _, err := l.buf.Write(p[:remaining]); err != nil {
			return 0, err
		}
	}
	if len(p) > remaining {
		l.truncated = true
	}
	return len(p), nil
}

func (l *logAccumulator) Bytes() []byte {
	if !l.truncated {
		return l.buf.Bytes()
	}
	out := append([]byte{}, l.buf.Bytes()...)
	out = append(out, []byte("…(truncated)")...)
	return out
}

// logAudit writes an audit entry for r (linked to r's attempt when it recorded one) and marks r's context as
// having one.
func (h *Handler) logAudit(r *http.Request, al *db.AuditLog) error {
	setDeployApprovalID(r, al)
	err := h.auditLogger.LogRequestEntry(r.Context(), al)
	if err == nil {
		ctx := audit.MarkAuditLogCreated(r.Context())
		*r = *r.WithContext(ctx)
	}
	return err
}

// setDeployApprovalID ties al to the deploy approval that authorized r, if one did.
func setDeployApprovalID(r *http.Request, al *db.AuditLog) {
	if tracker := getDeployApprovalTracker(r.Context()); tracker != nil && tracker.request != nil {
		al.DeployApprovalRequestID = &tracker.request.ID
	}
}

// extractAppFromPath extracts the app name from a Convox API path
func extractAppFromPath(p string) string {
	// Strip /api/v1/rack-proxy prefix if present
	p = strings.TrimPrefix(p, "/api/v1/rack-proxy")
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// Handle: /apps/{app}/releases/..., /apps/{app}/processes/..., /apps/{app}/services/{service}/processes
	if len(parts) >= 2 && parts[0] == "apps" {
		return parts[1]
	}
	return ""
}

// appResourcePath splits a rack path /apps/{app}/{resource}/{id}[/...] into its segments, or returns nil when path
// isn't one. The ID is read at its fixed position: an app can be named "releases", "builds" or "processes", so the
// first segment with the resource's name isn't necessarily the resource.
func appResourcePath(path, resource string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "apps" || parts[2] != resource || parts[3] == "" {
		return nil
	}
	return parts
}

// appResourceID returns the {id} of a rack path /apps/{app}/{resource}/{id}[/...], or "" when path isn't one.
func appResourceID(path, resource string) string {
	if parts := appResourcePath(path, resource); parts != nil {
		return parts[3]
	}
	return ""
}

// extractReleaseIDFromPath returns the release ID of /apps/{app}/releases/{id}[/...].
func extractReleaseIDFromPath(path string) string {
	return appResourceID(path, "releases")
}

// extractBuildIDFromPath returns the build ID of /apps/{app}/builds/{id}[/...], or of a build export,
// /apps/{app}/builds/{id}.tgz.
func extractBuildIDFromPath(path string) string {
	return strings.TrimSuffix(appResourceID(path, "builds"), ".tgz")
}

// isBuildLogPath reports whether path is a build's logs, /apps/{app}/builds/{id}/logs.
func isBuildLogPath(path string) bool {
	parts := appResourcePath(path, "builds")
	return len(parts) == 5 && parts[4] == "logs"
}

// extractProcessIDFromPath returns the process ID of /apps/{app}/processes/{id}[/...].
func extractProcessIDFromPath(path string) string {
	return appResourceID(path, "processes")
}

// extractJSONString safely extracts a string from a JSON interface value
func extractJSONString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// forbiddenMessage returns a user-friendly message for RBAC denials
func forbiddenMessage(resource rbac.Resource, action rbac.Action) string {
	switch resource {
	case rbac.ResourceSecret:
		switch action {
		case rbac.ActionRead:
			return "You don't have permission to view secrets."
		case rbac.ActionSet:
			return "You don't have permission to modify secrets."
		}
	case rbac.ResourceEnv:
		if action == rbac.ActionRead {
			return "You don't have permission to view environment variables."
		}
	case rbac.ResourceProcess:
		switch action {
		case rbac.ActionStart, rbac.ActionExec:
			return "You don't have permission to run processes."
		case rbac.ActionTerminate, rbac.ActionStop:
			return "You don't have permission to stop processes."
		}
	case rbac.ResourceRelease:
		switch action {
		case rbac.ActionCreate:
			return "You don't have permission to create releases."
		case rbac.ActionPromote:
			return "You don't have permission to promote releases."
		}
	}
	return "permission denied"
}

// isDestructive returns true for destructive actions (delete, terminate, uninstall equivalents)
func isDestructive(method string, resource rbac.Resource, action rbac.Action) bool {
	if resource == rbac.ResourceProcess && (action == rbac.ActionTerminate || action == rbac.ActionStop) {
		return false
	}
	if strings.EqualFold(method, http.MethodDelete) {
		return true
	}
	// known destructive mappings
	if resource == rbac.ResourceApp && action == rbac.ActionDelete {
		return true
	}
	return false
}
