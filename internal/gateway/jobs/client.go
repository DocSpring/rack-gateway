package jobs

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/email"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
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

// AuditAnchorConfig holds configuration for WORM S3 anchor writer
type AuditAnchorConfig struct {
	S3Client      interface{} // *s3.Client
	Bucket        string
	ChainID       string
	RetentionDays int
}

// NewClient creates a new River job client with all workers registered
func NewClient(dbPool *pgxpool.Pool, deps *Dependencies, auditAnchorConfig *AuditAnchorConfig) (*Client, error) {
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

	// Setup periodic jobs
	var periodicJobs []*river.PeriodicJob

	// WORM anchor writer periodic job (hourly)
	if auditAnchorConfig != nil && auditAnchorConfig.S3Client != nil {
		s3Client, ok := auditAnchorConfig.S3Client.(*s3.Client)
		if !ok {
			return nil, fmt.Errorf("invalid S3 client type for audit anchor config")
		}
		anchorWorker := jobaudit.NewAnchorWriterWorker(
			deps.Database,
			s3Client,
			auditAnchorConfig.Bucket,
			auditAnchorConfig.ChainID,
			auditAnchorConfig.RetentionDays,
		)
		river.AddWorker(workers, anchorWorker)

		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			river.PeriodicInterval(1*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return jobaudit.AnchorWriterArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}

	riverClient, err := river.NewClient(riverpgxv5.New(dbPool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			QueueSecurity:      {MaxWorkers: 5},  // High priority security notifications
			QueueNotifications: {MaxWorkers: 10}, // Medium priority notifications
			QueueIntegrations:  {MaxWorkers: 3},  // Low priority CI/GitHub integrations
		},
		Workers:      workers,
		PeriodicJobs: periodicJobs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create river client: %w", err)
	}

	return &Client{river: riverClient}, nil
}

// NewAuditAnchorConfigFromEnv creates audit anchor config from environment variables if set
func NewAuditAnchorConfigFromEnv() (*AuditAnchorConfig, error) {
	bucket := os.Getenv("AUDIT_ANCHOR_BUCKET")
	if bucket == "" {
		return nil, nil // WORM not configured
	}

	chainID := os.Getenv("AUDIT_ANCHOR_CHAIN_ID")
	if chainID == "" {
		return nil, fmt.Errorf("AUDIT_ANCHOR_BUCKET is set but AUDIT_ANCHOR_CHAIN_ID is missing")
	}

	retentionDaysStr := os.Getenv("AUDIT_ANCHOR_RETENTION_DAYS")
	retentionDays := 400 // default
	if retentionDaysStr != "" {
		var err error
		retentionDays, err = strconv.Atoi(retentionDaysStr)
		if err != nil {
			return nil, fmt.Errorf("invalid AUDIT_ANCHOR_RETENTION_DAYS: %w", err)
		}
	}

	// Initialize AWS config and S3 client
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)

	return &AuditAnchorConfig{
		S3Client:      s3Client,
		Bucket:        bucket,
		ChainID:       chainID,
		RetentionDays: retentionDays,
	}, nil
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
func (c *Client) Insert(
	ctx context.Context,
	args river.JobArgs,
	opts *river.InsertOpts,
) (*rivertype.JobInsertResult, error) {
	return c.river.Insert(ctx, args, opts)
}

// InsertTx enqueues a new job within a transaction
func (c *Client) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	args river.JobArgs,
	opts *river.InsertOpts,
) (*rivertype.JobInsertResult, error) {
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
