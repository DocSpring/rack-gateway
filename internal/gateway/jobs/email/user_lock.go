package email

import (
	"context"

	"github.com/riverqueue/river"

	gtwemail "github.com/DocSpring/rack-gateway/internal/gateway/email"
)

// UserLockedArgs parameters for account locked notification
type UserLockedArgs struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// Kind returns the unique job type identifier for user locked notifications
func (UserLockedArgs) Kind() string { return "email:user:locked" }

// UserLockedWorker sends email notifications when a user account is locked
type UserLockedWorker struct {
	river.WorkerDefaults[UserLockedArgs]
	emailSender gtwemail.Sender
}

// NewUserLockedWorker creates a new worker for sending user locked notifications
func NewUserLockedWorker(emailSender gtwemail.Sender) *UserLockedWorker {
	return &UserLockedWorker{emailSender: emailSender}
}

// Work processes a user locked notification job and sends the email
func (w *UserLockedWorker) Work(_ context.Context, job *river.Job[UserLockedArgs]) error {
	args := job.Args
	subject := "Account Locked"
	text := "Your account has been locked by an administrator.\n\nReason: " + args.Reason +
		"\n\nPlease contact your administrator for assistance."
	html := "<p>Your account has been locked by an administrator.</p><p><strong>Reason:</strong> " +
		args.Reason + "</p><p>Please contact your administrator for assistance.</p>"
	return w.emailSender.Send(args.Email, subject, text, html)
}

// UserUnlockedArgs parameters for account unlocked notification
type UserUnlockedArgs struct {
	Email string `json:"email"`
}

// Kind returns the unique job type identifier for user unlocked notifications
func (UserUnlockedArgs) Kind() string { return "email:user:unlocked" }

// UserUnlockedWorker sends email notifications when a user account is unlocked
type UserUnlockedWorker struct {
	river.WorkerDefaults[UserUnlockedArgs]
	emailSender gtwemail.Sender
}

// NewUserUnlockedWorker creates a new worker for sending user unlocked notifications
func NewUserUnlockedWorker(emailSender gtwemail.Sender) *UserUnlockedWorker {
	return &UserUnlockedWorker{emailSender: emailSender}
}

// Work processes a user unlocked notification job and sends the email
func (w *UserUnlockedWorker) Work(_ context.Context, job *river.Job[UserUnlockedArgs]) error {
	args := job.Args
	subject := "Account Unlocked"
	text := "Your account has been unlocked by an administrator.\n\nYou can now log in again."
	html := "<p>Your account has been unlocked by an administrator.</p><p>You can now log in again.</p>"
	return w.emailSender.Send(args.Email, subject, text, html)
}
