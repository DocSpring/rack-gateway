package handlers

import (
	"encoding/json"
	"strings"
)

const redactedJobArg = "[REDACTED]"

var sensitiveJobArgKeyParts = []string{"token", "secret", "password", "key"}

// redactJobArgs replaces the value of every object key that looks secret (contains token, secret,
// password or key) before job arguments are returned by the jobs API. Workers no longer receive
// credentials through job arguments; this is defense in depth for older rows and future mistakes.
// Arguments that cannot be parsed are withheld rather than passed through.
func redactJobArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	redacted, err := json.Marshal(redactJobArgValue(decoded))
	if err != nil {
		return nil
	}
	return redacted
}

func redactJobArgValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			if isSensitiveJobArgKey(key) {
				typed[key] = redactedJobArg
				continue
			}
			typed[key] = redactJobArgValue(nested)
		}
		return typed
	case []interface{}:
		for i, nested := range typed {
			typed[i] = redactJobArgValue(nested)
		}
		return typed
	default:
		return value
	}
}

func isSensitiveJobArgKey(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range sensitiveJobArgKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}
