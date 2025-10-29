package security

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobemail "github.com/DocSpring/rack-gateway/internal/gateway/jobs/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
	"github.com/riverqueue/river"
)

// Notifier handles security event notifications via audit logs and email
type Notifier struct {
	emailSender   email.Sender
	auditLogger   *audit.Logger
	database      *db.Database
	adminEmails   []string
	emailsEnabled bool
	jobsClient    *jobs.Client

	// Email rate limiting and deduplication
	mu              sync.Mutex
	recentEmails    map[string]time.Time // hash -> last sent time
	cleanupInterval time.Duration
	dedupWindow     time.Duration
	stopCleanup     chan struct{}
}

// NewNotifier creates a new security notifier
func NewNotifier(emailSender email.Sender, auditLogger *audit.Logger, database *db.Database, adminEmails []string, jobsClient *jobs.Client) *Notifier {
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
		jobsClient:      jobsClient,
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
// enqueueSecurityNotification enqueues a security notification if deduplication allows
func (n *Notifier) enqueueSecurityNotification(recipient, subject, eventType string, args river.JobArgs) {
	if recipient == "" && !strings.HasPrefix(eventType, "admin") {
		return
	}
	if !n.shouldSendEmail(recipient, subject, eventType) {
		return
	}
	if n.jobsClient == nil {
		log.Printf("WARNING: security notification dropped (no jobs client configured): type=%s, recipient=%s", eventType, recipient)
		return
	}

	_, err := n.jobsClient.Insert(context.Background(), args, &river.InsertOpts{
		Queue:       jobs.QueueSecurity,
		MaxAttempts: jobs.MaxAttemptsNotification,
	})
	if err != nil {
		log.Printf("failed to enqueue %s notification: %v", eventType, err)
	}
}

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
		if n.jobsClient != nil {
			_, err := n.jobsClient.Insert(context.Background(), jobemail.FailedMFAArgs{
				UserEmail: userEmail,
				UserName:  userName,
				IPAddress: ipAddress,
				UserAgent: userAgent,
			}, &river.InsertOpts{
				Queue:       jobs.QueueSecurity,
				MaxAttempts: jobs.MaxAttemptsNotification,
			})
			if err != nil {
				log.Printf("failed to enqueue failed MFA notification email to %s: %v", userEmail, err)
			}
		}
	}
}

// LoginAttempt logs and notifies about login attempts
func (n *Notifier) LoginAttempt(userEmail, userName, channel, status, ipAddress, userAgent string, success bool) {
	// Audit log
	if n.database != nil {
		auditStatus := audit.StatusSuccess
		if !success {
			auditStatus = audit.StatusFailed
		}

		action := audit.BuildAction(audit.ActionScopeLogin, status)
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   audit.ActionTypeAuth,
			Action:       action,
			ResourceType: rbac.ResourceAuth.String(),
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
		if n.jobsClient != nil {
			_, err := n.jobsClient.Insert(context.Background(), jobemail.FailedLoginArgs{
				UserEmail: userEmail,
				UserName:  userName,
				Channel:   channel,
				Status:    status,
				IPAddress: ipAddress,
				UserAgent: userAgent,
			}, &river.InsertOpts{
				Queue:       jobs.QueueSecurity,
				MaxAttempts: jobs.MaxAttemptsNotification,
			})
			if err != nil {
				log.Printf("failed to enqueue failed login notification email to %s: %v", userEmail, err)
			}
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
			Action:       audit.BuildAction(audit.ActionScopeRateLimit, audit.ActionVerbExceeded),
			ResourceType: "security",
			Resource:     path,
			Status:       "denied",
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":audit.BuildAction(audit.ActionScopeRateLimit, audit.ActionVerbExceeded),"error":%q}`, err)
		}
	}

	// Email notification to user if known (with rate limiting)
	n.enqueueSecurityNotification(userEmail, "Rate Limit Exceeded", "rate_limit", jobemail.RateLimitUserArgs{
		UserEmail: userEmail,
		UserName:  userName,
		Path:      path,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	})

	// Also notify admins about rate limit violations (potential attack) - with deduplication
	// Use IP address in the hash so we deduplicate per-IP, not globally
	adminKey := fmt.Sprintf("admins-%s", ipAddress)
	n.enqueueSecurityNotification(adminKey, "Rate Limit Exceeded", "rate_limit_admin", jobemail.RateLimitAdminArgs{
		AdminEmails: n.adminEmails,
		UserEmail:   userEmail,
		UserName:    userName,
		Path:        path,
		IPAddress:   ipAddress,
		UserAgent:   userAgent,
	})
}

// SuspiciousActivity logs and notifies about suspicious login patterns
func (n *Notifier) SuspiciousActivity(userEmail, userName, reason, ipAddress, userAgent string, details map[string]string) {
	// Audit log
	if n.database != nil {
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   audit.ActionTypeSecurity,
			Action:       audit.ActionScopeSuspiciousActivity,
			ResourceType: audit.ActionScopeRateLimit,
			Resource:     reason,
			Status:       audit.StatusAlert,
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":%q,"error":%q}`, audit.ActionScopeSuspiciousActivity, err)
		}
	}

	// Email notification to user (with rate limiting)
	n.enqueueSecurityNotification(userEmail, "Suspicious Activity", "suspicious_activity", jobemail.SuspiciousActivityUserArgs{
		UserEmail: userEmail,
		UserName:  userName,
		Reason:    reason,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Details:   details,
	})

	// Notify admins about suspicious activity (with deduplication per user+reason)
	adminKey := fmt.Sprintf("admins-%s-%s", userEmail, reason)
	n.enqueueSecurityNotification(adminKey, "Suspicious Activity", "suspicious_activity_admin", jobemail.SuspiciousActivityAdminArgs{
		AdminEmails: n.adminEmails,
		UserEmail:   userEmail,
		UserName:    userName,
		Reason:      reason,
		IPAddress:   ipAddress,
		UserAgent:   userAgent,
		Details:     details,
	})
}
