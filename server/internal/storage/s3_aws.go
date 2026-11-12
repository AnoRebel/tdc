package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// AWSStore keeps objects in an S3-compatible bucket using the official AWS SDK.
//
// One of two interchangeable S3 implementations. This one suits an operator
// already running the AWS SDK elsewhere, or one who wants the client AWS itself
// maintains; the other is smaller. They are held to the same conformance tests,
// so the choice is preference rather than capability.
type AWSStore struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

// NewAWS opens a store backed by an S3-compatible bucket.
func NewAWS(config S3Config) (*AWSStore, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	region := config.Region
	if region == "" {
		// Self-hosted stores generally ignore the region, but the signing
		// algorithm requires one, so a placeholder keeps signatures valid
		// rather than making every operator invent a value.
		region = "us-east-1"
	}

	client := s3.New(s3.Options{
		Region:       region,
		BaseEndpoint: aws.String(config.Endpoint),
		UsePathStyle: config.UsePathStyle,
		Credentials: credentials.NewStaticCredentialsProvider(
			config.AccessKey, config.SecretKey, "",
		),
	})

	return &AWSStore{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    config.Bucket,
	}, nil
}

func (s *AWSStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := validKey(key); err != nil {
		return err
	}

	// The SDK signs the payload hash, so it reads the body once to hash it and
	// again to send it, and needs to seek back to the start between the two. A
	// plain reader cannot, and the request fails before it ever leaves the
	// process — so the body is always materialised into something seekable.
	//
	// Bounded by the declared size, so a body claiming one length and sending
	// more cannot store more than the operator's limits allowed. Uploads are
	// already checked against those limits before reaching this point, which is
	// what keeps the buffer bounded.
	buffered, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return fmt.Errorf("storage: reading %q for upload: %w", key, err)
	}
	if int64(len(buffered)) != size {
		return fmt.Errorf("storage: %q declared %d bytes but supplied %d",
			key, size, len(buffered))
	}
	body := bytes.NewReader(buffered)

	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	}); err != nil {
		return fmt.Errorf("storage: storing %q: %w", key, err)
	}

	return nil
}

func (s *AWSStore) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	if err := validKey(key); err != nil {
		return nil, nil, err
	}

	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isAWSNotFound(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("storage: reading %q: %w", key, err)
	}

	object := &Object{
		Key:         key,
		Size:        aws.ToInt64(output.ContentLength),
		ContentType: aws.ToString(output.ContentType),
	}
	if output.LastModified != nil {
		object.ModifiedAt = *output.LastModified
	}

	return output.Body, object, nil
}

func (s *AWSStore) Stat(ctx context.Context, key string) (*Object, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}

	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isAWSNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: reading %q: %w", key, err)
	}

	object := &Object{
		Key:         key,
		Size:        aws.ToInt64(output.ContentLength),
		ContentType: aws.ToString(output.ContentType),
	}
	if output.LastModified != nil {
		object.ModifiedAt = *output.LastModified
	}

	return object, nil
}

func (s *AWSStore) Delete(ctx context.Context, key string) error {
	if err := validKey(key); err != nil {
		return err
	}

	// S3 treats deleting a missing key as success, which is the behaviour the
	// interface asks for.
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage: deleting %q: %w", key, err)
	}

	return nil
}

// PresignGet returns a URL the browser follows directly to the object store.
//
// The server redirects to it rather than streaming the bytes itself, which
// keeps a large download off the application's connections entirely.
//
// An earlier revision of the design avoided this SDK because it signed
// `amz-sdk-request` into the URL, which a browser cannot send on a redirect.
// That was a cross-module version mismatch, fixed upstream; the current SDK
// signs only `host`, which the browser supplies itself.
func (s *AWSStore) PresignGet(ctx context.Context, key string, ttl time.Duration, downloadName string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if disposition := contentDisposition(downloadName); disposition != "" {
		input.ResponseContentDisposition = aws.String(disposition)
	}

	request, err := s.presigner.PresignGetObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presigning %q: %w", key, err)
	}

	return request.URL, nil
}

func (s *AWSStore) Close() error { return nil }

// isAWSNotFound reports whether an error means the object is not there.
//
// Checked by type for GetObject, which returns a typed NoSuchKey, and by error
// code for HeadObject, which has no body to decode one from and so surfaces a
// bare 404.
func isAWSNotFound(err error) bool {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}

	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}

	return false
}
