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

// ChainSource reads the stored audit chain. *db.Database implements it.
type ChainSource interface {
	AuditChainHead(ctx context.Context) (db.AuditChainHead, error)
	AuditChainRows(ctx context.Context, afterIndex, toIndex int64, limit int) ([]*db.AuditChainRow, error)
	AuditChainRowAt(ctx context.Context, index int64) (*db.AuditChainRow, error)
	AuditChainRowBefore(ctx context.Context, index int64) (*db.AuditChainRow, error)
}

// AnchorStore reads anchors and their versions. *s3.Client implements it.
type AnchorStore interface {
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
}

// Options configures a verification run.
type Options struct {
	// Secret is AUDIT_HMAC_SECRET. It is required.
	Secret []byte
	// From is the first chain index to check.
	From int64
	// To is the last chain index to check; nil means the latest event.
	To *int64
	// BatchSize is how many events are read per query (default 1000).
	BatchSize int
	// Anchors, when set, also checks every anchor in the bucket.
	Anchors *AnchorOptions
	// Now is the time missing anchor hours are counted up to (default: the current time).
	Now time.Time
}

// Run verifies the chain (events From..To) and, if configured, every anchor.
func Run(ctx context.Context, source ChainSource, opts Options) (*Report, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	head, err := source.AuditChainHead(ctx)
	if err != nil {
		return nil, err
	}
	report := &Report{Head: head}
	if err := verifyRows(ctx, source, opts, head, report); err != nil {
		return nil, err
	}
	if opts.Anchors != nil {
		now := opts.Now
		if now.IsZero() {
			now = time.Now()
		}
		if err := verifyAnchors(ctx, source, *opts.Anchors, head, now, report); err != nil {
			return nil, err
		}
	}
	return report, nil
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
	if o.BatchSize <= 0 {
		o.BatchSize = defaultBatchSize
	}
	if o.Anchors != nil && (o.Anchors.Store == nil || o.Anchors.Bucket == "" || o.Anchors.ChainID == "") {
		return errors.New("anchor verification needs an S3 client, a bucket and a chain ID")
	}
	return nil
}
