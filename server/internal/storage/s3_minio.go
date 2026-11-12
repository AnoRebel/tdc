package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioStore keeps objects in an S3-compatible bucket using minio-go.
//
// The other of two interchangeable S3 implementations: a smaller dependency and
// a simpler surface than the AWS SDK. The MinIO *server* is excluded from this
// project — archived in April 2026 carrying an unpatched vulnerability — but
// this is the client library, which is separately maintained, Apache-2.0, and
// speaks to any S3-compatible store.
type MinioStore struct {
	client *minio.Client
	bucket string
}

// NewMinio opens a store backed by an S3-compatible bucket.
func NewMinio(config S3Config) (*MinioStore, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	host, secure, err := config.hostAndTLS()
	if err != nil {
		return nil, err
	}

	region := config.Region
	if region == "" {
		// As with the AWS client: self-hosted stores generally ignore the
		// region, but signing needs one.
		region = "us-east-1"
	}

	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure:       secure,
		Region:       region,
		BucketLookup: bucketLookup(config.UsePathStyle),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: connecting to %q: %w", config.Endpoint, err)
	}

	return &MinioStore{client: client, bucket: config.Bucket}, nil
}

func bucketLookup(pathStyle bool) minio.BucketLookupType {
	if pathStyle {
		return minio.BucketLookupPath
	}
	return minio.BucketLookupDNS
}

func (s *MinioStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := validKey(key); err != nil {
		return err
	}

	// Bounded by the declared size, as elsewhere: a body that claims one length
	// and sends more must not store more than the limits allowed.
	_, err := s.client.PutObject(ctx, s.bucket, key, io.LimitReader(r, size), size,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("storage: storing %q: %w", key, err)
	}

	return nil
}

func (s *MinioStore) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	if err := validKey(key); err != nil {
		return nil, nil, err
	}

	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: reading %q: %w", key, err)
	}

	// GetObject is lazy: it returns without contacting the store, so a missing
	// object only surfaces on the first read or stat. Stat here, so the caller
	// learns it is missing now rather than part-way through a response body.
	info, err := object.Stat()
	if err != nil {
		object.Close()
		if isMinioNotFound(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("storage: reading %q: %w", key, err)
	}

	return object, &Object{
		Key:         key,
		Size:        info.Size,
		ContentType: info.ContentType,
		ModifiedAt:  info.LastModified,
	}, nil
}

func (s *MinioStore) Stat(ctx context.Context, key string) (*Object, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}

	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isMinioNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: reading %q: %w", key, err)
	}

	return &Object{
		Key:         key,
		Size:        info.Size,
		ContentType: info.ContentType,
		ModifiedAt:  info.LastModified,
	}, nil
}

func (s *MinioStore) Delete(ctx context.Context, key string) error {
	if err := validKey(key); err != nil {
		return err
	}

	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		if isMinioNotFound(err) {
			return nil
		}
		return fmt.Errorf("storage: deleting %q: %w", key, err)
	}

	return nil
}

func (s *MinioStore) PresignGet(ctx context.Context, key string, ttl time.Duration, downloadName string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}

	parameters := url.Values{}
	if disposition := contentDisposition(downloadName); disposition != "" {
		parameters.Set("response-content-disposition", disposition)
	}

	presigned, err := s.client.PresignedGetObject(ctx, s.bucket, key, ttl, parameters)
	if err != nil {
		return "", fmt.Errorf("storage: presigning %q: %w", key, err)
	}

	return presigned.String(), nil
}

func (s *MinioStore) Close() error { return nil }

// isMinioNotFound reports whether an error means the object is not there.
func isMinioNotFound(err error) bool {
	response := minio.ToErrorResponse(err)
	switch response.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return true
	}
	return response.StatusCode == http.StatusNotFound
}
