package audit

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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

type Logger struct {
	redactPatterns     []*regexp.Regexp
	database           *db.Database
	slackNotifier      SlackNotifier
	auditEventEnqueuer AuditEventEnqueuer
}

type SlackNotifier interface {
	NotifyAuditEvent(auditLog *db.AuditLog) error
}

type AuditEventEnqueuer interface {
	EnqueueAuditEvent(auditLogID int64) error
}

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

func NewLogger(database *db.Database) *Logger {
	patterns := []string{
		`(?i)(secret|token|password|key|authorization|cookie|set-cookie|session)`,
		`(?i)(api[-_]?key|api[-_]?secret|client[-_]?secret)`,
		`(?i)(bearer|jwt|auth)`,
	}

	compiled := make([]*regexp.Regexp, len(patterns))
	for i, pattern := range patterns {
		compiled[i] = regexp.MustCompile(pattern)
	}

	return &Logger{
		redactPatterns: compiled,
		database:       database,
		slackNotifier:  nil, // Set externally via SetSlackNotifier if needed
	}
}

func (l *Logger) SetSlackNotifier(notifier SlackNotifier) {
	l.slackNotifier = notifier
}

func (l *Logger) SetAuditEventEnqueuer(enqueuer AuditEventEnqueuer) {
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
	writeAuditLine(data)
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
		writeAuditLine(data)
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

func (l *Logger) LogRequest(r *http.Request, userEmail, rack, rbacDecision string, status int, latency time.Duration, err error) {
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

func (l *Logger) redactMap(data map[string]interface{}) map[string]interface{} {
	redacted := make(map[string]interface{})

	for key, value := range data {
		if l.shouldRedact(key) {
			redacted[key] = "[REDACTED]"
			continue
		}

		switch v := value.(type) {
		case map[string]interface{}:
			redacted[key] = l.redactMap(v)
		case []interface{}:
			redacted[key] = l.redactSlice(v)
		case string:
			if l.shouldRedact(v) {
				redacted[key] = "[REDACTED]"
			} else {
				redacted[key] = v
			}
		default:
			redacted[key] = value
		}
	}

	return redacted
}

func (l *Logger) redactSlice(data []interface{}) []interface{} {
	redacted := make([]interface{}, len(data))

	for i, item := range data {
		switch v := item.(type) {
		case map[string]interface{}:
			redacted[i] = l.redactMap(v)
		case string:
			if l.shouldRedact(v) {
				redacted[i] = "[REDACTED]"
			} else {
				redacted[i] = v
			}
		default:
			redacted[i] = item
		}
	}

	return redacted
}

func (l *Logger) shouldRedact(value string) bool {
	for _, pattern := range l.redactPatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func (l *Logger) RedactEnvVars(envVars map[string]string) map[string]string {
	redacted := make(map[string]string)
	for key := range envVars {
		redacted[key] = "[REDACTED]"
	}
	return redacted
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
	// For collection list actions, return "all" to indicate all resources
	// Check this FIRST before checking for specific resource instances
	if action == "list" {
		return "all"
	}

	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")

	// Processes with ID
	if resource == "process" {
		for i, seg := range parts {
			if seg == "processes" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	if resource == "release" {
		for i, seg := range parts {
			if seg == "releases" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	if resource == "instance" {
		for i, seg := range parts {
			if seg == "instances" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	// App-scoped routes: return app name if present
	if len(parts) >= 2 && parts[0] == "apps" {
		if parts[1] != "" {
			return parts[1]
		}
	}
	return resource
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

// mapStatusToString delegates to RBAC + HTTP mapping
func (l *Logger) mapStatusToString(httpStatus int, rbacDecision string) string {
	if strings.ToLower(rbacDecision) == "deny" {
		return "denied"
	}
	return l.MapHttpStatusToStatus(httpStatus)
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
