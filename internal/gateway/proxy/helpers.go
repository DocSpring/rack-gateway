package proxy

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// clientIPFromRequest extracts the client IP address from the request
func clientIPFromRequest(r *http.Request) string {
	ip := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

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

// logAudit is a helper to log audit entries and mark the request context
func (h *Handler) logAudit(r *http.Request, al *db.AuditLog) error {
	err := h.auditLogger.LogDBEntry(al)
	if err == nil && r != nil {
		ctx := audit.MarkAuditLogCreated(r.Context())
		*r = *r.WithContext(ctx)
	}
	return err
}

// extractAppFromPath extracts the app name from a Convox API path
func extractAppFromPath(p string) string {
	// Strip /api/v1/convox prefix if present
	p = strings.TrimPrefix(p, "/api/v1/convox")
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// Handle: /apps/{app}/releases/..., /apps/{app}/processes/..., /apps/{app}/services/{service}/processes
	if len(parts) >= 2 && parts[0] == "apps" {
		return parts[1]
	}
	return ""
}

// extractReleaseIDFromPath extracts the release ID from a path
func extractReleaseIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range parts {
		if seg == "releases" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
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
		case rbac.ActionCreate, rbac.ActionPromote:
			return "You don't have permission to deploy releases."
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
