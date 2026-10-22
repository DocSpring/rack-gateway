package verifyaudit

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"

	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/fakes3"
)

// The cache keeps anchor-sized bodies only: a bigger version is refused, and never kept, after reading no more than
// an anchor can hold.
func TestAnchorCacheRefusesOversizedVersions(t *testing.T) {
	ctx := context.Background()
	bucket := fakes3.New(firstRun)
	key := jobaudit.AnchorKey(testChainID, firstRun)
	bucket.PutAt(key, make([]byte, jobaudit.MaxAnchorSize+1), firstRun)
	listing, err := bucket.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(testBucket)})
	require.NoError(t, err)
	require.Len(t, listing.Versions, 1)

	cache := newAnchorCache(bucket)
	_, err = cache.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket), Key: aws.String(key), VersionId: listing.Versions[0].VersionId,
	})
	require.ErrorIs(t, err, jobaudit.ErrAnchorTooLarge)
	require.Empty(t, cache.bodies)
}
