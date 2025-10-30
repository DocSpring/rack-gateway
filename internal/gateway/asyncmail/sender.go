package asyncmail

import (
	"context"

	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	"github.com/DocSpring/rack-gateway/internal/gateway/jobs"
	jobemail "github.com/DocSpring/rack-gateway/internal/gateway/jobs/email"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Inserter is the minimal jobs client interface we rely on
type Inserter interface {
    Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Sender implements email.Sender by enqueuing generic email jobs
type Sender struct {
    jobs Inserter
}

func NewSender(j Inserter) email.Sender {
    return &Sender{jobs: j}
}

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


