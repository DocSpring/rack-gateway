package email

import (
	"context"
	"fmt"
	"strings"

	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/riverqueue/river"
)

// SuspiciousActivityUserArgs contains parameters for suspicious activity (user) email notification
type SuspiciousActivityUserArgs struct {
	UserEmail string            `json:"user_email"`
	UserName  string            `json:"user_name"`
	Reason    string            `json:"reason"`
	IPAddress string            `json:"ip_address"`
	UserAgent string            `json:"user_agent"`
	Details   map[string]string `json:"details"`
}

// Kind returns the unique identifier for this job type
func (SuspiciousActivityUserArgs) Kind() string { return "email:security:suspicious_activity_user" }

// SuspiciousActivityUserWorker sends suspicious activity (user) email notifications
type SuspiciousActivityUserWorker struct {
	river.WorkerDefaults[SuspiciousActivityUserArgs]
	emailSender email.Sender
}

// NewSuspiciousActivityUserWorker creates a new suspicious activity user email worker
func NewSuspiciousActivityUserWorker(emailSender email.Sender) *SuspiciousActivityUserWorker {
	return &SuspiciousActivityUserWorker{emailSender: emailSender}
}

// Work sends the suspicious activity email to the user
func (w *SuspiciousActivityUserWorker) Work(ctx context.Context, job *river.Job[SuspiciousActivityUserArgs]) error {
	args := job.Args

	subject := "Suspicious Activity Detected"

	detailsText := formatDetailsAsText(args.Details)
	text := fmt.Sprintf(`Hello %s,

We detected suspicious activity on your account.

Reason: %s

Details:
- Time: %s
- IP Address: %s
- User Agent: %s
%s
If this wasn't you, please contact your administrator immediately and consider changing your password.

This is an automated security notification from Rack Gateway.`,
		args.UserName,
		args.Reason,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.IPAddress,
		args.UserAgent,
		detailsText,
	)

	detailsHTML := formatDetailsAsHTML(args.Details)
	html := fmt.Sprintf(`<p>Hello %s,</p>
<p>We detected suspicious activity on your account.</p>
<p><strong>Reason:</strong> %s</p>
<p><strong>Details:</strong></p>
<ul>
<li>Time: %s</li>
<li>IP Address: %s</li>
<li>User Agent: %s</li>
%s
</ul>
<p>If this wasn't you, please contact your administrator immediately and consider changing your password.</p>
<p><em>This is an automated security notification from Rack Gateway.</em></p>`,
		args.UserName,
		args.Reason,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.IPAddress,
		args.UserAgent,
		detailsHTML,
	)

	if err := w.emailSender.Send(args.UserEmail, subject, text, html); err != nil {
		return fmt.Errorf("failed to send suspicious activity user email: %w", err)
	}

	return nil
}

// SuspiciousActivityAdminArgs contains parameters for suspicious activity (admin alert) email notification
type SuspiciousActivityAdminArgs struct {
	AdminEmails []string          `json:"admin_emails"`
	UserEmail   string            `json:"user_email"`
	UserName    string            `json:"user_name"`
	Reason      string            `json:"reason"`
	IPAddress   string            `json:"ip_address"`
	UserAgent   string            `json:"user_agent"`
	Details     map[string]string `json:"details"`
}

// Kind returns the unique identifier for this job type
func (SuspiciousActivityAdminArgs) Kind() string { return "email:security:suspicious_activity_admin" }

// SuspiciousActivityAdminWorker sends suspicious activity (admin alert) email notifications
type SuspiciousActivityAdminWorker struct {
	river.WorkerDefaults[SuspiciousActivityAdminArgs]
	emailSender email.Sender
}

// NewSuspiciousActivityAdminWorker creates a new suspicious activity admin email worker
func NewSuspiciousActivityAdminWorker(emailSender email.Sender) *SuspiciousActivityAdminWorker {
	return &SuspiciousActivityAdminWorker{emailSender: emailSender}
}

// Work sends the suspicious activity alert to admins
func (w *SuspiciousActivityAdminWorker) Work(ctx context.Context, job *river.Job[SuspiciousActivityAdminArgs]) error {
	args := job.Args

	subject := fmt.Sprintf("Suspicious Activity - User %s", args.UserEmail)

	detailsText := formatDetailsAsText(args.Details)
	text := fmt.Sprintf(`Admin Alert: Suspicious activity detected.

User: %s (%s)
Reason: %s

Details:
- Time: %s
- IP Address: %s
- User Agent: %s
%s
This is an automated security notification from Rack Gateway.`,
		args.UserEmail,
		args.UserName,
		args.Reason,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.IPAddress,
		args.UserAgent,
		detailsText,
	)

	detailsHTML := formatDetailsAsHTML(args.Details)
	html := fmt.Sprintf(`<p><strong>Admin Alert: Suspicious activity detected.</strong></p>
<p><strong>User:</strong> %s (%s)</p>
<p><strong>Reason:</strong> %s</p>
<p><strong>Details:</strong></p>
<ul>
<li>Time: %s</li>
<li>IP Address: %s</li>
<li>User Agent: %s</li>
%s
</ul>
<p><em>This is an automated security notification from Rack Gateway.</em></p>`,
		args.UserEmail,
		args.UserName,
		args.Reason,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.IPAddress,
		args.UserAgent,
		detailsHTML,
	)

	if err := w.emailSender.SendMany(args.AdminEmails, subject, text, html); err != nil {
		return fmt.Errorf("failed to send suspicious activity admin emails: %w", err)
	}

	return nil
}

// formatDetailsAsText formats additional details map as plain text list items
func formatDetailsAsText(details map[string]string) string {
	if len(details) == 0 {
		return ""
	}
	var parts []string
	for k, v := range details {
		parts = append(parts, fmt.Sprintf("- %s: %s", k, v))
	}
	return "\n" + strings.Join(parts, "\n")
}

// formatDetailsAsHTML formats additional details map as HTML list items
func formatDetailsAsHTML(details map[string]string) string {
	if len(details) == 0 {
		return ""
	}
	var parts []string
	for k, v := range details {
		parts = append(parts, fmt.Sprintf("<li>%s: %s</li>", k, v))
	}
	return strings.Join(parts, "\n")
}
