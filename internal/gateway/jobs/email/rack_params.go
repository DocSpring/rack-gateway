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

func (RackParamsChangedArgs) Kind() string { return "email:rack:params_changed" }

type RackParamsChangedWorker struct {
	river.WorkerDefaults[RackParamsChangedArgs]
	emailSender gtwemail.Sender
}

func NewRackParamsChangedWorker(emailSender gtwemail.Sender) *RackParamsChangedWorker {
	return &RackParamsChangedWorker{emailSender: emailSender}
}

func (w *RackParamsChangedWorker) Work(ctx context.Context, job *river.Job[RackParamsChangedArgs]) error {
	args := job.Args
	if len(args.AdminEmails) == 0 {
		return nil
	}
	subject := fmt.Sprintf("Rack Gateway (%s): %s changed rack parameters", args.Rack, args.Actor)
	text, html, _ := emailtemplates.RenderRackParamsChanged(args.Rack, args.Actor, args.Changes)
	return w.emailSender.SendMany(args.AdminEmails, subject, text, html)
}
