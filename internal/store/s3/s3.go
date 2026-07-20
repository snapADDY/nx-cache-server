// Package s3 provides an S3-backed Store implementation.
//
// Overwrite protection uses conditional writes (If-None-Match: "*"),
// which requires AWS S3 or MinIO >= RELEASE.2024-08.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/snapaddy/nx-cache-server/internal/store"
)

// Client is the subset of *s3.Client used by Store, extracted as an
// interface so tests can substitute a fake.
type Client interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Store implements store.Store on top of an S3 bucket.
type Store struct {
	client Client
	bucket string
	prefix string
}

var _ store.Store = (*Store)(nil)

func New(client Client, bucket, prefix string) *Store {
	return &Store{client: client, bucket: bucket, prefix: prefix}
}

func (s *Store) objectKey(key string) string {
	return path.Join(s.prefix, key)
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
	})
	if err != nil {
		if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
			return nil, 0, fmt.Errorf("get object %q: %w", key, store.ErrNotFound)
		}

		return nil, 0, fmt.Errorf("get object %q: %w", key, err)
	}

	return out.Body, aws.ToInt64(out.ContentLength), nil
}

func (s *Store) Put(ctx context.Context, key string, size int64, r io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
		Body:   r,
		// An explicit content length lets the SDK stream the non-seekable
		// request body without buffering it.
		ContentLength: aws.Int64(size),
		// Atomic overwrite protection: S3 rejects the write with
		// 412 PreconditionFailed if the object already exists, or with
		// 409 ConditionalRequestConflict on concurrent conditional writes.
		IfNoneMatch: aws.String("*"),
	})
	if err != nil {
		if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
			switch apiErr.ErrorCode() {
			case "PreconditionFailed", "ConditionalRequestConflict":
				return fmt.Errorf("put object %q: %w", key, store.ErrAlreadyExists)
			}
		}

		return fmt.Errorf("put object %q: %w", key, err)
	}

	return nil
}
