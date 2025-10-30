package jobs

import (
	"context"

	"github.com/riverqueue/river"

	jobslack "github.com/DocSpring/rack-gateway/internal/gateway/jobs/slack"
)

// AuditEventEnqueuer implements audit.AuditEventEnqueuer interface
type AuditEventEnqueuer struct {
	client *Client
}

// NewAuditEventEnqueuer creates a new audit event enqueuer
func NewAuditEventEnqueuer(client *Client) *AuditEventEnqueuer {
	return &AuditEventEnqueuer{client: client}
}

// EnqueueAuditEvent enqueues an audit event notification job
func (e *AuditEventEnqueuer) EnqueueAuditEvent(auditLogID int64) error {
	_, err := e.client.Insert(context.Background(), jobslack.AuditEventArgs{
		AuditLogID: auditLogID,
	}, &river.InsertOpts{
		Queue:       QueueNotifications,
		MaxAttempts: MaxAttemptsNotification,
	})
	return err
}
