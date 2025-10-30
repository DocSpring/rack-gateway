package email

import (
	"context"

	gtwemail "github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/riverqueue/river"
)

// UserLockedArgs parameters for account locked notification
type UserLockedArgs struct {
    Email  string `json:"email"`
    Reason string `json:"reason"`
}

func (UserLockedArgs) Kind() string { return "email:user:locked" }

type UserLockedWorker struct {
    river.WorkerDefaults[UserLockedArgs]
    emailSender gtwemail.Sender
}

func NewUserLockedWorker(emailSender gtwemail.Sender) *UserLockedWorker {
    return &UserLockedWorker{emailSender: emailSender}
}

func (w *UserLockedWorker) Work(ctx context.Context, job *river.Job[UserLockedArgs]) error {
    args := job.Args
    subject := "Account Locked"
    text := "Your account has been locked by an administrator.\n\nReason: " + args.Reason + "\n\nPlease contact your administrator for assistance."
    html := "<p>Your account has been locked by an administrator.</p><p><strong>Reason:</strong> " + args.Reason + "</p><p>Please contact your administrator for assistance.</p>"
    return w.emailSender.Send(args.Email, subject, text, html)
}

// UserUnlockedArgs parameters for account unlocked notification
type UserUnlockedArgs struct {
    Email string `json:"email"`
}

func (UserUnlockedArgs) Kind() string { return "email:user:unlocked" }

type UserUnlockedWorker struct {
    river.WorkerDefaults[UserUnlockedArgs]
    emailSender gtwemail.Sender
}

func NewUserUnlockedWorker(emailSender gtwemail.Sender) *UserUnlockedWorker {
    return &UserUnlockedWorker{emailSender: emailSender}
}

func (w *UserUnlockedWorker) Work(ctx context.Context, job *river.Job[UserUnlockedArgs]) error {
    args := job.Args
    subject := "Account Unlocked"
    text := "Your account has been unlocked by an administrator.\n\nYou can now log in again."
    html := "<p>Your account has been unlocked by an administrator.</p><p>You can now log in again.</p>"
    return w.emailSender.Send(args.Email, subject, text, html)
}


