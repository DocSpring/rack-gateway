package audit

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client is an interface for S3 operations needed by anchor writer
// This allows mocking in tests
type S3Client interface {
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// Ensure *s3.Client implements S3Client
var _ S3Client = (*s3.Client)(nil)

