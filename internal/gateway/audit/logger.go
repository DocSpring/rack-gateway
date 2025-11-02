package audit

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

type contextKey string

const (
	requestLoggedKey   contextKey = "rgw-request-logged"
	auditLogCreatedKey contextKey = "rgw-audit-log-created"
)

// Logger provides structured audit logging with automatic secret redaction
// and integration with database persistence and external notification systems.
type Logger struct {
	redactPatterns     []*regexp.Regexp
	database           *db.Database
	slackNotifier      SlackNotifier
	auditEventEnqueuer EventEnqueuer
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
	Timestamp     string                 `json:"ts"`
	UserEmail     string                 `json:"user_email"`
	Method        string                 `json:"method"`
	Path          string                 `json:"path"`
	QueryParams   string                 `json:"query_params,omitempty"`
	Status        int                    `json:"status"`
	LatencyMs     int64                  `json:"latency_ms"`
	RBACDecision  string                 `json:"rbac_decision"`
	RequestID     string                 `json:"request_id"`
	ClientIP      string                 `json:"client_ip"`
	RequestBody   map[string]interface{} `json:"request_body,omitempty"`
	ResponseError string                 `json:"response_error,omitempty"`
}

// NewLogger creates a new audit logger with automatic secret redaction patterns.
func NewLogger(database *db.Database) *Logger {
	return &Logger{
		redactPatterns: buildRedactPatterns(),
		database:       database,
		slackNotifier:  nil, // Set externally via SetSlackNotifier if needed
	}
}

// SetSlackNotifier configures the Slack notification integration for audit events.
func (l *Logger) SetSlackNotifier(notifier SlackNotifier) {
	l.slackNotifier = notifier
}

// SetAuditEventEnqueuer configures the audit event queue for asynchronous processing.
func (l *Logger) SetAuditEventEnqueuer(enqueuer EventEnqueuer) {
	l.auditEventEnqueuer = enqueuer
}

// LogDBEntry persists a DB-style audit log entry using this logger's database.
func (l *Logger) LogDBEntry(al *db.AuditLog) error {
	err := LogDB(l.database, al)
	if err == nil {
		l.notifySlackAsync(al)
	}
	return err
}

// LogDBEntryWithContext persists a DB-style audit log entry and marks the context as having an audit log.
// Returns the updated context.
func (l *Logger) LogDBEntryWithContext(ctx context.Context, al *db.AuditLog) (context.Context, error) {
	err := LogDB(l.database, al)
	if err == nil {
		ctx = MarkAuditLogCreated(ctx)
		l.notifySlackAsync(al)
	}
	return ctx, err
}

// Log writes a structured audit log entry to stdout for CloudWatch ingestion.
func (l *Logger) Log(entry *LogEntry) {
	if entry.RequestBody != nil {
		entry.RequestBody = l.redactMap(entry.RequestBody)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		gtwlog.Errorf("audit: failed to marshal audit log entry: %v", err)
		return
	}

	// Output structured JSON to stdout for CloudWatch ingestion
	if len(data) == 0 {
		data = []byte("{}")
	}
	buf := make([]byte, len(data)+1)
	copy(buf, data)
	buf[len(data)] = '\n'
	if _, err := os.Stdout.Write(buf); err != nil {
		gtwlog.Errorf("audit: failed to write audit log line: %v", err)
	}
}

// LogDB writes a DB-style audit entry to stdout as structured JSON and persists it.
// Use this helper anywhere an audit log would otherwise be written directly to the DB.
// This marks the request context as having created an audit log.
func LogDB(database *db.Database, al *db.AuditLog) error {
	// Mirror fields into a structured line suitable for CloudWatch ingestion
	count := al.EventCount
	if count <= 0 {
		count = 1
	}
	payload := map[string]interface{}{
		"ts":             time.Now().UTC().Format(time.RFC3339),
		"user_email":     al.UserEmail,
		"user_name":      al.UserName,
		"api_token_name": strings.TrimSpace(al.APITokenName),
		"action_type":    al.ActionType,
		"action":         al.Action,
		"resource":       al.Resource,
		"resource_type":  al.ResourceType,
		"command":        al.Command,
		"status":         al.Status,
		"rbac_decision":  al.RBACDecision,
		"http_status":    al.HTTPStatus,
		"latency_ms":     al.ResponseTimeMs,
		"ip_address":     al.IPAddress,
		"user_agent":     al.UserAgent,
		"event_count":    count,
	}
	if al.APITokenID != nil {
		payload["api_token_id"] = *al.APITokenID
	}
	if strings.TrimSpace(al.APITokenName) == "" {
		delete(payload, "api_token_name")
	}
	// Omit verbose request details; method/path are already logged separately
	if data, err := json.Marshal(payload); err == nil {
		if len(data) == 0 {
			data = []byte("{}")
		}
		buf := make([]byte, len(data)+1)
		copy(buf, data)
		buf[len(data)] = '\n'
		if _, writeErr := os.Stdout.Write(buf); writeErr != nil {
			gtwlog.Errorf("audit: failed to write audit log line: %v", writeErr)
		}
	} else {
		gtwlog.Errorf("audit: failed to marshal audit db log: %v", err)
	}
	if database == nil {
		return nil
	}
	// Set timestamp if not already set
	if al.Timestamp.IsZero() {
		al.Timestamp = time.Now().UTC()
	}
	return database.CreateAuditLog(al)
}

func (l *Logger) notifySlackAsync(al *db.AuditLog) {
	if l.auditEventEnqueuer == nil {
		return
	}

	// Check if Slack integration is configured before enqueueing
	if l.database != nil {
		integration, err := l.database.GetSlackIntegration()
		if err != nil || integration == nil {
			// No integration configured, skip enqueueing
			return
		}
	}

	if err := l.auditEventEnqueuer.EnqueueAuditEvent(al.ID); err != nil {
		log.Printf("failed to enqueue Slack audit event notification: %v", err)
	}
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
		QueryParams:  r.URL.RawQuery,
		Status:       status,
		LatencyMs:    latency.Milliseconds(),
		RBACDecision: rbacDecision,
		RequestID:    getRequestID(r),
		ClientIP:     getClientIP(r),
	}

	if err != nil {
		entry.ResponseError = err.Error()
	}

	l.Log(entry)
	markRequestLogged(r)
}

func getRequestID(r *http.Request) string {
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
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

// GetClientIP extracts the client IP address from the request
func (l *Logger) GetClientIP(r *http.Request) string {
	return getClientIP(r)
}

func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

// ParseConvoxAction extracts meaningful action and resource from the request.
// Returns "unknown" for both if route cannot be matched - caller must handle this.
// If resourceIDOverride is provided, it will be used instead of parsing from the path.
func (l *Logger) ParseConvoxAction(path, method, resourceIDOverride string) (action, resource string) {
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
func (l *Logger) BuildDetailsJSON(r *http.Request) string {
	details := map[string]interface{}{
		"method": r.Method,
		"path":   r.URL.Path,
	}

	// Add query parameters as-is (only app IDs and pagination params)
	if r.URL.RawQuery != "" {
		details["query"] = r.URL.RawQuery
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
	if requestID := getRequestID(r); requestID != "" {
		details["request_id"] = requestID
	}

	data, err := json.Marshal(details)
	if err != nil {
		return "{}"
	}

	return string(data)
}

// MapHttpStatusToStatus converts HTTP status code to audit status string
func (l *Logger) MapHttpStatusToStatus(httpStatus int) string {
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
func (l *Logger) InferResourceType(path, action string) string {
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
