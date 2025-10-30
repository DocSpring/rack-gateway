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
	Html    string `json:"html"`
}

func (SendSingleArgs) Kind() string { return "email:send:single" }

type SendSingleWorker struct {
	river.WorkerDefaults[SendSingleArgs]
	delivery gtwemail.Sender
}

func NewSendSingleWorker(deliverySender gtwemail.Sender) *SendSingleWorker {
	return &SendSingleWorker{delivery: deliverySender}
}

func (w *SendSingleWorker) Work(ctx context.Context, job *river.Job[SendSingleArgs]) error {
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
	Html    string   `json:"html"`
}

func (SendManyArgs) Kind() string { return "email:send:many" }

type SendManyWorker struct {
	river.WorkerDefaults[SendManyArgs]
	delivery gtwemail.Sender
}

func NewSendManyWorker(deliverySender gtwemail.Sender) *SendManyWorker {
	return &SendManyWorker{delivery: deliverySender}
}

func (w *SendManyWorker) Work(ctx context.Context, job *river.Job[SendManyArgs]) error {
	args := job.Args
	if len(args.To) == 0 {
		return nil
	}
	return w.delivery.SendMany(args.To, args.Subject, args.Text, args.Html)
}
