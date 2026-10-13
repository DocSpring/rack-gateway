package auditverify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

func lockConfig(mode types.ObjectLockRetentionMode, days, years int32) *types.ObjectLockConfiguration {
	retention := &types.DefaultRetention{Mode: mode}
	if days > 0 {
		retention.Days = aws.Int32(days)
	}
	if years > 0 {
		retention.Years = aws.Int32(years)
	}
	return &types.ObjectLockConfiguration{
		ObjectLockEnabled: types.ObjectLockEnabledEnabled,
		Rule:              &types.ObjectLockRule{DefaultRetention: retention},
	}
}

type deniedLockReader struct{}

func (deniedLockReader) GetObjectLockConfiguration(
	context.Context,
	*s3.GetObjectLockConfigurationInput,
	...func(*s3.Options),
) (*s3.GetObjectLockConfigurationOutput, error) {
	return nil, errors.New("AccessDenied")
}

func TestCheckBucketRetention(t *testing.T) {
	compliance, governance := types.ObjectLockRetentionModeCompliance, types.ObjectLockRetentionModeGovernance
	noRule := &types.ObjectLockConfiguration{ObjectLockEnabled: types.ObjectLockEnabledEnabled}
	cases := []struct {
		name   string
		config *types.ObjectLockConfiguration
		want   string
	}{
		{"compliance for long enough", lockConfig(compliance, 400, 0), ""},
		{"years count as 365 days", lockConfig(compliance, 0, 2), ""},
		{"too short", lockConfig(compliance, 30, 0), "for 30 days, less than AUDIT_ANCHOR_RETENTION_DAYS (400)"},
		{"governance mode", lockConfig(governance, 400, 0), "anchors need COMPLIANCE mode"},
		{"no default rule", noRule, "has no default Object Lock retention"},
		{"no Object Lock", &types.ObjectLockConfiguration{}, "doesn't have Object Lock enabled"},
		{"no configuration", nil, "doesn't have Object Lock enabled"},
	}
	for _, tc := range cases {
		bucket := fakes3.New(time.Now())
		bucket.LockConfiguration = tc.config
		err := CheckBucketRetention(context.Background(), bucket, "anchors", 400)
		if tc.want == "" {
			require.NoError(t, err, tc.name)
		} else {
			require.ErrorContains(t, err, tc.want, tc.name)
		}
	}

	err := CheckBucketRetention(context.Background(), deniedLockReader{}, "anchors", 400)
	require.ErrorContains(t, err, "couldn't read the Object Lock configuration of s3://anchors: AccessDenied")
}
