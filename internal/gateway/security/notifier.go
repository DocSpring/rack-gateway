package security

import (
	"crypto/sha256"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// Notifier handles security event notifications via audit logs and email
type Notifier struct {
	emailSender   email.Sender
	auditLogger   *audit.Logger
	database      *db.Database
	adminEmails   []string
	emailsEnabled bool

	// Email rate limiting and deduplication
	mu              sync.Mutex
	recentEmails    map[string]time.Time // hash -> last sent time
	cleanupInterval time.Duration
	dedupWindow     time.Duration
	stopCleanup     chan struct{}
}

// NewNotifier creates a new security notifier
func NewNotifier(emailSender email.Sender, auditLogger *audit.Logger, database *db.Database, adminEmails []string) *Notifier {
	// Check if email sender is actually configured (not noop)
	emailsEnabled := false
	if emailSender != nil {
		// NoopSender is not useful for notifications
		if _, isNoop := emailSender.(email.NoopSender); !isNoop {
			emailsEnabled = true
		}
	}

	n := &Notifier{
		emailSender:     emailSender,
		auditLogger:     auditLogger,
		database:        database,
		adminEmails:     adminEmails,
		emailsEnabled:   emailsEnabled,
		recentEmails:    make(map[string]time.Time),
		cleanupInterval: 5 * time.Minute,
		dedupWindow:     15 * time.Minute, // Don't send same email within 15 minutes
		stopCleanup:     make(chan struct{}),
	}

	// Start cleanup goroutine to prevent memory leak
	go n.cleanupRecentEmails()

	return n
}

// Stop stops the cleanup goroutine
func (n *Notifier) Stop() {
	close(n.stopCleanup)
}

// cleanupRecentEmails removes old entries from the deduplication map
func (n *Notifier) cleanupRecentEmails() {
	ticker := time.NewTicker(n.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			n.mu.Lock()
			now := time.Now()
			for hash, lastSent := range n.recentEmails {
				if now.Sub(lastSent) > n.dedupWindow {
					delete(n.recentEmails, hash)
				}
			}
			n.mu.Unlock()
		case <-n.stopCleanup:
			return
		}
	}
}

// shouldSendEmail checks if we should send this email based on deduplication window
// Returns true if email should be sent, false if it was recently sent
func (n *Notifier) shouldSendEmail(recipient, subject, eventType string) bool {
	if !n.emailsEnabled {
		return false
	}

	// Create a hash of recipient + subject + event type for deduplication
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(recipient+subject+eventType)))

	n.mu.Lock()
	defer n.mu.Unlock()

	if lastSent, exists := n.recentEmails[hash]; exists {
		if time.Since(lastSent) < n.dedupWindow {
			// Email was sent recently, skip to avoid spam
			return false
		}
	}

	// Mark this email as sent
	n.recentEmails[hash] = time.Now()
	return true
}

// FailedMFAAttempt logs and notifies about failed MFA verification
func (n *Notifier) FailedMFAAttempt(userEmail, userName, ipAddress, userAgent string) {
	// Audit log
	if n.database != nil {
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   "auth",
			Action:       "mfa.verify.failed",
			ResourceType: "auth",
			Resource:     "mfa",
			Status:       "failed",
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":"mfa.verify.failed","error":%q}`, err)
		}
	}

	// Email notification to user (with rate limiting)
	if userEmail != "" && n.shouldSendEmail(userEmail, "Failed MFA Attempt", "mfa_failed") {
		subject := "Failed MFA Attempt on Your Account"
		text := fmt.Sprintf(`Hello,

We detected a failed multi-factor authentication attempt on your account.

Time: %s UTC
IP Address: %s
User Agent: %s

If this was you, you can safely ignore this message. If you did not attempt to log in, please contact your administrator immediately.

This is an automated security notification.`, time.Now().UTC().Format(time.RFC3339), ipAddress, userAgent)

		if err := n.emailSender.Send(userEmail, subject, text, ""); err != nil {
			log.Printf("failed to send failed MFA notification email to %s: %v", userEmail, err)
		}
	}
}

// LoginAttempt logs and notifies about login attempts
func (n *Notifier) LoginAttempt(userEmail, userName, channel, status, ipAddress, userAgent string, success bool) {
	// Audit log
	if n.database != nil {
		auditStatus := rbac.StatusStringSuccess
		if !success {
			auditStatus = rbac.StatusStringFailed
		}

		action := rbac.BuildAction(rbac.ResourceStringLogin, status)
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   rbac.ActionTypeAuth,
			Action:       action,
			ResourceType: rbac.ResourceStringAuth,
			Resource:     channel,
			Status:       auditStatus,
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":%q,"error":%q}`, action, err)
		}
	}

	// Email notification for failed logins (with rate limiting)
	if !success && userEmail != "" && n.shouldSendEmail(userEmail, "Failed Login Attempt", "login_failed") {
		subject := "Failed Login Attempt on Your Account"
		text := fmt.Sprintf(`Hello,

We detected a failed login attempt on your account.

Time: %s UTC
Channel: %s
IP Address: %s
User Agent: %s
Reason: %s

If this was you, please verify your credentials. If you did not attempt to log in, please contact your administrator immediately.

This is an automated security notification.`, time.Now().UTC().Format(time.RFC3339), channel, ipAddress, userAgent, status)

		if err := n.emailSender.Send(userEmail, subject, text, ""); err != nil {
			log.Printf("failed to send failed login notification email to %s: %v", userEmail, err)
		}
	}
}

