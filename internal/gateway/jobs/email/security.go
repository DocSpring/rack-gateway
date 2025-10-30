package email

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/email"
)

// FailedMFAArgs contains parameters for failed MFA attempt email notification
type FailedMFAArgs struct {
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name"`
	IPAddress string `json:"ip_address"`
	UserAgent string `json:"user_agent"`
}

// Kind returns the unique identifier for this job type
func (FailedMFAArgs) Kind() string { return "email:security:failed_mfa" }

// FailedMFAWorker sends failed MFA attempt email notifications
type FailedMFAWorker struct {
	river.WorkerDefaults[FailedMFAArgs]
	emailSender email.Sender
}

// NewFailedMFAWorker creates a new failed MFA email worker
func NewFailedMFAWorker(emailSender email.Sender) *FailedMFAWorker {
	return &FailedMFAWorker{emailSender: emailSender}
}

// Work sends the failed MFA attempt email
func (w *FailedMFAWorker) Work(_ context.Context, job *river.Job[FailedMFAArgs]) error {
	args := job.Args

	subject := "Failed MFA Verification Attempt"
	text := fmt.Sprintf(`Hello %s,

We detected a failed multi-factor authentication attempt on your account.

Details:
- Time: %s
- IP Address: %s
- User Agent: %s

If this wasn't you, please contact your administrator immediately.

This is an automated security notification from Rack Gateway.`,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.IPAddress,
		args.UserAgent,
	)

	html := fmt.Sprintf(`<p>Hello %s,</p>
<p>We detected a failed multi-factor authentication attempt on your account.</p>
<p><strong>Details:</strong></p>
<ul>
<li>Time: %s</li>
<li>IP Address: %s</li>
<li>User Agent: %s</li>
</ul>
<p>If this wasn't you, please contact your administrator immediately.</p>
<p><em>This is an automated security notification from Rack Gateway.</em></p>`,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.IPAddress,
		args.UserAgent,
	)

	if err := w.emailSender.Send(args.UserEmail, subject, text, html); err != nil {
		return fmt.Errorf("failed to send failed MFA email: %w", err)
	}

	return nil
}

// FailedLoginArgs contains parameters for failed login attempt email notification
type FailedLoginArgs struct {
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name"`
	Channel   string `json:"channel"`
	Status    string `json:"status"`
	IPAddress string `json:"ip_address"`
	UserAgent string `json:"user_agent"`
}

// Kind returns the unique identifier for this job type
func (FailedLoginArgs) Kind() string { return "email:security:failed_login" }

// FailedLoginWorker sends failed login attempt email notifications
type FailedLoginWorker struct {
	river.WorkerDefaults[FailedLoginArgs]
	emailSender email.Sender
}

// NewFailedLoginWorker creates a new failed login email worker
func NewFailedLoginWorker(emailSender email.Sender) *FailedLoginWorker {
	return &FailedLoginWorker{emailSender: emailSender}
}

// Work sends the failed login attempt email
func (w *FailedLoginWorker) Work(_ context.Context, job *river.Job[FailedLoginArgs]) error {
	args := job.Args

	subject := "Failed Login Attempt"
	text := fmt.Sprintf(`Hello %s,

We detected a failed login attempt on your account.

Details:
- Time: %s
- Channel: %s
- Status: %s
- IP Address: %s
- User Agent: %s

If this wasn't you, please contact your administrator immediately.

This is an automated security notification from Rack Gateway.`,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.Channel,
		args.Status,
		args.IPAddress,
		args.UserAgent,
	)

	html := fmt.Sprintf(`<p>Hello %s,</p>
<p>We detected a failed login attempt on your account.</p>
<p><strong>Details:</strong></p>
<ul>
<li>Time: %s</li>
<li>Channel: %s</li>
<li>Status: %s</li>
<li>IP Address: %s</li>
<li>User Agent: %s</li>
</ul>
<p>If this wasn't you, please contact your administrator immediately.</p>
<p><em>This is an automated security notification from Rack Gateway.</em></p>`,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.Channel,
		args.Status,
		args.IPAddress,
		args.UserAgent,
	)

	if err := w.emailSender.Send(args.UserEmail, subject, text, html); err != nil {
		return fmt.Errorf("failed to send failed login email: %w", err)
	}

	return nil
}

