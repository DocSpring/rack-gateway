package email

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
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

type capturedEmail struct{ subject, text, html string }

type capturingSender struct{ sent []capturedEmail }

func (c *capturingSender) Send(_, subject, text, html string) error {
	c.sent = append(c.sent, capturedEmail{subject, text, html})
	return nil
}

func (c *capturingSender) SendMany(_ []string, subject, text, html string) error {
	c.sent = append(c.sent, capturedEmail{subject, text, html})
	return nil
}

func TestRateLimitAdminEmailEscapesRequestValues(t *testing.T) {
	sender := &capturingSender{}
	job := &river.Job[RateLimitAdminArgs]{
		JobRow: &rivertype.JobRow{CreatedAt: time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)},
		Args: RateLimitAdminArgs{
			AdminEmails: []string{"admin@example.com"},
			UserName:    `<b>Mallory</b>`,
			Path:        `/api/v1/auth/cli/start?<script>`,
			IPAddress:   "100.85.250.16",
			UserAgent:   `evil</body><a href="https://phish.example">click</a>`,
		},
	}
	require.NoError(t, NewRateLimitAdminWorker(sender).Work(context.Background(), job))

	require.Len(t, sender.sent, 1)
	html := sender.sent[0].html
	assert.NotContains(t, html, "<b>Mallory</b>")
	assert.NotContains(t, html, "<script>")
	assert.NotContains(t, html, "</body>")
	assert.NotContains(t, html, `<a href="https://phish.example">`)
	assert.Contains(t, html, "&lt;b&gt;Mallory&lt;/b&gt;")
	assert.Contains(t, html, "evil&lt;/body&gt;")
}
