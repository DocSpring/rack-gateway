package jobs

import (
	"context"

	jobslack "github.com/DocSpring/rack-gateway/internal/gateway/jobs/slack"
	"github.com/riverqueue/river"
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
