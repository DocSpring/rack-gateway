package email

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	gtwemail "github.com/DocSpring/rack-gateway/internal/gateway/email"
)

// TokenCreatedOwnerArgs contains parameters for notifying the token owner
type TokenCreatedOwnerArgs struct {
	OwnerEmail   string `json:"owner_email"`
	TokenName    string `json:"token_name"`
	CreatorLabel string `json:"creator_label"`
	Rack         string `json:"rack"`
}

// Kind returns the unique identifier for this job type
func (TokenCreatedOwnerArgs) Kind() string { return "email:token:created_owner" }

// TokenCreatedOwnerWorker sends an email to the token owner
type TokenCreatedOwnerWorker struct {
	river.WorkerDefaults[TokenCreatedOwnerArgs]
	emailSender gtwemail.Sender
}

// NewTokenCreatedOwnerWorker creates a new worker instance
func NewTokenCreatedOwnerWorker(emailSender gtwemail.Sender) *TokenCreatedOwnerWorker {
	return &TokenCreatedOwnerWorker{emailSender: emailSender}
}

// Work sends the token created email to the owner
func (w *TokenCreatedOwnerWorker) Work(_ context.Context, job *river.Job[TokenCreatedOwnerArgs]) error {
	args := job.Args
	if args.OwnerEmail == "" {
		return nil
	}
	subject := fmt.Sprintf("Rack Gateway (%s): New API token created", args.Rack)
	text := fmt.Sprintf("A new API token '%s' was created for your account by %s.", args.TokenName, args.CreatorLabel)
	html := ""
	if err := w.emailSender.Send(args.OwnerEmail, subject, text, html); err != nil {
		return fmt.Errorf("failed to send token created owner email: %w", err)
	}
	return nil
}

// TokenCreatedAdminArgs contains parameters for notifying admins (excluding owner)
type TokenCreatedAdminArgs struct {
	AdminEmails  []string `json:"admin_emails"`
	OwnerEmail   string   `json:"owner_email"`
	TokenName    string   `json:"token_name"`
	CreatorLabel string   `json:"creator_label"`
	Rack         string   `json:"rack"`
}

// Kind returns the unique identifier for this job type
func (TokenCreatedAdminArgs) Kind() string { return "email:token:created_admin" }

// TokenCreatedAdminWorker sends admin notification emails about token creation
type TokenCreatedAdminWorker struct {
	river.WorkerDefaults[TokenCreatedAdminArgs]
	emailSender gtwemail.Sender
}

// NewTokenCreatedAdminWorker creates a new worker instance
func NewTokenCreatedAdminWorker(emailSender gtwemail.Sender) *TokenCreatedAdminWorker {
	return &TokenCreatedAdminWorker{emailSender: emailSender}
}

// Work sends the admin notification emails
func (w *TokenCreatedAdminWorker) Work(_ context.Context, job *river.Job[TokenCreatedAdminArgs]) error {
	args := job.Args
	if len(args.AdminEmails) == 0 {
		return nil
	}
	subject := fmt.Sprintf("Rack Gateway (%s): API token created for %s", args.Rack, args.OwnerEmail)
	text := fmt.Sprintf(
		"An API token '%s' was created for %s by %s.",
		args.TokenName,
		args.OwnerEmail,
		args.CreatorLabel,
	)
	html := ""
	if err := w.emailSender.SendMany(args.AdminEmails, subject, text, html); err != nil {
		return fmt.Errorf("failed to send token created admin emails: %w", err)
	}
	return nil
}
