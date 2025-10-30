package email

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	gtwemail "github.com/DocSpring/rack-gateway/internal/gateway/email"
	emailtemplates "github.com/DocSpring/rack-gateway/internal/gateway/email/templates"
)

// RackParamsChangedArgs parameters for rack params change admin emails
type RackParamsChangedArgs struct {
	AdminEmails []string `json:"admin_emails"`
	Rack        string   `json:"rack"`
	Actor       string   `json:"actor"`
	Changes     string   `json:"changes"`
}

// Kind returns the job kind identifier for rack params change notifications
func (RackParamsChangedArgs) Kind() string { return "email:rack:params_changed" }

// RackParamsChangedWorker sends email notifications when rack parameters are changed
type RackParamsChangedWorker struct {
	river.WorkerDefaults[RackParamsChangedArgs]
	emailSender gtwemail.Sender
}

// NewRackParamsChangedWorker creates a new worker for rack params change notifications
func NewRackParamsChangedWorker(emailSender gtwemail.Sender) *RackParamsChangedWorker {
	return &RackParamsChangedWorker{emailSender: emailSender}
}

// Work processes a rack params change notification job and sends emails to admins
func (w *RackParamsChangedWorker) Work(_ context.Context, job *river.Job[RackParamsChangedArgs]) error {
	args := job.Args
	if len(args.AdminEmails) == 0 {
		return nil
	}
	subject := fmt.Sprintf("Rack Gateway (%s): %s changed rack parameters", args.Rack, args.Actor)
	text, html, _ := emailtemplates.RenderRackParamsChanged(args.Rack, args.Actor, args.Changes)
	return w.emailSender.SendMany(args.AdminEmails, subject, text, html)
}
