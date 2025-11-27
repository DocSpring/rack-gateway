package audit

import "regexp"

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

// RedactEnvVars redacts all environment variables for security in audit logs
func (_ *Logger) RedactEnvVars(envVars map[string]string) map[string]string {
	redacted := make(map[string]string)
	for key := range envVars {
		redacted[key] = "[REDACTED]"
	}
	return redacted
}

// buildRedactPatterns creates the default set of regex patterns for secret redaction
func buildRedactPatterns() []*regexp.Regexp {
	patterns := []string{
		`(?i)(secret|token|password|key|authorization|cookie|set-cookie|session)`,
		`(?i)(api[-_]?key|api[-_]?secret|client[-_]?secret)`,
		`(?i)(bearer|jwt|auth)`,
	}

	compiled := make([]*regexp.Regexp, len(patterns))
	for i, pattern := range patterns {
		compiled[i] = regexp.MustCompile(pattern)
	}
	return compiled
}
