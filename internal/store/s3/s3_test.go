package s3_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/snapaddy/nx-cache-server/internal/store"
	s3store "github.com/snapaddy/nx-cache-server/internal/store/s3"
)

type fakeClient struct {
	getIn  *awss3.GetObjectInput
	getOut *awss3.GetObjectOutput
	getErr error

	putIn  *awss3.PutObjectInput
	putErr error
}

func (f *fakeClient) GetObject(_ context.Context, in *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	f.getIn = in

	if f.getErr != nil {
		return nil, f.getErr
	}

	return f.getOut, nil
}

func (f *fakeClient) PutObject(_ context.Context, in *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	f.putIn = in

	if f.putErr != nil {
		return nil, f.putErr
	}

	return &awss3.PutObjectOutput{}, nil
}

func TestGet(t *testing.T) {
	t.Parallel()

	body := io.NopCloser(strings.NewReader("artifact"))
	client := &fakeClient{
		getOut: &awss3.GetObjectOutput{Body: body, ContentLength: aws.Int64(8)},
	}
	s := s3store.New(client, "bucket", "prefix")

	rc, size, err := s.Get(t.Context(), "hash1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got := aws.ToString(client.getIn.Bucket); got != "bucket" {
		t.Errorf("bucket = %q, want %q", got, "bucket")
	}

	if got := aws.ToString(client.getIn.Key); got != "prefix/hash1" {
		t.Errorf("key = %q, want %q", got, "prefix/hash1")
	}

	if size != 8 {
		t.Errorf("size = %d, want 8", size)
	}

	if rc != body {
		t.Error("body not passed through")
	}
}

func TestGetNoPrefix(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		getOut: &awss3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("")), ContentLength: aws.Int64(0)},
	}
	s := s3store.New(client, "bucket", "")

	if _, _, err := s.Get(t.Context(), "hash1"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got := aws.ToString(client.getIn.Key); got != "hash1" {
		t.Errorf("key = %q, want %q", got, "hash1")
	}
}

func TestGetNotFound(t *testing.T) {
	t.Parallel()

	client := &fakeClient{getErr: &types.NoSuchKey{}}
	s := s3store.New(client, "bucket", "")

	_, _, err := s.Get(t.Context(), "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetOtherError(t *testing.T) {
	t.Parallel()

	client := &fakeClient{getErr: &smithy.GenericAPIError{Code: "AccessDenied"}}
	s := s3store.New(client, "bucket", "")

	_, _, err := s.Get(t.Context(), "hash1")
	if err == nil {
		t.Fatal("err = nil, want error")
	}

	if errors.Is(err, store.ErrNotFound) {
		t.Error("unrelated error mapped to ErrNotFound")
	}
}

func TestPut(t *testing.T) {
	t.Parallel()

	client := &fakeClient{}
	s := s3store.New(client, "bucket", "prefix")

	err := s.Put(t.Context(), "hash1", 8, strings.NewReader("artifact"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if got := aws.ToString(client.putIn.Key); got != "prefix/hash1" {
		t.Errorf("key = %q, want %q", got, "prefix/hash1")
	}

	if got := aws.ToInt64(client.putIn.ContentLength); got != 8 {
		t.Errorf("content length = %d, want 8", got)
	}

	if got := aws.ToString(client.putIn.IfNoneMatch); got != "*" {
		t.Errorf("if-none-match = %q, want %q", got, "*")
	}
}

func TestPutAlreadyExists(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"PreconditionFailed", "ConditionalRequestConflict"} {
		client := &fakeClient{putErr: &smithy.GenericAPIError{Code: code}}
		s := s3store.New(client, "bucket", "")

		err := s.Put(t.Context(), "hash1", 1, strings.NewReader("a"))
		if !errors.Is(err, store.ErrAlreadyExists) {
			t.Errorf("code %s: err = %v, want ErrAlreadyExists", code, err)
		}
	}
}

func TestPutOtherError(t *testing.T) {
	t.Parallel()

	client := &fakeClient{putErr: &smithy.GenericAPIError{Code: "AccessDenied"}}
	s := s3store.New(client, "bucket", "")

	err := s.Put(t.Context(), "hash1", 1, strings.NewReader("a"))
	if err == nil {
		t.Fatal("err = nil, want error")
	}

	if errors.Is(err, store.ErrAlreadyExists) {
		t.Error("unrelated error mapped to ErrAlreadyExists")
	}
}
