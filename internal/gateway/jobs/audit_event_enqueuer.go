package jobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	jobslack "github.com/DocSpring/rack-gateway/internal/gateway/jobs/slack"
)

// auditEventEnqueueTimeout bounds queueing one audit event's notification, as an audit write is bounded, so a
// stalled database can't hold the request or job that wrote the event. It doesn't use the caller's context: a client
// that disconnects mustn't drop the notification of what its request did.
const auditEventEnqueueTimeout = 10 * time.Second

// AuditEventEnqueuer implements audit.EventEnqueuer interface
type AuditEventEnqueuer struct {
	client *Client
}

// NewAuditEventEnqueuer creates a new audit event enqueuer
func NewAuditEventEnqueuer(client *Client) *AuditEventEnqueuer {
	return &AuditEventEnqueuer{client: client}
}

// EnqueueAuditEvent enqueues an audit event notification job
func (e *AuditEventEnqueuer) EnqueueAuditEvent(auditLogID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), auditEventEnqueueTimeout)
	defer cancel()
	_, err := e.client.Insert(ctx, jobslack.AuditEventArgs{
		AuditLogID: auditLogID,
	}, &river.InsertOpts{
		Queue:       QueueNotifications,
		MaxAttempts: MaxAttemptsNotification,
	})
	return err
}
