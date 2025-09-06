package audit

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/DocSpring/convox-gateway/internal/gateway/db"
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

// storeInDatabase stores the audit log in SQLite for admin UI and compliance
func (l *Logger) storeInDatabase(r *http.Request, userEmail, rack, rbacDecision string, status int, latency time.Duration, err error) {
	if l.database == nil {
		return // Skip database logging if not configured
	}

	// Determine action and resource from path
	action, resource := l.parseConvoxAction(r.URL.Path, r.Method)
	// If resource is unknown for rack-wide endpoints like /system, set to rack name
	if resource == "unknown" && strings.HasPrefix(action, "system.") && rack != "" {
		resource = rack
	}

	// Get user name if available from context or header
	userName := r.Header.Get("X-User-Name") // Set by auth middleware

	// Create database audit log
	auditLog := &db.AuditLog{
		UserEmail:      userEmail,
		UserName:       userName,
		ActionType:     "convox_api",
		Action:         action,
		Resource:       resource,
		Details:        l.buildDetailsJSON(r),
		IPAddress:      getClientIP(r),
		UserAgent:      r.UserAgent(),
		Status:         l.mapStatusToString(status, rbacDecision),
		ResponseTimeMs: int(latency.Milliseconds()),
	}

	// Enrich with command when executing
	if action == "process.exec" {
		if cmd := r.Header.Get("command"); cmd != "" {
			auditLog.Command = cmd
		}
	}

	if dbErr := l.database.CreateAuditLog(auditLog); dbErr != nil {
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
		Path:         l.redactPath(r.URL.Path),
		QueryParams:  l.redactQueryParams(r.URL.RawQuery),
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

func (l *Logger) redactPath(path string) string {
	if strings.Contains(path, "/env") {
		parts := strings.Split(path, "/")
		for i, part := range parts {
			if i > 0 && parts[i-1] == "env" {
				if l.shouldRedact(part) {
					parts[i] = "[REDACTED]"
				}
			}
		}
		return strings.Join(parts, "/")
	}
	return path
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

func (l *Logger) redactQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	params := strings.Split(rawQuery, "&")
	for i, param := range params {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) == 2 {
			// Always redact query parameter values, keep the keys
			params[i] = parts[0] + "=[REDACTED]"
		}
	}
	return strings.Join(params, "&")
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
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		return "unknown", "unknown"
	}

	// Handle common Convox API patterns - check more specific paths first
	switch {
	// Check for specific sub-resources first (more specific matches)
	case strings.Contains(path, "/env"):
		if method == "GET" {
			action = "env.get"
		} else {
			action = "env.set"
		}
		// Find app name - it's usually before /env
		for i, part := range parts {
			if part == "env" && i > 0 {
				resource = parts[i-1]
				break
			}
		}

	case strings.Contains(path, "/builds"):
		if method == "GET" {
			action = "builds.list"
		} else if method == "POST" {
			action = "builds.create"
		}
		// Find app name - it's usually before /builds
		for i, part := range parts {
			if part == "builds" && i > 0 {
				resource = parts[i-1]
				break
			}
		}

	case strings.Contains(path, "/releases"):
		if method == "GET" {
			action = "releases.list"
		} else if method == "POST" {
			action = "releases.promote"
		}
		// Find app name - it's usually before /releases
		for i, part := range parts {
			if part == "releases" && i > 0 {
				resource = parts[i-1]
				break
			}
		}

	case strings.Contains(path, "/run"):
		action = "run.command"
		// Find app name - it's usually before /run
		for i, part := range parts {
			if part == "run" && i > 0 {
				resource = parts[i-1]
				break
			}
		}

	case strings.Contains(path, "/ps"):
		if method == "GET" {
			action = "ps.list"
		} else {
			action = "ps.manage"
		}
		// Find app name - it's usually before /ps
		for i, part := range parts {
			if part == "ps" && i > 0 {
				resource = parts[i-1]
				break
			}
		}

	case strings.Contains(path, "/processes/") && strings.HasSuffix(path, "/exec"):
		// /apps/{app}/processes/{pid}/exec
		action = "process.exec"
		// Extract process id as resource
		for i, part := range parts {
			if part == "processes" && i+1 < len(parts) {
				resource = parts[i+1]
				break
			}
		}

	case strings.Contains(path, "/processes/"):
		// /apps/{app}/processes/{pid}
		switch method {
		case "DELETE":
			action = "process.terminate"
		case "GET":
			action = "process.get"
		default:
			action = "process.manage"
		}
		for i, part := range parts {
			if part == "processes" && i+1 < len(parts) {
				resource = parts[i+1]
				break
			}
		}

		// Check for apps at the root level (less specific match)
	case strings.HasPrefix(path, "apps"):
		if method == "GET" {
			if len(parts) == 1 {
				action = "apps.list"
			} else {
				action = "apps.get"
			}
		} else if method == "POST" {
			action = "apps.create"
		} else if method == "DELETE" {
			action = "apps.delete"
		}
		if len(parts) > 1 {
			resource = parts[1]
		}

	default:
		action = fmt.Sprintf("%s.%s", parts[0], strings.ToLower(method))
		if len(parts) > 1 {
			resource = parts[1]
		}
	}

	if resource == "" {
		resource = "unknown"
	}

	return action, resource
}

// buildDetailsJSON creates a JSON string with request details
func (l *Logger) buildDetailsJSON(r *http.Request) string {
	details := map[string]interface{}{
		"method": r.Method,
		"path":   r.URL.Path,
	}

	// Add query parameters (redacted)
	if r.URL.RawQuery != "" {
		details["query"] = l.redactQueryParams(r.URL.RawQuery)
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
func (l *Logger) mapStatusToString(httpStatus int, rbacDecision string) string {
	switch {
	case rbacDecision == "deny":
		return "denied"
	case httpStatus >= 200 && httpStatus < 300:
		return "success"
	case httpStatus >= 400 && httpStatus < 500:
		return "blocked"
	case httpStatus >= 500:
		return "error"
	default:
		return "unknown"
	}
}
