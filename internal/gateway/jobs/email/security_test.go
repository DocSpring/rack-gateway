package email

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test FailedMFAArgs.Kind
func TestFailedMFAArgs_Kind(t *testing.T) {
	args := FailedMFAArgs{
		UserEmail: "test@example.com",
		UserName:  "Test User",
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
	}
	assert.Equal(t, "email:security:failed_mfa", args.Kind())
}

// Test NewFailedMFAWorker
func TestNewFailedMFAWorker(t *testing.T) {
	worker := NewFailedMFAWorker(nil)
	require.NotNil(t, worker)
}

// Test FailedLoginArgs.Kind
func TestFailedLoginArgs_Kind(t *testing.T) {
	args := FailedLoginArgs{
		UserEmail: "test@example.com",
		UserName:  "Test User",
		Channel:   "web",
		Status:    "failed",
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
	}
	assert.Equal(t, "email:security:failed_login", args.Kind())
}

// Test NewFailedLoginWorker
func TestNewFailedLoginWorker(t *testing.T) {
	worker := NewFailedLoginWorker(nil)
	require.NotNil(t, worker)
}

// Test RateLimitUserArgs.Kind
func TestRateLimitUserArgs_Kind(t *testing.T) {
	args := RateLimitUserArgs{
		UserEmail: "test@example.com",
		UserName:  "Test User",
		Path:      "/api/v1/apps",
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
	}
	assert.Equal(t, "email:security:rate_limit_user", args.Kind())
}

// Test NewRateLimitUserWorker
func TestNewRateLimitUserWorker(t *testing.T) {
	worker := NewRateLimitUserWorker(nil)
	require.NotNil(t, worker)
}

// Test RateLimitAdminArgs.Kind
func TestRateLimitAdminArgs_Kind(t *testing.T) {
	args := RateLimitAdminArgs{
		AdminEmails: []string{"admin@example.com"},
		UserEmail:   "test@example.com",
		UserName:    "Test User",
		Path:        "/api/v1/apps",
		IPAddress:   "192.168.1.1",
		UserAgent:   "Mozilla/5.0",
	}
	assert.Equal(t, "email:security:rate_limit_admin", args.Kind())
}

// Test NewRateLimitAdminWorker
func TestNewRateLimitAdminWorker(t *testing.T) {
	worker := NewRateLimitAdminWorker(nil)
	require.NotNil(t, worker)
}
