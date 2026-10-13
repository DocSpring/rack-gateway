package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/riverqueue/river"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/heartbeat"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
	jobverifyaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/verifyaudit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testbypass"
)

// defaultAnchorInterval is how often the anchor writer runs unless AUDIT_ANCHOR_INTERVAL_MINUTES says otherwise.
const defaultAnchorInterval = time.Hour

// defaultAnchorRetentionDays is AUDIT_ANCHOR_RETENTION_DAYS when it isn't set.
const defaultAnchorRetentionDays = 400

// bucketCheckTimeout bounds the startup check of the anchor bucket's Object Lock configuration.
const bucketCheckTimeout = 30 * time.Second

// AuditAnchorConfig says where audit anchors are written: an S3 bucket with Object Lock.
type AuditAnchorConfig struct {
	S3Client *s3.Client
	Bucket   string
	ChainID  string
	// RetentionDays is the least the bucket's default COMPLIANCE retention may be (AUDIT_ANCHOR_RETENTION_DAYS).
	// The bucket's rule locks the anchors; the gateway only checks it.
	RetentionDays int
	// Interval is how often the anchor writer runs (AUDIT_ANCHOR_INTERVAL_MINUTES). Anchors are keyed by hour, so
	// it writes at most one an hour.
	Interval time.Duration
}

// NewAuditAnchorConfigFromEnv returns the anchor configuration, or nil when AUDIT_ANCHOR_BUCKET isn't set.
func NewAuditAnchorConfigFromEnv() (*AuditAnchorConfig, error) {
	bucket := os.Getenv("AUDIT_ANCHOR_BUCKET")
	if bucket == "" {
		return nil, nil
	}

	chainID := os.Getenv("AUDIT_ANCHOR_CHAIN_ID")
	if chainID == "" {
		return nil, fmt.Errorf("AUDIT_ANCHOR_BUCKET is set but AUDIT_ANCHOR_CHAIN_ID is missing")
	}

	retentionDays := defaultAnchorRetentionDays
	if value := os.Getenv("AUDIT_ANCHOR_RETENTION_DAYS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("AUDIT_ANCHOR_RETENTION_DAYS must be a positive number of days (got %q)", value)
		}
		retentionDays = parsed
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		// MinIO requires path-style addressing
		if testbypass.S3EndpointOverride() {
			o.UsePathStyle = true
		}
	})

	return &AuditAnchorConfig{
		S3Client:      s3Client,
		Bucket:        bucket,
		ChainID:       chainID,
		RetentionDays: retentionDays,
		Interval:      anchorIntervalFromEnv(),
	}, nil
}

// CheckBucketRetention refuses an anchor bucket that doesn't lock anchors in COMPLIANCE mode for at least
// RetentionDays (auditverify.CheckBucketRetention). The gateway checks it before it starts.
func (c *AuditAnchorConfig) CheckBucketRetention(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, bucketCheckTimeout)
	defer cancel()
	return auditverify.CheckBucketRetention(ctx, c.S3Client, c.Bucket, c.RetentionDays)
}

// anchorIntervalFromEnv returns AUDIT_ANCHOR_INTERVAL_MINUTES, or an hour when it isn't a positive number.
func anchorIntervalFromEnv() time.Duration {
	if minutes, err := strconv.Atoi(os.Getenv("AUDIT_ANCHOR_INTERVAL_MINUTES")); err == nil && minutes > 0 {
		return time.Duration(minutes) * time.Minute
	}
	return defaultAnchorInterval
}

// AuditVerifyOptions returns the configuration `verify-audit` and the scheduled verification share: the secret
// from AUDIT_HMAC_SECRET (db.ErrAuditHMACSecretNotSet without it: never the development fallback), the hash v2
// start pinned by AUDIT_HASH_V2_FROM and, unless anchors is nil, every anchor under the chain ID.
func AuditVerifyOptions(anchors *AuditAnchorConfig) (auditverify.Options, error) {
	secret, err := db.AuditHMACSecretFromEnv()
	if err != nil {
		return auditverify.Options{}, err
	}
	pinned, err := auditverify.PinnedHashV2StartFromEnv()
	if err != nil {
		return auditverify.Options{}, err
	}
	opts := auditverify.Options{Secret: secret, HashV2From: pinned}
	if anchors != nil {
		opts.Anchors = &auditverify.AnchorOptions{
			Store:            anchors.S3Client,
			Bucket:           anchors.Bucket,
			ChainID:          anchors.ChainID,
			MaxAge:           maxAnchorAge(anchors.Interval),
			MinRetentionDays: anchors.RetentionDays,
		}
	}
	return opts, nil
}

// maxAnchorAge is how old the latest anchor may be: auditverify's default, which allows two missed hourly
// anchors, or three anchor intervals when anchors are written less often than hourly.
func maxAnchorAge(interval time.Duration) time.Duration {
	return max(auditverify.DefaultMaxAnchorAge, 3*interval)
}

// anchorWriterJob registers the anchor writer, which runs when the gateway starts and every anchor interval.
func anchorWriterJob(workers *river.Workers, deps *Dependencies, anchors *AuditAnchorConfig) *river.PeriodicJob {
	worker := jobaudit.NewAnchorWriterWorker(deps.Database, anchors.S3Client, anchors.Bucket, anchors.ChainID)
	river.AddWorker(workers, worker)
	return river.NewPeriodicJob(
		river.PeriodicInterval(anchors.Interval),
		func() (river.JobArgs, *river.InsertOpts) { return jobaudit.AnchorWriterArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)
}

// auditVerificationJob registers the scheduled audit verification, which runs the same checks as verify-audit
// and pings AUDIT_VERIFY_HEARTBEAT_URL after every run. It returns nil when AUDIT_HMAC_SECRET isn't set, which the
// gateway only allows in DEV_MODE: verification never uses the development secret. Without anchors it checks the
// hash chain only.
func auditVerificationJob(
	workers *river.Workers,
	deps *Dependencies,
	anchors *AuditAnchorConfig,
) (*river.PeriodicJob, error) {
	pinger, err := heartbeat.FromEnv(jobverifyaudit.HeartbeatEnv)
	if err != nil {
		return nil, err
	}
	opts, err := AuditVerifyOptions(anchors)
	if errors.Is(err, db.ErrAuditHMACSecretNotSet) {
		log.Printf("Audit verification: disabled because AUDIT_HMAC_SECRET is not set")
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("audit verification: %w", err)
	}
	if anchors == nil {
		log.Printf("Audit verification: anchors aren't configured (AUDIT_ANCHOR_BUCKET), so the hourly " +
			"verification checks the hash chain only")
	}
	var ping jobverifyaudit.HeartbeatFunc
	if pinger != nil {
		ping = pinger.Ping
	}
	river.AddWorker(workers, jobverifyaudit.NewWorker(deps.Database, opts, deps.AuditLogger.LogDBEntry, ping))
	return jobverifyaudit.PeriodicJob(), nil
}
