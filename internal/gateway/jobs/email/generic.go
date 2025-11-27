package email

import (
	"context"

	"github.com/riverqueue/river"

	gtwemail "github.com/DocSpring/rack-gateway/internal/gateway/email"
)

// SendSingleArgs encapsulates a single-recipient email
type SendSingleArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	Html    string `json:"html"` //nolint:staticcheck // JSON field name matches email API
}

// Kind returns the job kind identifier for single email sending
func (SendSingleArgs) Kind() string { return "email:send:single" }

// SendSingleWorker is a River worker that sends single-recipient emails
type SendSingleWorker struct {
	river.WorkerDefaults[SendSingleArgs]
	delivery gtwemail.Sender
}

// NewSendSingleWorker creates a new worker for sending single-recipient emails
func NewSendSingleWorker(deliverySender gtwemail.Sender) *SendSingleWorker {
	return &SendSingleWorker{delivery: deliverySender}
}

// Work processes a single email job
func (w *SendSingleWorker) Work(_ context.Context, job *river.Job[SendSingleArgs]) error {
	args := job.Args
	if args.To == "" {
		return nil
	}
	return w.delivery.Send(args.To, args.Subject, args.Text, args.Html)
}

// SendManyArgs encapsulates a multi-recipient email
type SendManyArgs struct {
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	Html    string   `json:"html"` //nolint:staticcheck // JSON field name matches email API
}

// Kind returns the job kind identifier for multi-recipient email sending
func (SendManyArgs) Kind() string { return "email:send:many" }

// SendManyWorker is a River worker that sends multi-recipient emails
type SendManyWorker struct {
	river.WorkerDefaults[SendManyArgs]
	delivery gtwemail.Sender
}

// NewSendManyWorker creates a new worker for sending multi-recipient emails
func NewSendManyWorker(deliverySender gtwemail.Sender) *SendManyWorker {
	return &SendManyWorker{delivery: deliverySender}
}

// Work processes a multi-recipient email job
func (w *SendManyWorker) Work(_ context.Context, job *river.Job[SendManyArgs]) error {
	args := job.Args
	if len(args.To) == 0 {
		return nil
	}
	return w.delivery.SendMany(args.To, args.Subject, args.Text, args.Html)
}