// RateLimitExceeded logs and notifies about rate limit violations
func (n *Notifier) RateLimitExceeded(userEmail, userName, path, ipAddress, userAgent string) {
	// Audit log
	if n.database != nil {
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   "security",
			Action:       rbac.BuildAction(rbac.ResourceStringRateLimit, rbac.ActionStringExceeded),
			ResourceType: "security",
			Resource:     path,
			Status:       "denied",
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":rbac.BuildAction(rbac.ResourceStringRateLimit, rbac.ActionStringExceeded),"error":%q}`, err)
		}
	}

	// Email notification to user if known (with rate limiting)
	if userEmail != "" && n.shouldSendEmail(userEmail, "Rate Limit Exceeded", "rate_limit") {
		subject := "Rate Limit Exceeded on Your Account"
		text := fmt.Sprintf(`Hello,

Your account has exceeded the rate limit for authentication requests.

Time: %s UTC
Path: %s
IP Address: %s
User Agent: %s

This could indicate a security issue or misconfiguration. If you did not make these requests, please contact your administrator immediately.

This is an automated security notification.`, time.Now().UTC().Format(time.RFC3339), path, ipAddress, userAgent)

		if err := n.emailSender.Send(userEmail, subject, text, ""); err != nil {
			log.Printf("failed to send rate limit notification email to %s: %v", userEmail, err)
		}
	}

	// Also notify admins about rate limit violations (potential attack) - with deduplication
	// Use IP address in the hash so we deduplicate per-IP, not globally
	adminKey := fmt.Sprintf("admins-%s", ipAddress)
	if len(n.adminEmails) > 0 && n.shouldSendEmail(adminKey, "Rate Limit Exceeded", "rate_limit_admin") {
		subject := "Rate Limit Exceeded - Security Alert"
		text := fmt.Sprintf(`Security Alert: Rate limit exceeded

Time: %s UTC
Path: %s
User: %s (%s)
IP Address: %s
User Agent: %s

This could indicate:
- Legitimate user with misconfigured client
- Brute force attack attempt
- API abuse

Please investigate immediately.

This is an automated security notification.`, time.Now().UTC().Format(time.RFC3339), path, userEmail, userName, ipAddress, userAgent)

		if err := n.emailSender.SendMany(n.adminEmails, subject, text, ""); err != nil {
			log.Printf("failed to send rate limit admin notification: %v", err)
		}
	}
}

// SuspiciousActivity logs and notifies about suspicious login patterns
func (n *Notifier) SuspiciousActivity(userEmail, userName, reason, ipAddress, userAgent string, details map[string]string) {
	// Audit log
	if n.database != nil {
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   "security",
			Action:       "suspicious_activity",
			ResourceType: "security",
			Resource:     reason,
			Status:       "alert",
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":"suspicious_activity","error":%q}`, err)
		}
	}

	// Email notification to user (with rate limiting)
	if userEmail != "" && n.shouldSendEmail(userEmail, "Suspicious Activity", "suspicious_activity") {
		subject := "Suspicious Activity Detected on Your Account"
		text := fmt.Sprintf(`Hello,

We detected suspicious activity on your account.

Time: %s UTC
Reason: %s
IP Address: %s
User Agent: %s

If this was you, you can safely ignore this message. If you did not perform this action, please contact your administrator immediately and consider changing your password.

This is an automated security notification.`, time.Now().UTC().Format(time.RFC3339), reason, ipAddress, userAgent)

		if err := n.emailSender.Send(userEmail, subject, text, ""); err != nil {
			log.Printf("failed to send suspicious activity notification email to %s: %v", userEmail, err)
		}
	}

	// Notify admins about suspicious activity (with deduplication per user+reason)
	adminKey := fmt.Sprintf("admins-%s-%s", userEmail, reason)
	if len(n.adminEmails) > 0 && n.shouldSendEmail(adminKey, "Suspicious Activity", "suspicious_activity_admin") {
		detailsStr := ""
		for k, v := range details {
			detailsStr += fmt.Sprintf("%s: %s\n", k, v)
		}

		subject := "Suspicious Activity Detected - Security Alert"
		text := fmt.Sprintf(`Security Alert: Suspicious activity detected

Time: %s UTC
User: %s (%s)
Reason: %s
IP Address: %s
User Agent: %s

Additional Details:
%s

Please investigate immediately.

This is an automated security notification.`, time.Now().UTC().Format(time.RFC3339), userEmail, userName, reason, ipAddress, userAgent, detailsStr)

		if err := n.emailSender.SendMany(n.adminEmails, subject, text, ""); err != nil {
			log.Printf("failed to send suspicious activity admin notification: %v", err)
		}
	}
}
