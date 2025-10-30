package email

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test SuspiciousActivityUserArgs.Kind
func TestSuspiciousActivityUserArgs_Kind(t *testing.T) {
	args := SuspiciousActivityUserArgs{
		UserEmail: "test@example.com",
		UserName:  "Test User",
		Reason:    "Multiple failed login attempts",
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
		Details:   map[string]string{"attempts": "5"},
	}
	assert.Equal(t, "email:security:suspicious_activity_user", args.Kind())
}

// Test NewSuspiciousActivityUserWorker
func TestNewSuspiciousActivityUserWorker(t *testing.T) {
	worker := NewSuspiciousActivityUserWorker(nil)
	require.NotNil(t, worker)
}

// Test SuspiciousActivityAdminArgs.Kind
func TestSuspiciousActivityAdminArgs_Kind(t *testing.T) {
	args := SuspiciousActivityAdminArgs{
		AdminEmails: []string{"admin@example.com"},
		UserEmail:   "test@example.com",
		UserName:    "Test User",
		Reason:      "Multiple failed login attempts",
		IPAddress:   "192.168.1.1",
		UserAgent:   "Mozilla/5.0",
		Details:     map[string]string{"attempts": "5"},
	}
	assert.Equal(t, "email:security:suspicious_activity_admin", args.Kind())
}

// Test NewSuspiciousActivityAdminWorker
func TestNewSuspiciousActivityAdminWorker(t *testing.T) {
	worker := NewSuspiciousActivityAdminWorker(nil)
	require.NotNil(t, worker)
}

// Test formatDetailsAsText - Empty
func TestFormatDetailsAsText_Empty(t *testing.T) {
	result := formatDetailsAsText(nil)
	assert.Equal(t, "", result)

	result = formatDetailsAsText(map[string]string{})
	assert.Equal(t, "", result)
}

// Test formatDetailsAsText - With Data
func TestFormatDetailsAsText_WithData(t *testing.T) {
	details := map[string]string{
		"attempts": "5",
		"window":   "10m",
	}
	result := formatDetailsAsText(details)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "attempts: 5")
	assert.Contains(t, result, "window: 10m")
}

// Test formatDetailsAsHTML - Empty
func TestFormatDetailsAsHTML_Empty(t *testing.T) {
	result := formatDetailsAsHTML(nil)
	assert.Equal(t, "", result)

	result = formatDetailsAsHTML(map[string]string{})
	assert.Equal(t, "", result)
}

// Test formatDetailsAsHTML - With Data
func TestFormatDetailsAsHTML_WithData(t *testing.T) {
	details := map[string]string{
		"attempts": "5",
		"window":   "10m",
	}
	result := formatDetailsAsHTML(details)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "<li>attempts: 5</li>")
	assert.Contains(t, result, "<li>window: 10m</li>")
}
