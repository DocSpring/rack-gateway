package jobs

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	jobcircleci "github.com/DocSpring/rack-gateway/internal/gateway/jobs/circleci"
	jobemail "github.com/DocSpring/rack-gateway/internal/gateway/jobs/email"
	jobgithub "github.com/DocSpring/rack-gateway/internal/gateway/jobs/github"
	jobslack "github.com/DocSpring/rack-gateway/internal/gateway/jobs/slack"
	"github.com/DocSpring/rack-gateway/internal/gateway/slack"
)

// Client wraps the River client for background job processing
type Client struct {
	river *river.Client[pgx.Tx]
}

// Dependencies holds all dependencies needed to create job workers
type Dependencies struct {
	Database      *db.Database
	EmailSender   email.Sender
	SlackNotifier *slack.Notifier
}

// NewClient creates a new River job client with all workers registered
func NewClient(dbPool *pgxpool.Pool, deps *Dependencies) (*Client, error) {
	workers := river.NewWorkers()

	// Email workers - security notifications
	river.AddWorker(workers, jobemail.NewFailedMFAWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewFailedLoginWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewRateLimitUserWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewRateLimitAdminWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewSuspiciousActivityUserWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewSuspiciousActivityAdminWorker(deps.EmailSender))

	// Email workers - user management
	river.AddWorker(workers, jobemail.NewWelcomeWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewUserAddedAdminWorker(deps.EmailSender))

	// Email workers - generic async delivery
	river.AddWorker(workers, jobemail.NewSendSingleWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewSendManyWorker(deps.EmailSender))

	// Email workers - API tokens
	river.AddWorker(workers, jobemail.NewTokenCreatedOwnerWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewTokenCreatedAdminWorker(deps.EmailSender))

	// Email workers - user lock/unlock
	river.AddWorker(workers, jobemail.NewUserLockedWorker(deps.EmailSender))
	river.AddWorker(workers, jobemail.NewUserUnlockedWorker(deps.EmailSender))

	// Email workers - MFA auto-lock
	river.AddWorker(workers, jobemail.NewMFAAutoLockWorker(deps.EmailSender))

	// Email workers - rack params changed
	river.AddWorker(workers, jobemail.NewRackParamsChangedWorker(deps.EmailSender))

	// Slack workers
	river.AddWorker(workers, jobslack.NewAuditEventWorker(deps.Database, deps.SlackNotifier))
	river.AddWorker(workers, jobslack.NewDeployApprovalWorker(deps.Database, deps.SlackNotifier))

	// CI/GitHub workers
	river.AddWorker(workers, jobcircleci.NewApproveJobWorker())
	river.AddWorker(workers, jobgithub.NewPostPRCommentWorker())

	riverClient, err := river.NewClient(riverpgxv5.New(dbPool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			QueueSecurity:      {MaxWorkers: 5},  // High priority security notifications
			QueueNotifications: {MaxWorkers: 10}, // Medium priority notifications
			QueueIntegrations:  {MaxWorkers: 3},  // Low priority CI/GitHub integrations
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create river client: %w", err)
	}

	return &Client{river: riverClient}, nil
}

// Start begins processing jobs in the background
func (c *Client) Start(ctx context.Context) error {
	if err := c.river.Start(ctx); err != nil {
		return fmt.Errorf("failed to start river client: %w", err)
	}
	log.Println("River job worker started")
	return nil
}

// Stop gracefully shuts down the job worker
func (c *Client) Stop(ctx context.Context) error {
	if err := c.river.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop river client: %w", err)
	}
	log.Println("River job worker stopped")
	return nil
}

// Insert enqueues a new job for background processing
func (c *Client) Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	return c.river.Insert(ctx, args, opts)
}

// InsertTx enqueues a new job within a transaction
func (c *Client) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	return c.river.InsertTx(ctx, tx, args, opts)
}

// JobList lists jobs with the given parameters
func (c *Client) JobList(ctx context.Context, params *river.JobListParams) (*river.JobListResult, error) {
	return c.river.JobList(ctx, params)
}

// JobGet retrieves a job by ID
func (c *Client) JobGet(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	return c.river.JobGet(ctx, id)
}
