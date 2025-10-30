package jobslack

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/slack"
)

// AuditEventArgs contains parameters for Slack audit event notification
type AuditEventArgs struct {
	AuditLogID int64 `json:"audit_log_id"`
}

// Kind returns the unique identifier for this job type
func (AuditEventArgs) Kind() string { return "slack:audit_event" }

// AuditEventWorker sends Slack notifications for audit events
type AuditEventWorker struct {
	river.WorkerDefaults[AuditEventArgs]
	database      *db.Database
	slackNotifier *slack.Notifier
}

// NewAuditEventWorker creates a new Slack audit event worker
func NewAuditEventWorker(database *db.Database, slackNotifier *slack.Notifier) *AuditEventWorker {
	return &AuditEventWorker{
		database:      database,
		slackNotifier: slackNotifier,
	}
}

// Work sends the Slack audit event notification
func (w *AuditEventWorker) Work(ctx context.Context, job *river.Job[AuditEventArgs]) error {
	// Load the audit log from database
	auditLog, err := w.database.GetAuditLogByID(job.Args.AuditLogID)
	if err != nil {
		return fmt.Errorf("failed to load audit log %d: %w", job.Args.AuditLogID, err)
	}

	// Send Slack notification
	if err := w.slackNotifier.NotifyAuditEvent(auditLog); err != nil {
		return fmt.Errorf("failed to send Slack audit event notification: %w", err)
	}

	return nil
}

// DeployApprovalArgs contains parameters for Slack deploy approval notification
type DeployApprovalArgs struct {
	DeployApprovalRequestID int64  `json:"deploy_approval_request_id"`
	GatewayDomain           string `json:"gateway_domain"`
}

// Kind returns the unique identifier for this job type
func (DeployApprovalArgs) Kind() string { return "slack:deploy_approval" }

// DeployApprovalWorker sends Slack notifications for deploy approval requests
type DeployApprovalWorker struct {
	river.WorkerDefaults[DeployApprovalArgs]
	database      *db.Database
	slackNotifier *slack.Notifier
}

// NewDeployApprovalWorker creates a new Slack deploy approval worker
func NewDeployApprovalWorker(database *db.Database, slackNotifier *slack.Notifier) *DeployApprovalWorker {
	return &DeployApprovalWorker{
		database:      database,
		slackNotifier: slackNotifier,
	}
}

// Work sends the Slack deploy approval notification
func (w *DeployApprovalWorker) Work(ctx context.Context, job *river.Job[DeployApprovalArgs]) error {
	// Load the deploy approval request from database
	deployApprovalRequest, err := w.database.GetDeployApprovalRequest(job.Args.DeployApprovalRequestID)
	if err != nil {
		return fmt.Errorf("failed to load deploy approval request %d: %w", job.Args.DeployApprovalRequestID, err)
	}

	// Send Slack notification
	if err := w.slackNotifier.NotifyDeployApprovalCreated(deployApprovalRequest, job.Args.GatewayDomain); err != nil {
		return fmt.Errorf("failed to send Slack deploy approval notification: %w", err)
	}

	return nil
}
