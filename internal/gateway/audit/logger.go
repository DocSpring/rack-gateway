package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

type contextKey string

const (
	requestLoggedKey   contextKey = "rgw-request-logged"
	auditLogCreatedKey contextKey = "rgw-audit-log-created"
)

// Logger provides structured audit logging with database persistence and external notification systems.
// Callers redact sensitive values before they reach an entry: see envutil.AuditVisibleKeys for env changes.
type Logger struct {
	database           *db.Database
	writer             ChainWriter
	slackNotifier      SlackNotifier
	auditEventEnqueuer EventEnqueuer
	denials            *denialThrottle
}

// SlackNotifier sends audit event notifications to Slack.
type SlackNotifier interface {
	NotifyAuditEvent(auditLog *db.AuditLog) error
}

// EventEnqueuer queues audit events for asynchronous processing.
type EventEnqueuer interface {
	EnqueueAuditEvent(auditLogID int64) error
}

// LogEntry represents a structured audit log entry for CloudWatch ingestion.
type LogEntry struct {
	Timestamp     string `json:"ts"`
	UserEmail     string `json:"user_email"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	QueryParams   string `json:"query_params,omitempty"`
	Status        int    `json:"status"`
	LatencyMs     int64  `json:"latency_ms"`
	RBACDecision  string `json:"rbac_decision"`
	RequestID     string `json:"request_id"`
	ClientIP      string `json:"client_ip"`
	ResponseError string `json:"response_error,omitempty"`
}

// NewLogger creates a new audit logger that appends entries to database's hash chain.
func NewLogger(database *db.Database) *Logger {
	l := &Logger{
		database:      database,
		slackNotifier: nil, // Set externally via SetSlackNotifier if needed
	}
	if database != nil {
		l.writer = database
	}
	l.denials = newDenialThrottle(l.writeDenial)
	return l
}

// SetSlackNotifier configures the Slack notification integration for audit events.
func (l *Logger) SetSlackNotifier(notifier SlackNotifier) {
	l.slackNotifier = notifier
}

// SetAuditEventEnqueuer configures the audit event queue for asynchronous processing.
func (l *Logger) SetAuditEventEnqueuer(enqueuer EventEnqueuer) {
	l.auditEventEnqueuer = enqueuer
}

// LogDBEntry persists a DB-style audit log entry using this logger's database, and queues its Slack
// notification. A failure to write the entry is logged and sent to Sentry as well as returned; a notification that
// can't be queued is only logged.
func (l *Logger) LogDBEntry(al *db.AuditLog) error {
	if err := l.write(al); err != nil {
		return err
	}
	if err := l.notifySlack(al); err != nil {
		log.Printf("%v", err)
	}
	return nil
}

// LogAlert persists an alert's audit log entry like LogDBEntry, and also returns an error when its Slack
// notification can't be queued, so the sender can try the alert again.
func (l *Logger) LogAlert(al *db.AuditLog) error {
	if err := l.write(al); err != nil {
		return err
	}
	return l.notifySlack(al)
}

// Log writes a structured audit log entry to stdout for CloudWatch ingestion.
func (_ *Logger) Log(entry *LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		gtwlog.Errorf("audit: failed to marshal audit log entry: %v", err)
		return
	}
	WriteAuditLine(data)
}

// notifySlack queues the entry's Slack notification, when a Slack integration is configured.
func (l *Logger) notifySlack(al *db.AuditLog) error {
	if l.auditEventEnqueuer == nil {
		return nil
	}
	if l.database != nil {
		integration, err := l.database.GetSlackIntegration()
		if err != nil {
			return fmt.Errorf("failed to look up the Slack integration for audit event %d: %w", al.ID, err)
		}
		if integration == nil {
			return nil
		}
	}
	if err := l.auditEventEnqueuer.EnqueueAuditEvent(al.ID); err != nil {
		return fmt.Errorf("failed to enqueue Slack audit event notification: %w", err)
	}
	return nil
}

// MarkAuditLogCreated marks that an explicit audit log was created for this request
func MarkAuditLogCreated(ctx context.Context) context.Context {
	return context.WithValue(ctx, auditLogCreatedKey, true)
}

// HasAuditLogBeenCreated checks if an explicit audit log was created for this request
func HasAuditLogBeenCreated(ctx context.Context) bool {
	if v := ctx.Value(auditLogCreatedKey); v != nil {
		if created, ok := v.(bool); ok {
			return created
		}
	}
	return false
}

// LogRequest logs an HTTP request with user context, RBAC decision, and response details.
func (l *Logger) LogRequest(
	r *http.Request,
	userEmail, _ /* rack */, rbacDecision string,
	status int,
	latency time.Duration,
	err error,
) {
	// Use original path if available (before prefix stripping)
	path := r.Header.Get("X-Original-Path")
	if path == "" {
		path = r.URL.Path
	}

	// Log request to stdout for CloudWatch (all requests)
	entry := &LogEntry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		UserEmail:    userEmail,
		Method:       r.Method,
		Path:         path,
		QueryParams:  RedactQuery(r.URL.RawQuery),
		Status:       status,
		LatencyMs:    latency.Milliseconds(),
		RBACDecision: rbacDecision,
		RequestID:    RequestID(r),
		ClientIP:     netutil.ClientIP(r),
	}

	if err != nil {
		entry.ResponseError = err.Error()
	}

	l.Log(entry)
	markRequestLogged(r)
}

// RequestID returns r's request ID. The gateway generates one for every request (clients can't choose it, see
// middleware.StripInternalHeaders), so every audit entry written for a request carries the same unique ID: that's
// how a request's attempt entry and its result entries are paired. A request that arrived some other way (tests)
// gets one here, kept on the request so later entries agree.
func RequestID(r *http.Request) string {
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
		if r.Header == nil {
			r.Header = make(http.Header)
		}
		r.Header.Set("X-Request-ID", requestID)
	}
	return requestID
}

func markRequestLogged(r *http.Request) {
	if r == nil {
		return
	}
	ctx := context.WithValue(r.Context(), requestLoggedKey, true)
	*r = *r.WithContext(ctx)
}

// RequestAlreadyLogged checks if a request has already been logged to prevent duplicate logging.
func RequestAlreadyLogged(r *http.Request) bool {
	if r == nil {
		return false
	}
	if v := r.Context().Value(requestLoggedKey); v != nil {
		if logged, ok := v.(bool); ok {
			return logged
		}
	}
	return false
}

// ParseConvoxAction extracts meaningful action and resource from the request.
// Returns "unknown" for both if route cannot be matched - caller must handle this.
// If resourceIDOverride is provided, it will be used instead of parsing from the path.
func (_ *Logger) ParseConvoxAction(path, method, resourceIDOverride string) (action, resource string) {
	// For audit purposes, treat WebSocket GET upgrades as SOCKET method for matching
	res, act, ok := rbac.MatchRackRoute(method, path)
	if !ok && method == http.MethodGet && strings.Contains(path, "/logs") {
		if r2, a2, ok2 := rbac.MatchRackRoute("SOCKET", path); ok2 {
			res, act, ok = r2, a2, true
		}
	}
	if ok {
		resourceID := resourceIDOverride
		if resourceID == "" {
			resourceID = resourceInstance(path, res.String(), act.String())
		}
		return res.String() + "." + act.String(), resourceID
	}
	return "unknown", "unknown"
}

func resourceInstance(path, resource, action string) string {
	if action == "list" {
		return "all"
	}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")

	if candidate := extractSpecificResource(resource, parts); candidate != "" {
		return candidate
	}

	if app := extractAppName(parts); app != "" {
		return app
	}

	return resource
}

func extractSpecificResource(resource string, parts []string) string {
	lookups := map[string]string{
		"process":  "processes",
		"release":  "releases",
		"instance": "instances",
	}

	segment, ok := lookups[resource]
	if !ok {
		return ""
	}

	for i, part := range parts {
		if part == segment && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return ""
}

func extractAppName(parts []string) string {
	if len(parts) >= 2 && parts[0] == "apps" {
		return parts[1]
	}
	return ""
}

// BuildDetailsJSON creates a JSON string with request details
func (_ *Logger) BuildDetailsJSON(r *http.Request) string {
	details := map[string]interface{}{
		"method": r.Method,
		"path":   r.URL.Path,
	}

	if r.URL.RawQuery != "" {
		details["query"] = RedactQuery(r.URL.RawQuery)
	}

	// For exec, include command and process id if available
	if strings.HasSuffix(r.URL.Path, "/exec") {
		if cmd := r.Header.Get("command"); cmd != "" {
			details["command"] = cmd
		}
		// No fallback to WS frames; authoritative source is the 'command' header
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		for i, p := range parts {
			if p == "processes" && i+1 < len(parts) {
				details["process_id"] = parts[i+1]
				break
			}
		}
	}

	// Add request ID if available
	if requestID := RequestID(r); requestID != "" {
		details["request_id"] = requestID
	}

	data, err := json.Marshal(details)
	if err != nil {
		return "{}"
	}

	return string(data)
}

// MapHTTPStatusToStatus converts HTTP status code to audit status string
func (_ *Logger) MapHTTPStatusToStatus(httpStatus int) string {
	switch {
	case httpStatus == 101: // WebSocket Switching Protocols treated as success
		return "success"
	case httpStatus >= 100 && httpStatus < 400:
		return "success"
	case httpStatus >= 400 && httpStatus < 500:
		return "failed"
	case httpStatus >= 500:
		return "error"
	default:
		gtwlog.Warnf("audit: unexpected HTTP status %d, treating as error", httpStatus)
		return "error"
	}
}

// InferResourceType attempts to derive a normalized resource type label for UI display.
func (_ *Logger) InferResourceType(path, action string) string {
	// First, try to infer from action (most reliable)
	if i := strings.Index(action, "."); i > 0 {
		return action[:i]
	}

	// Fallback to path-based inference
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	if len(parts) > 0 {
		switch parts[0] {
		case "apps":
			return "app"
		case "racks":
			return "rack"
		case "system":
			return "system"
		case "instances":
			return "instance"
		}
	}

	if action != "" {
		return action
	}
	gtwlog.Warnf("audit: unable to infer resource type for path=%q action=%q", path, action)
	if trimmed := strings.Trim(strings.TrimSpace(path), "/"); trimmed != "" {
		return trimmed
	}
	return "unknown"
}
