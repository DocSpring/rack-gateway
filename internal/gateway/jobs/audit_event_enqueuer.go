package jobs

import (
	"context"

	"github.com/riverqueue/river"

	jobslack "github.com/DocSpring/rack-gateway/internal/gateway/jobs/slack"
)

// AuditEventEnqueuer implements audit.EventEnqueuer interface
type AuditEventEnqueuer struct {
	client *Client
}

// NewAuditEventEnqueuer creates a new audit event enqueuer
func NewAuditEventEnqueuer(client *Client) *AuditEventEnqueuer {
	return &AuditEventEnqueuer{client: client}
}

// EnqueueAuditEvent enqueues an audit event notification job. The audit row is already written, so a client that
// disconnects mustn't drop the notification of what its request did.
func (e *AuditEventEnqueuer) EnqueueAuditEvent(auditLogID int64) error {
	_, err := e.client.InsertAfterWrite(context.Background(), jobslack.AuditEventArgs{
		AuditLogID: auditLogID,
	}, &river.InsertOpts{
		Queue:       QueueNotifications,
		MaxAttempts: MaxAttemptsNotification,
	})
	return err
}
