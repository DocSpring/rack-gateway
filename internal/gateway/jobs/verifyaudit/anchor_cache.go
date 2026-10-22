package verifyaudit

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	jobaudit "github.com/DocSpring/rack-gateway/internal/gateway/jobs/audit"
)

// anchorCache keeps the anchor versions runs have read, so a run only fetches the versions written since the
// last one instead of one request per hour of history. It changes nothing a run checks: a version of an object
// never changes once written (an Object Lock bucket is always versioned), and every run still lists every
// version, so a new version or a delete marker shows up as before. A version without an ID (an unversioned
// bucket) or with the ID "null" (written while versioning was off, and replaceable) is always fetched. Anchors
// are a few hundred bytes, one an hour, and a bigger version is never read or kept (jobaudit.MaxAnchorSize).
type anchorCache struct {
	store  auditverify.AnchorStore
	mu     sync.Mutex
	bodies map[anchorVersion][]byte
}

var _ auditverify.AnchorStore = (*anchorCache)(nil)

type anchorVersion struct {
	bucket    string
	key       string
	versionID string
}

func newAnchorCache(store auditverify.AnchorStore) *anchorCache {
	return &anchorCache{store: store, bodies: map[anchorVersion][]byte{}}
}

// GetObjectLockConfiguration always reads the bucket's configuration.
func (c *anchorCache) GetObjectLockConfiguration(
	ctx context.Context,
	params *s3.GetObjectLockConfigurationInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectLockConfigurationOutput, error) {
	return c.store.GetObjectLockConfiguration(ctx, params, optFns...)
}

// ListObjectVersions always lists the bucket.
func (c *anchorCache) ListObjectVersions(
	ctx context.Context,
	params *s3.ListObjectVersionsInput,
	optFns ...func(*s3.Options),
) (*s3.ListObjectVersionsOutput, error) {
	return c.store.ListObjectVersions(ctx, params, optFns...)
}

// GetObject returns a version it has read before from memory, and fetches anything else.
func (c *anchorCache) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	version := anchorVersion{
		bucket:    aws.ToString(params.Bucket),
		key:       aws.ToString(params.Key),
		versionID: aws.ToString(params.VersionId),
	}
	cacheable := version.versionID != "" && version.versionID != "null"
	if body, ok := c.cached(version); cacheable && ok {
		return objectOutput(body), nil
	}

	output, err := c.store.GetObject(ctx, params, optFns...)
	if err != nil || !cacheable {
		return output, err
	}
	defer func() { _ = output.Body.Close() }()
	body, err := jobaudit.ReadAnchorBody(output.Body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.bodies[version] = body
	c.mu.Unlock()
	return objectOutput(body), nil
}

func (c *anchorCache) cached(version anchorVersion) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	body, ok := c.bodies[version]
	return body, ok
}

func objectOutput(body []byte) *s3.GetObjectOutput {
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body))}
}