// RateLimitUserArgs contains parameters for rate limit exceeded (user) email notification
type RateLimitUserArgs struct {
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name"`
	Path      string `json:"path"`
	IPAddress string `json:"ip_address"`
	UserAgent string `json:"user_agent"`
}

// Kind returns the unique identifier for this job type
func (RateLimitUserArgs) Kind() string { return "email:security:rate_limit_user" }

// RateLimitUserWorker sends rate limit exceeded (user) email notifications
type RateLimitUserWorker struct {
	river.WorkerDefaults[RateLimitUserArgs]
	emailSender email.Sender
}

// NewRateLimitUserWorker creates a new rate limit user email worker
func NewRateLimitUserWorker(emailSender email.Sender) *RateLimitUserWorker {
	return &RateLimitUserWorker{emailSender: emailSender}
}

// Work sends the rate limit exceeded email to the user
func (w *RateLimitUserWorker) Work(_ context.Context, job *river.Job[RateLimitUserArgs]) error {
	args := job.Args

	subject := "Rate Limit Exceeded"
	text := fmt.Sprintf(`Hello %s,

Your account has exceeded the rate limit.

Details:
- Time: %s
- Path: %s
- IP Address: %s
- User Agent: %s

Please slow down your requests. If you believe this is an error, contact your administrator.

This is an automated security notification from Rack Gateway.`,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.Path,
		args.IPAddress,
		args.UserAgent,
	)

	html := fmt.Sprintf(`<p>Hello %s,</p>
<p>Your account has exceeded the rate limit.</p>
<p><strong>Details:</strong></p>
<ul>
<li>Time: %s</li>
<li>Path: %s</li>
<li>IP Address: %s</li>
<li>User Agent: %s</li>
</ul>
<p>Please slow down your requests. If you believe this is an error, contact your administrator.</p>
<p><em>This is an automated security notification from Rack Gateway.</em></p>`,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.Path,
		args.IPAddress,
		args.UserAgent,
	)

	if err := w.emailSender.Send(args.UserEmail, subject, text, html); err != nil {
		return fmt.Errorf("failed to send rate limit user email: %w", err)
	}

	return nil
}

// RateLimitAdminArgs contains parameters for rate limit exceeded (admin alert) email notification
type RateLimitAdminArgs struct {
	AdminEmails []string `json:"admin_emails"`
	UserEmail   string   `json:"user_email"`
	UserName    string   `json:"user_name"`
	Path        string   `json:"path"`
	IPAddress   string   `json:"ip_address"`
	UserAgent   string   `json:"user_agent"`
}

// Kind returns the unique identifier for this job type
func (RateLimitAdminArgs) Kind() string { return "email:security:rate_limit_admin" }

// RateLimitAdminWorker sends rate limit exceeded (admin alert) email notifications
type RateLimitAdminWorker struct {
	river.WorkerDefaults[RateLimitAdminArgs]
	emailSender email.Sender
}

// NewRateLimitAdminWorker creates a new rate limit admin email worker
func NewRateLimitAdminWorker(emailSender email.Sender) *RateLimitAdminWorker {
	return &RateLimitAdminWorker{emailSender: emailSender}
}

// Work sends the rate limit exceeded alert to admins
func (w *RateLimitAdminWorker) Work(_ context.Context, job *river.Job[RateLimitAdminArgs]) error {
	args := job.Args

	subject := fmt.Sprintf("Rate Limit Exceeded - User %s", args.UserEmail)
	text := fmt.Sprintf(`Admin Alert: Rate limit exceeded by user.

User: %s (%s)
Time: %s
Path: %s
IP Address: %s
User Agent: %s

This is an automated security notification from Rack Gateway.`,
		args.UserEmail,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.Path,
		args.IPAddress,
		args.UserAgent,
	)

	html := fmt.Sprintf(`<p><strong>Admin Alert: Rate limit exceeded by user.</strong></p>
<p><strong>User:</strong> %s (%s)</p>
<p><strong>Details:</strong></p>
<ul>
<li>Time: %s</li>
<li>Path: %s</li>
<li>IP Address: %s</li>
<li>User Agent: %s</li>
</ul>
<p><em>This is an automated security notification from Rack Gateway.</em></p>`,
		args.UserEmail,
		args.UserName,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
		args.Path,
		args.IPAddress,
		args.UserAgent,
	)

	if err := w.emailSender.SendMany(args.AdminEmails, subject, text, html); err != nil {
		return fmt.Errorf("failed to send rate limit admin emails: %w", err)
	}

	return nil
}
