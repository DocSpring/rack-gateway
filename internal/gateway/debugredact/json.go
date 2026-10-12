package debugredact

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

const (
	// TooLongPlaceholder is logged instead of a body longer than what was read of it: the start of a JSON body
	// can't be parsed, so it can't be redacted.
	TooLongPlaceholder = "[not logged: too long to redact]"
	// NotJSONPlaceholder is logged instead of a body that isn't valid JSON, which can't be redacted.
	NotJSONPlaceholder = "[not logged: not valid JSON, so it can't be redacted]"
)

// envValueFields hold env vars by name, such as the env editor's {"set": {"KEY": "value"}} or a release's "env":
// the names are logged and every value is redacted.
var envValueFields = map[string]struct{}{
	"env":         {},
	"environment": {},
	"set":         {},
}

// Body is a JSON body as it may be logged, with sensitive values redacted (see redactJSONValue). body is what was
// read of it, and limit how much was read: only a whole body of valid JSON can be redacted, so any other is left
// out, and a placeholder says why.
func Body(body []byte, limit int) string {
	return redactBody(body, limit, "")
}

// EnvBody is Body for a body that maps env var names to values, such as an app's environment from the rack: the
// names are logged and every value is redacted.
func EnvBody(body []byte, limit int) string {
	return redactBody(body, limit, "env")
}

func redactBody(body []byte, limit int, field string) string {
	if len(body) > limit {
		return TooLongPlaceholder
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil || decoder.More() {
		return NotJSONPlaceholder
	}
	redacted, err := json.Marshal(redactJSONValue(field, value))
	if err != nil {
		return NotJSONPlaceholder
	}
	return string(redacted)
}

// redactJSONValue redacts the value of the field named key: all of it when key is sensitive, every value in it
// when it holds env vars, and otherwise whatever is sensitive inside it. An array's items are redacted as values
// of the array's field.
func redactJSONValue(key string, value interface{}) interface{} {
	if key != "" && isSensitiveName(key) {
		return Redacted
	}
	if _, holdsEnv := envValueFields[strings.ToLower(key)]; holdsEnv {
		return redactEnvValues(value)
	}
	switch typed := value.(type) {
	case string:
		return redactString(typed)
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
		return Redacted
	}
	for name := range env {
		env[name] = Redacted
	}
	return env
}

// redactString redacts a string that holds a URL with credentials (postgres://user:password@host/db) whole, since
// the rest of the string may be part of the secret too. Other URLs in it lose their sensitive query values.
func redactString(value string) string {
	hasCredentials := false
	redacted := absoluteURLPattern.ReplaceAllStringFunc(value, func(match string) string {
		parsed, err := url.Parse(match)
		if err != nil || parsed.User != nil {
			hasCredentials = true
			return Redacted
		}
		return URL(match)
	})
	if hasCredentials {
		return Redacted
	}
	return redacted
}
