package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"unicode"
)

// redactedLogValue stands in for a credential or secret in a debug log.
const redactedLogValue = "[REDACTED]"

// sensitiveNameParts mark a header or JSON field that carries a credential or secret wherever they appear in its
// name: "Authorization", "Cookie", "X-CSRF-Token", "X-Postmark-Server-Token", "api_token", "assertion_response".
var sensitiveNameParts = []string{
	"assertion",
	"authorization",
	"cookie",
	"credential",
	"csrf",
	"otp",
	"passwd",
	"password",
	"private",
	"secret",
	"signature",
	"token",
}

// sensitiveNameWords mark a credential when they're a whole word of a header or field name ("code",
// "login_code", "backup_codes", "session_data", "X-Api-Key"). They're too common inside other words to match
// anywhere.
var sensitiveNameWords = map[string]struct{}{
	"code":    {},
	"codes":   {},
	"key":     {},
	"pin":     {},
	"session": {},
	"state":   {},
}

// envValueFields hold env vars by name, such as the env editor's {"set": {"KEY": "value"}}: the names are
// logged and every value is redacted.
var envValueFields = map[string]struct{}{
	"env":         {},
	"environment": {},
	"set":         {},
}

// isSensitiveName reports whether a header or JSON field named name carries a credential or secret.
func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, part := range sensitiveNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	words := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, word := range words {
		if _, ok := sensitiveNameWords[word]; ok {
			return true
		}
	}
	return false
}

// isSensitiveHeader reports whether a header's value must not be logged. Every MFA header (X-MFA-TOTP,
// X-MFA-WebAuthn, ...) is, since some carry a code or an assertion.
func isSensitiveHeader(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "x-mfa-") || isSensitiveName(name)
}

// formatHeadersForLog lists headers one per line, sorted, with the values of sensitive ones redacted.
func formatHeadersForLog(headers http.Header) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		for _, value := range headers[key] {
			if isSensitiveHeader(key) {
				value = redactedLogValue
			}
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(value)
			builder.WriteByte('\n')
		}
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

// redactJSONForLog returns a JSON body with the values of sensitive fields redacted, or ok false when body isn't
// valid JSON. A body that can't be parsed can't be redacted, so the caller doesn't log it.
func redactJSONForLog(body []byte) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil || decoder.More() {
		return "", false
	}
	redacted, err := json.Marshal(redactJSONValue("", value))
	if err != nil {
		return "", false
	}
	return string(redacted), true
}

// redactJSONValue redacts the value of the field named key: all of it when key is sensitive, every value in it
// when it holds env vars, and otherwise whatever is sensitive inside it. An array's items are redacted as values
// of the array's field.
func redactJSONValue(key string, value interface{}) interface{} {
	if key != "" && isSensitiveName(key) {
		return redactedLogValue
	}
	if _, holdsEnv := envValueFields[strings.ToLower(key)]; holdsEnv {
		return redactEnvValues(value)
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		for field, child := range typed {
			typed[field] = redactJSONValue(field, child)
		}
	case []interface{}:
		for i, item := range typed {
			typed[i] = redactJSONValue(key, item)
		}
	}
	return value
}

// redactEnvValues redacts a field holding env vars. Only the names of env vars given as an object are kept: a
// value in any other shape (such as "KEY=value" strings) could hold a value anywhere, so all of it is redacted.
func redactEnvValues(value interface{}) interface{} {
	env, ok := value.(map[string]interface{})
	if !ok {
		return redactedLogValue
	}
	for name := range env {
		env[name] = redactedLogValue
	}
	return env
}
