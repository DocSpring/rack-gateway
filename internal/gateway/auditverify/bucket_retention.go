package auditverify

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// daysPerYear is how S3 counts a default retention given in years.
const daysPerYear = 365

// BucketLockReader reads a bucket's Object Lock configuration (s3:GetBucketObjectLockConfiguration). *s3.Client
// implements it.
type BucketLockReader interface {
	GetObjectLockConfiguration(
		ctx context.Context,
		params *s3.GetObjectLockConfigurationInput,
		optFns ...func(*s3.Options),
	) (*s3.GetObjectLockConfigurationOutput, error)
}

// CheckBucketRetention returns an error unless the bucket locks every new object in COMPLIANCE mode for at least
// minDays days by default. The anchor writer sets no retention of its own, so the bucket's default rule is all that
// keeps anchors from being deleted or overwritten; GOVERNANCE mode can be bypassed.
func CheckBucketRetention(ctx context.Context, store BucketLockReader, bucket string, minDays int) error {
	output, err := store.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("couldn't read the Object Lock configuration of s3://%s: %w", bucket, err)
	}
	config := output.ObjectLockConfiguration
	if config == nil || config.ObjectLockEnabled != types.ObjectLockEnabledEnabled {
		return fmt.Errorf("s3://%s doesn't have Object Lock enabled", bucket)
	}
	if config.Rule == nil || config.Rule.DefaultRetention == nil {
		return fmt.Errorf("s3://%s has no default Object Lock retention, so anchors aren't locked", bucket)
	}
	retention := config.Rule.DefaultRetention
	if retention.Mode != types.ObjectLockRetentionModeCompliance {
		return fmt.Errorf("s3://%s's default Object Lock retention is in %s mode; anchors need COMPLIANCE mode",
			bucket, retention.Mode)
	}
	days := int(aws.ToInt32(retention.Days)) + int(aws.ToInt32(retention.Years))*daysPerYear
	if days < minDays {
		return fmt.Errorf("s3://%s locks anchors for %d days, less than AUDIT_ANCHOR_RETENTION_DAYS (%d)",
			bucket, days, minDays)
	}
	return nil
}
