package audit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Logger struct {
	redactPatterns []*regexp.Regexp
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

func NewLogger() *Logger {
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

	fmt.Println(string(data))
}

func (l *Logger) LogRequest(r *http.Request, userEmail, rack, rbacDecision string, status int, latency time.Duration, err error) {
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

	l.Log(entry)
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
