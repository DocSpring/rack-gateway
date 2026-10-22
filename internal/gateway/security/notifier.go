package security

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobemail "github.com/DocSpring/rack-gateway/internal/gateway/jobs/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/rbac"
)

// jobInserter queues jobs (*jobs.Client).
type jobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// insertTimeout bounds queueing an email's job, so a database that doesn't answer can't hold up the request that
// raised the alert.
const insertTimeout = 10 * time.Second

// Notifier handles security event notifications via audit logs and email
type Notifier struct {
	auditLogger *audit.Logger
	database    *db.Database
	// jobs queues the emails (nil: none are sent).
	jobs jobInserter

	// Email rate limiting and deduplication
	mu              sync.Mutex
	recentEmails    map[string]time.Time // hash -> when its email was queued, or is being queued
	cleanupInterval time.Duration
	dedupWindow     time.Duration
	stopCleanup     chan struct{}
}

// NewNotifier creates a new security notifier. Its emails are sent by jobs on jobsClient.
func NewNotifier(auditLogger *audit.Logger, database *db.Database, jobsClient *jobs.Client) *Notifier {
	n := &Notifier{
		auditLogger:     auditLogger,
		database:        database,
		recentEmails:    make(map[string]time.Time),
		cleanupInterval: 5 * time.Minute,
		dedupWindow:     15 * time.Minute, // Don't send same email within 15 minutes
		stopCleanup:     make(chan struct{}),
	}

	if jobsClient != nil {
		n.jobs = jobsClient
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

// admins returns the admins to alert, looked up now so admins added or removed since startup are respected.
func (n *Notifier) admins() []string {
	if n.database == nil {
		return nil
	}
	emails, err := n.database.UnlockedAdminEmails()
	if err != nil {
		log.Printf(`{"level":"error","event":"admin_lookup_failed","error":%q}`, err)
		return nil
	}
	return emails
}

// enqueueSecurityNotification queues a security email, unless the same one (recipient, subject and event type) was
// queued within the dedup window. It only counts as sent once its job is queued: when the insert fails, the next
// alert tries again.
func (n *Notifier) enqueueSecurityNotification(recipient, subject, eventType string, args river.JobArgs) {
	if recipient == "" && !strings.HasPrefix(eventType, "admin") {
		return
	}
	if n.jobs == nil {
		log.Printf(
			"WARNING: security notification dropped (no jobs client configured): type=%s, recipient=%s",
			eventType,
			recipient,
		)
		return
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(recipient+subject+eventType)))
	if !n.claim(key) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), insertTimeout)
	defer cancel()
	_, err := n.jobs.Insert(ctx, args, &river.InsertOpts{
		Queue:       jobs.QueueSecurity,
		MaxAttempts: jobs.MaxAttemptsNotification,
	})
	if err != nil {
		n.release(key)
		log.Printf("failed to enqueue %s notification: %v", eventType, err)
	}
}

// claim reserves key for an email being queued now. It returns false when one was queued, or is being queued,
// within the dedup window.
func (n *Notifier) claim(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if at, exists := n.recentEmails[key]; exists && time.Since(at) < n.dedupWindow {
		return false
	}
	n.recentEmails[key] = time.Now()
	return true
}

// release gives up key's claim after its email couldn't be queued.
func (n *Notifier) release(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.recentEmails, key)
}

// NewCLISession emails the user when a CLI session is created for their account, so a login they
// did not start is noticed. Repeats are suppressed per user and client IP (the device name is chosen
// by the client, so it can't decide whether the email is sent).
func (n *Notifier) NewCLISession(userEmail, userName, deviceName, ipAddress, userAgent string) {
	n.enqueueSecurityNotification(userEmail, "New CLI Login", "cli_session:"+ipAddress, jobemail.NewCLISessionArgs{
		UserEmail:  userEmail,
		UserName:   userName,
		DeviceName: deviceName,
		IPAddress:  ipAddress,
		UserAgent:  userAgent,
	})
}

// FailedMFAAttempt logs and notifies about failed MFA verification
func (n *Notifier) FailedMFAAttempt(userEmail, userName, ipAddress, userAgent string) {
	// Audit log
	if n.database != nil {
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   audit.ActionTypeAuth,
			Action:       audit.ActionMFAVerifyFailed,
			ResourceType: "auth",
			Resource:     "mfa",
			Status:       "failed",
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			log.Printf(`{"level":"error","event":"audit_log_failed","action":%q,"error":%q}`,
				audit.ActionMFAVerifyFailed, err)
		}
	}

	// Email notification to user (with rate limiting)
	n.enqueueSecurityNotification(userEmail, "Failed MFA Attempt", "mfa_failed", jobemail.FailedMFAArgs{
		UserEmail: userEmail,
		UserName:  userName,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	})
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
	if !success {
		n.enqueueSecurityNotification(userEmail, "Failed Login Attempt", "login_failed", jobemail.FailedLoginArgs{
			UserEmail: userEmail,
			UserName:  userName,
			Channel:   channel,
			Status:    status,
			IPAddress: ipAddress,
			UserAgent: userAgent,
		})
	}
}

// RateLimitExceeded logs and notifies about rate limit violations
func (n *Notifier) RateLimitExceeded(userEmail, userName, path, ipAddress, userAgent string) {
	// Audit log
	if n.database != nil {
		if err := n.auditLogger.LogDBEntry(&db.AuditLog{
			UserEmail:    userEmail,
			UserName:     userName,
			ActionType:   audit.ActionTypeSecurity,
			Action:       audit.BuildAction(audit.ActionScopeRateLimit, audit.ActionVerbExceeded),
			ResourceType: "security",
			Resource:     path,
			Status:       "denied",
			IPAddress:    ipAddress,
			UserAgent:    userAgent,
		}); err != nil {
			action := audit.BuildAction(audit.ActionScopeRateLimit, audit.ActionVerbExceeded)
			log.Printf(
				`{"level":"error","event":"audit_log_failed","action":%q,"error":%q}`,
				action,
				err,
			)
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
	admins := n.admins()
	if len(admins) == 0 {
		return
	}
	adminKey := fmt.Sprintf("admins-%s", ipAddress)
	n.enqueueSecurityNotification(adminKey, "Rate Limit Exceeded", "rate_limit_admin", jobemail.RateLimitAdminArgs{
		AdminEmails: admins,
		UserEmail:   userEmail,
		UserName:    userName,
		Path:        path,
		IPAddress:   ipAddress,
		UserAgent:   userAgent,
	})
}
