// Package auditverify checks the audit log's hash chain in the database against its HMACs and against the
// anchors written to the S3 Object Lock bucket.
//
// Integrity problems go into the Report; Run only returns an error when it can't read what it needs to check.
package auditverify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// defaultBatchSize is how many events are read per query.
const defaultBatchSize = 1000

// DefaultMaxAnchorAge is how old the latest anchor may be before verification fails. Anchors are written every
// hour, so this allows two missed runs.
const DefaultMaxAnchorAge = 3 * time.Hour

// ChainSource reads the stored audit chain. *db.Database implements it.
type ChainSource interface {
	AuditChainHead(ctx context.Context) (db.AuditChainHead, error)
	AuditChainRows(ctx context.Context, afterIndex, toIndex int64, limit int) ([]*db.AuditChainRow, error)
	AuditChainRowAt(ctx context.Context, index int64) (*db.AuditChainRow, error)
	AuditChainRowBefore(ctx context.Context, index int64) (*db.AuditChainRow, error)
	AuditChainStats(ctx context.Context, toIndex int64) (db.AuditChainStats, error)
}

// AnchorStore reads anchors, their versions and the bucket's Object Lock configuration. *s3.Client implements it.
// GetObject is called from several goroutines at once.
type AnchorStore interface {
	BucketLockReader
	ListObjectVersions(
		ctx context.Context,
		params *s3.ListObjectVersionsInput,
		optFns ...func(*s3.Options),
	) (*s3.ListObjectVersionsOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

var _ AnchorStore = (*s3.Client)(nil)

// AnchorOptions says where the anchors are.
type AnchorOptions struct {
	Store   AnchorStore
	Bucket  string
	ChainID string
	// MaxAge is how old the latest anchor may be (default DefaultMaxAnchorAge). An older one means anchors have
	// stopped being written, which leaves every new event unprotected.
	MaxAge time.Duration
	// MinRetentionDays is AUDIT_ANCHOR_RETENTION_DAYS: the bucket must lock new objects in COMPLIANCE mode for at
	// least this long by default (CheckBucketRetention). Zero skips the check.
	MinRetentionDays int
}

// Options configures a verification run.
type Options struct {
	// Secret is AUDIT_HMAC_SECRET. It is required.
	Secret []byte
	// From is the first chain index to check.
	From int64
	// To is the last chain index to check; nil means the latest event.
	To *int64
	// HashV2From pins the chain index of the first hash v2 event, recorded outside the database after
	// upgrading (nil: not pinned). The database's own value can't be trusted: v2 events relabeled as legacy skip
	// their HMAC check. Without a pin or a format 2 anchor, legacy events make verification fail.
	HashV2From *int64
	// VerifiedThrough is the latest event an earlier verification checked (nil: none). The chain must still hold
	// it, unchanged: otherwise events were removed from the end, perhaps with new ones written in their place,
	// which checking the events that are now there can't show.
	VerifiedThrough *VerifiedEvent
	// BatchSize is how many events are read per query (default 1000).
	BatchSize int
	// Anchors, when set, also checks every anchor in the bucket.
	Anchors *AnchorOptions
	// Now is the time the latest anchor's age and missing anchor hours are measured at (default: the current
	// time).
	Now time.Time
}

// VerifiedEvent is an event a verification checked, by chain index and hash.
type VerifiedEvent struct {
	Index int64
	Hash  []byte
}

// Run verifies the chain (events From..To) and, if configured, every anchor. A full run (no From or To) also
// counts the stored events, and every run checks that something outside the database confirms where hash v2
// starts. A run that can't finish (a read fails) returns the error with the report so far, StoppedBy set: the
// failures it found before it stopped still count.
func Run(ctx context.Context, source ChainSource, opts Options) (*Report, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	head, err := source.AuditChainHead(ctx)
	if err != nil {
		return nil, err
	}
	report := &Report{Head: head, HashV2FromPinned: opts.HashV2From}
	if err := runChecks(ctx, source, opts, head, report); err != nil {
		report.StoppedBy = err.Error()
		return report, err
	}
	return report, nil
}

func runChecks(ctx context.Context, source ChainSource, opts Options, head db.AuditChainHead, report *Report) error {
	checkPinnedHashV2Start(head, opts.HashV2From, report)
	if err := checkVerifiedEvent(ctx, source, head, opts.VerifiedThrough, report); err != nil {
		return err
	}
	if err := verifyRows(ctx, source, opts, head, report); err != nil {
		return err
	}
	if opts.From == 0 && opts.To == nil {
		if err := checkRowCounts(ctx, source, head, report); err != nil {
			return err
		}
	}
	if opts.Anchors != nil {
		if err := verifyAnchors(ctx, source, *opts.Anchors, head, opts.now(), report); err != nil {
			return err
		}
	}
	checkHashV2StartConfirmed(head, report)
	return nil
}

func (o *Options) now() time.Time {
	if o.Now.IsZero() {
		return time.Now()
	}
	return o.Now
}

func (o *Options) validate() error {
	if len(o.Secret) == 0 {
		return errors.New("the audit HMAC secret is required to verify event hashes")
	}
	if o.From < 0 {
		return fmt.Errorf("--from must not be negative (got %d)", o.From)
	}
	if o.To != nil && *o.To < o.From {
		return fmt.Errorf("--to (%d) is before --from (%d)", *o.To, o.From)
	}
	if o.HashV2From != nil && *o.HashV2From < 0 {
		return fmt.Errorf("the hash v2 start must not be negative (got %d)", *o.HashV2From)
	}
	if o.VerifiedThrough != nil && o.VerifiedThrough.Index < 0 {
		return fmt.Errorf("the verified chain index must not be negative (got %d)", o.VerifiedThrough.Index)
	}
	if o.BatchSize <= 0 {
		o.BatchSize = defaultBatchSize
	}
	if o.Anchors == nil {
		return nil
	}
	return o.Anchors.validate()
}

func (a *AnchorOptions) validate() error {
	if a.Store == nil || a.Bucket == "" || a.ChainID == "" {
		return errors.New("anchor verification needs an S3 client, a bucket and a chain ID")
	}
	if a.MaxAge < 0 {
		return fmt.Errorf("the maximum anchor age must not be negative (got %s)", a.MaxAge)
	}
	if a.MinRetentionDays < 0 {
		return fmt.Errorf("the minimum anchor retention must not be negative (got %d)", a.MinRetentionDays)
	}
	return nil
}
