package email

import (
	"context"
	"fmt"
	"html"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/email"
)

// NewCLISessionArgs contains parameters for the new CLI session email notification.
type NewCLISessionArgs struct {
	UserEmail  string `json:"user_email"`
	UserName   string `json:"user_name"`
	DeviceName string `json:"device_name"`
	IPAddress  string `json:"ip_address"`
	UserAgent  string `json:"user_agent"`
}

// Kind returns the unique identifier for this job type
func (NewCLISessionArgs) Kind() string { return "email:security:new_cli_session" }

// NewCLISessionWorker emails a user whenever a CLI session is created for their account.
type NewCLISessionWorker struct {
	river.WorkerDefaults[NewCLISessionArgs]
	emailSender email.Sender
}

// NewNewCLISessionWorker creates a new CLI session email worker
func NewNewCLISessionWorker(emailSender email.Sender) *NewCLISessionWorker {
	return &NewCLISessionWorker{emailSender: emailSender}
}

// Work sends the new CLI session email
func (w *NewCLISessionWorker) Work(_ context.Context, job *river.Job[NewCLISessionArgs]) error {
	args := job.Args
	when := job.CreatedAt.Format("2006-01-02 15:04:05 MST")

	subject := "New CLI Login"
	text := fmt.Sprintf(`Hello %s,

A new rack-gateway CLI session was created for your account.

Details:
- Time: %s
- Device: %s
- IP Address: %s
- User Agent: %s

If you did not just run "rack-gateway login", revoke the session in the web UI and contact your administrator.

This is an automated security notification from Rack Gateway.`,
		args.UserName, when, args.DeviceName, args.IPAddress, args.UserAgent,
	)

	body := fmt.Sprintf(`<p>Hello %s,</p>
<p>A new rack-gateway CLI session was created for your account.</p>
<p><strong>Details:</strong></p>
<ul>
<li>Time: %s</li>
<li>Device: %s</li>
<li>IP Address: %s</li>
<li>User Agent: %s</li>
</ul>
<p>If you did not just run <code>rack-gateway login</code>, revoke the session in the web UI and contact
your administrator.</p>
<p><em>This is an automated security notification from Rack Gateway.</em></p>`,
		html.EscapeString(args.UserName),
		html.EscapeString(when),
		html.EscapeString(args.DeviceName),
		html.EscapeString(args.IPAddress),
		html.EscapeString(args.UserAgent),
	)

	if err := w.emailSender.Send(args.UserEmail, subject, text, body); err != nil {
		return fmt.Errorf("failed to send new CLI session email: %w", err)
	}
	return nil
}
