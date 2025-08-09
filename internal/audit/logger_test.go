package audit

import (
	"testing"
)

func TestRedaction(t *testing.T) {
	logger := NewLogger()

	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "redact secret keys",
			input: map[string]interface{}{
				"username": "user@example.com",
				"password": "secret123",
				"api_key":  "key123",
			},
			expected: map[string]interface{}{
				"username": "user@example.com",
				"password": "[REDACTED]",
				"api_key":  "[REDACTED]",
			},
		},
		{
			name: "redact nested secrets",
			input: map[string]interface{}{
				"config": map[string]interface{}{
					"database": "mydb",
					"secret":   "hidden",
				},
			},
			expected: map[string]interface{}{
				"config": map[string]interface{}{
					"database": "mydb",
					"secret":   "[REDACTED]",
				},
			},
		},
		{
			name: "redact authorization headers",
			input: map[string]interface{}{
				"headers": map[string]interface{}{
					"Content-Type":  "application/json",
					"Authorization": "Bearer token123",
				},
			},
			expected: map[string]interface{}{
				"headers": map[string]interface{}{
					"Content-Type":  "application/json",
					"Authorization": "[REDACTED]",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := logger.redactMap(tt.input)
			
			for key, expectedValue := range tt.expected {
				actualValue, exists := result[key]
				if !exists {
					t.Errorf("Expected key %s not found in result", key)
					continue
				}

				switch expected := expectedValue.(type) {
				case map[string]interface{}:
					actual, ok := actualValue.(map[string]interface{})
					if !ok {
						t.Errorf("Expected map for key %s, got %T", key, actualValue)
						continue
					}
					for k, v := range expected {
						if actual[k] != v {
							t.Errorf("For key %s.%s, expected %v, got %v", key, k, v, actual[k])
						}
					}
				default:
					if actualValue != expectedValue {
						t.Errorf("For key %s, expected %v, got %v", key, expectedValue, actualValue)
					}
				}
			}
		})
	}
}

func TestShouldRedact(t *testing.T) {
	logger := NewLogger()

	tests := []struct {
		value    string
		expected bool
	}{
		{"password", true},
		{"PASSWORD", true},
		{"api_key", true},
		{"api-key", true},
		{"secret", true},
		{"token", true},
		{"authorization", true},
		{"cookie", true},
		{"session", true},
		{"username", false},
		{"email", false},
		{"name", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			result := logger.shouldRedact(tt.value)
			if result != tt.expected {
				t.Errorf("shouldRedact(%s) = %v, expected %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestRedactEnvVars(t *testing.T) {
	logger := NewLogger()

	envVars := map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"API_KEY":      "secret123",
		"NODE_ENV":     "production",
	}

	result := logger.RedactEnvVars(envVars)

	for key := range envVars {
		if result[key] != "[REDACTED]" {
			t.Errorf("Expected env var %s to be redacted, got %s", key, result[key])
		}
	}
}