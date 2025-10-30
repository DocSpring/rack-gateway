package asyncmail

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobemail "github.com/DocSpring/rack-gateway/internal/gateway/jobs/email"
)

// Inserter is the minimal jobs client interface we rely on
type Inserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Sender implements email.Sender by enqueuing generic email jobs
type Sender struct {
	jobs Inserter
}

// NewSender creates a new async email sender that enqueues jobs via the provided inserter.
func NewSender(j Inserter) email.Sender {
	return &Sender{jobs: j}
}

// Send enqueues a single email job to be sent asynchronously.
func (s *Sender) Send(to, subject, textBody, htmlBody string) error {
	if s == nil || s.jobs == nil || to == "" {
		return nil
	}
	_, _ = s.jobs.Insert(context.Background(), jobemail.SendSingleArgs{
		To:      to,
		Subject: subject,
		Text:    textBody,
		Html:    htmlBody,
	}, &river.InsertOpts{Queue: jobs.QueueNotifications, MaxAttempts: jobs.MaxAttemptsNotification})
	return nil
}

// SendMany enqueues a batch email job to send to multiple recipients asynchronously.
func (s *Sender) SendMany(to []string, subject, textBody, htmlBody string) error {
	if s == nil || s.jobs == nil || len(to) == 0 {
		return nil
	}
	_, _ = s.jobs.Insert(context.Background(), jobemail.SendManyArgs{
		To:      to,
		Subject: subject,
		Text:    textBody,
		Html:    htmlBody,
	}, &river.InsertOpts{Queue: jobs.QueueNotifications, MaxAttempts: jobs.MaxAttemptsNotification})
	return nil
}
