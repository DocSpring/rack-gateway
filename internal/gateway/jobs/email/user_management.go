package email

import (
	"context"
	"fmt"
	"strings"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/email"
)

// WelcomeArgs contains parameters for welcome email to new users
type WelcomeArgs struct {
	Email        string   `json:"email"`
	Name         string   `json:"name"`
	Roles        []string `json:"roles"`
	InviterEmail string   `json:"inviter_email"`
	Rack         string   `json:"rack"`
	BaseURL      string   `json:"base_url"`
}

// Kind returns the unique identifier for this job type
func (WelcomeArgs) Kind() string { return "email:user:welcome" }

// WelcomeWorker sends welcome emails to new users
type WelcomeWorker struct {
	river.WorkerDefaults[WelcomeArgs]
	emailSender email.Sender
}

// NewWelcomeWorker creates a new welcome email worker
func NewWelcomeWorker(emailSender email.Sender) *WelcomeWorker {
	return &WelcomeWorker{emailSender: emailSender}
}

// Work sends the welcome email
func (w *WelcomeWorker) Work(_ context.Context, job *river.Job[WelcomeArgs]) error {
	args := job.Args

	subject := fmt.Sprintf("Welcome to %s Rack Gateway", args.Rack)
	rolesText := strings.Join(args.Roles, ", ")

	text := fmt.Sprintf(`Hello %s,

You have been added to the %s Rack Gateway by %s.

Your assigned roles: %s

You can access the gateway at:
%s

Please log in with your Google Workspace account to get started.

If you have any questions, please contact your administrator.

Welcome aboard!`,
		args.Name,
		args.Rack,
		args.InviterEmail,
		rolesText,
		args.BaseURL,
	)

	html := fmt.Sprintf(`<p>Hello %s,</p>
<p>You have been added to the %s Rack Gateway by %s.</p>
<p><strong>Your assigned roles:</strong> %s</p>
<p>You can access the gateway at:<br>
<a href="%s">%s</a></p>
<p>Please log in with your Google Workspace account to get started.</p>
<p>If you have any questions, please contact your administrator.</p>
<p>Welcome aboard!</p>`,
		args.Name,
		args.Rack,
		args.InviterEmail,
		rolesText,
		args.BaseURL,
		args.BaseURL,
	)

	if err := w.emailSender.Send(args.Email, subject, text, html); err != nil {
		return fmt.Errorf("failed to send welcome email: %w", err)
	}

	return nil
}

// UserAddedAdminArgs contains parameters for admin notification when a user is added
type UserAddedAdminArgs struct {
	AdminEmails  []string `json:"admin_emails"`
	NewUserEmail string   `json:"new_user_email"`
	NewUserName  string   `json:"new_user_name"`
	Roles        []string `json:"roles"`
	CreatorEmail string   `json:"creator_email"`
	Rack         string   `json:"rack"`
}

// Kind returns the unique identifier for this job type
func (UserAddedAdminArgs) Kind() string { return "email:user:added_admin" }

// UserAddedAdminWorker sends admin notifications when a user is added
type UserAddedAdminWorker struct {
	river.WorkerDefaults[UserAddedAdminArgs]
	emailSender email.Sender
}

// NewUserAddedAdminWorker creates a new user added admin email worker
func NewUserAddedAdminWorker(emailSender email.Sender) *UserAddedAdminWorker {
	return &UserAddedAdminWorker{emailSender: emailSender}
}

// Work sends the admin notification email
func (w *UserAddedAdminWorker) Work(_ context.Context, job *river.Job[UserAddedAdminArgs]) error {
	args := job.Args

	subject := fmt.Sprintf("New User Added to %s Rack Gateway: %s", args.Rack, args.NewUserEmail)
	rolesText := strings.Join(args.Roles, ", ")

	text := fmt.Sprintf(`Admin Notification: A new user has been added to the %s Rack Gateway.

New User: %s (%s)
Roles: %s
Added by: %s
Time: %s

This is an automated notification from Rack Gateway.`,
		args.Rack,
		args.NewUserEmail,
		args.NewUserName,
		rolesText,
		args.CreatorEmail,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
	)

	html := fmt.Sprintf(`<p><strong>Admin Notification: A new user has been added to the %s Rack Gateway.</strong></p>
<p><strong>New User:</strong> %s (%s)</p>
<p><strong>Roles:</strong> %s</p>
<p><strong>Added by:</strong> %s</p>
<p><strong>Time:</strong> %s</p>
<p><em>This is an automated notification from Rack Gateway.</em></p>`,
		args.Rack,
		args.NewUserEmail,
		args.NewUserName,
		rolesText,
		args.CreatorEmail,
		job.CreatedAt.Format("2006-01-02 15:04:05 MST"),
	)

	if err := w.emailSender.SendMany(args.AdminEmails, subject, text, html); err != nil {
		return fmt.Errorf("failed to send user added admin emails: %w", err)
	}

	return nil
}
