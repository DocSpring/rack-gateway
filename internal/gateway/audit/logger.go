package audit

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
	"github.com/DocSpring/convox-gateway/internal/gateway/routematch"
	"github.com/google/uuid"
)

type Logger struct {
	redactPatterns []*regexp.Regexp
	database       *db.Database
}

type LogEntry struct {
	Timestamp     string                 `json:"ts"`
	UserEmail     string                 `json:"user_email"`
	Rack          string                 `json:"rack"`
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
	}
}

// LogDBEntry persists a DB-style audit log entry using this logger's database.
func (l *Logger) LogDBEntry(al *db.AuditLog) error {
	return LogDB(l.database, al)
}

func (l *Logger) Log(entry *LogEntry) {
	if entry.RequestBody != nil {
		entry.RequestBody = l.redactMap(entry.RequestBody)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal audit log: %v\n", err)
		return
	}

	// Output structured JSON to stdout for CloudWatch ingestion
	fmt.Println(string(data))
}

// LogDB writes a DB-style audit entry to stdout as structured JSON and persists it.
// Use this helper anywhere an audit log would otherwise be written directly to the DB.
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
		fmt.Println(string(data))
	} else {
		fmt.Fprintf(os.Stderr, "Failed to marshal audit db log: %v\n", err)
	}
	if database == nil {
		return nil
	}
	return database.CreateAuditLog(al)
}

// storeInDatabase stores the audit log for admin UI and compliance
func (l *Logger) storeInDatabase(r *http.Request, userEmail, rack, rbacDecision string, status int, latency time.Duration, err error) {
	if l.database == nil {
		return // Skip database logging if not configured
	}

	// Determine action and resource from path
	action, resource := l.parseConvoxAction(r.URL.Path, r.Method)
	// Allow override of resource via header for create events where we know the created ID
	if override := r.Header.Get("X-Audit-Resource"); override != "" {
		resource = override
	}
	// Normalize list actions to resource="all"
	if strings.HasSuffix(action, ".list") {
		resource = "all"
	} else if resource == "unknown" && rack != "" {
		// For rack-wide endpoints like /system when no specific resource, use the rack name
		if strings.HasPrefix(action, "system.") {
			resource = rack
		}
	}

	// Get user name if available from context or header
	userName := r.Header.Get("X-User-Name") // Set by auth middleware

	// Create database audit log
	// Determine resource type (app, rack, process, system, etc.)
	resourceType := l.inferResourceType(r.URL.Path, action)

	// Determine final status: if RBAC denied, mark as denied; otherwise map HTTP code
	finalStatus := ""
	if strings.ToLower(rbacDecision) == "deny" {
		finalStatus = "denied"
	} else {
		finalStatus = l.mapHttpStatusToStatus(status)
	}

	var tokenIDPtr *int64
	if tokenIDHeader := strings.TrimSpace(r.Header.Get("X-API-Token-ID")); tokenIDHeader != "" {
		if parsed, parseErr := strconv.ParseInt(tokenIDHeader, 10, 64); parseErr == nil {
			tokenIDPtr = &parsed
		}
	}
	tokenName := strings.TrimSpace(r.Header.Get("X-API-Token-Name"))

	auditLog := &db.AuditLog{
		UserEmail:      userEmail,
		UserName:       userName,
		APITokenID:     tokenIDPtr,
		APITokenName:   tokenName,
		ActionType:     "convox",
		Action:         action,
		Resource:       resource,
		ResourceType:   resourceType,
		Details:        l.buildDetailsJSON(r),
		IPAddress:      getClientIP(r),
		UserAgent:      r.UserAgent(),
		Status:         finalStatus,
		RBACDecision:   rbacDecision,
		HTTPStatus:     status,
		ResponseTimeMs: int(latency.Milliseconds()),
		EventCount:     1,
	}

	// Enrich with command when executing
	if action == "process.exec" {
		if cmd := r.Header.Get("command"); cmd != "" {
			auditLog.Command = cmd
		}
	}

	// Persist and mirror to stdout as DB-style JSON
	if dbErr := LogDB(l.database, auditLog); dbErr != nil {
		log.Printf("Failed to store audit log in database: %v", dbErr)
	}
}

func (l *Logger) LogRequest(r *http.Request, userEmail, rack, rbacDecision string, status int, latency time.Duration, err error) {
	// Create audit log entry for stdout/CloudWatch
	entry := &LogEntry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		UserEmail:    userEmail,
		Rack:         rack,
		Method:       r.Method,
		Path:         r.URL.Path,
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

	// Log to stdout for CloudWatch
	l.Log(entry)

	// Also store in database for queryability
	l.storeInDatabase(r, userEmail, rack, rbacDecision, status, latency, err)
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

// no ephemeral exec hints; authoritative source is the 'command' header from the WS request

// SaveProcessCommand stores a pid->command mapping (used to enrich exec audit entries)
// No-op migration helper removed; exec commands are stored directly on audit logs.

func getRequestID(r *http.Request) string {
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
	}
	return requestID
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

// parseConvoxAction extracts meaningful action and resource from the request
func (l *Logger) parseConvoxAction(path, method string) (action, resource string) {
	// For audit purposes, treat WebSocket GET upgrades as SOCKET method for matching
	res, act, ok := routematch.Match(method, path)
	if !ok && method == http.MethodGet && strings.Contains(path, "/logs") {
		if r2, a2, ok2 := routematch.Match("SOCKET", path); ok2 {
			res, act, ok = r2, a2, true
		}
	}
	if ok {
		return res + "." + act, resourceInstance(path, res, act)
	}
	return "unknown", "unknown"
}

func resourceInstance(path, resource, action string) string {
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
	// For collection list actions, prefer "unknown" (UI will render as "all")
	if action == "list" {
		return "unknown"
	}
	return resource
}

// buildDetailsJSON creates a JSON string with request details
func (l *Logger) buildDetailsJSON(r *http.Request) string {
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

// mapStatusToString converts HTTP status and RBAC decision to audit status
func (l *Logger) mapHttpStatusToStatus(httpStatus int) string {
	switch {
	case httpStatus == 101: // WebSocket Switching Protocols treated as success
		return "success"
	case httpStatus >= 200 && httpStatus < 300:
		return "success"
	case httpStatus >= 400 && httpStatus < 500:
		return "failed"
	case httpStatus >= 500:
		return "error"
	default:
		return "unknown"
	}
}

// mapStatusToString delegates to RBAC + HTTP mapping
func (l *Logger) mapStatusToString(httpStatus int, rbacDecision string) string {
	if strings.ToLower(rbacDecision) == "deny" {
		return "denied"
	}
	return l.mapHttpStatusToStatus(httpStatus)
}

// inferResourceType attempts to derive a normalized resource type label for UI display.
func (l *Logger) inferResourceType(path, action string) string {
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	if strings.HasPrefix(action, "release.") {
		return "release"
	}
	// Priority by path patterns
	if strings.Contains(path, "/processes/") {
		return "process"
	}
	if len(parts) > 0 {
		switch parts[0] {
		case "apps":
			return "app"
		case "racks":
			return "rack"
		case "system":
			return "system"
		}
	}
	// Fallback to action prefix (e.g., env.read -> env)
	if i := strings.Index(action, "."); i > 0 {
		return action[:i]
	}
	if action != "" {
		return action
	}
	return "unknown"
}
