package email

import (
	"context"

	"github.com/riverqueue/river"

	gtwemail "github.com/DocSpring/rack-gateway/internal/gateway/email"
)

// MFAAutoLockArgs parameters for MFA auto-lock email
type MFAAutoLockArgs struct {
	Email string `json:"email"`
}

// Kind returns the unique job identifier for MFA auto-lock email jobs.
func (MFAAutoLockArgs) Kind() string { return "email:security:mfa_auto_lock" }

// MFAAutoLockWorker handles sending MFA auto-lock notification emails.
type MFAAutoLockWorker struct {
	river.WorkerDefaults[MFAAutoLockArgs]
	emailSender gtwemail.Sender
}

// NewMFAAutoLockWorker constructs a new MFA auto-lock email worker.
func NewMFAAutoLockWorker(emailSender gtwemail.Sender) *MFAAutoLockWorker {
	return &MFAAutoLockWorker{emailSender: emailSender}
}

// Work sends an MFA auto-lock notification email to the user.
func (w *MFAAutoLockWorker) Work(_ context.Context, job *river.Job[MFAAutoLockArgs]) error {
	args := job.Args
	subject := "Account Locked - Multiple Failed Login Attempts"
	text := "Your account has been automatically locked due to multiple failed authentication attempts.\n\n" +
		"Reason: 5 failed MFA attempts in 5 minutes\n\n" +
		"If this was not you, please contact your administrator immediately.\n\n" +
		"For assistance, please contact your system administrator."
	html := `<p><strong>Your account has been automatically locked</strong> ` +
		`due to multiple failed authentication attempts.</p>
<p><strong>Reason:</strong> 5 failed MFA attempts in 5 minutes</p>
<p><strong style="color: #d9534f;">If this was not you, please contact your administrator immediately.</strong></p>
<p>For assistance, please contact your system administrator.</p>`
	return w.emailSender.Send(args.Email, subject, text, html)
}
