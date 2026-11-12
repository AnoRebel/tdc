package storage_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/storage"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

/*
The S3 implementations are tested against a real S3-compatible server rather
than a mock.

A mock would prove only that the code calls the functions the test expects it to
call. What actually needs proving is that both clients behave identically
against a store an operator would really run — that a missing object surfaces as
ErrNotFound from each, that presigned URLs are followable, that a declared size
is honoured. None of that can be established without a server on the other end.

Point TDC_TEST_S3_ENDPOINT at one to run these; they skip otherwise, so a
contributor without Docker is not blocked. See the storage documentation for the
one-line command to start one.
*/

const (
	testS3EndpointVar = "TDC_TEST_S3_ENDPOINT"
	// SeaweedFS's S3 gateway accepts any credentials when none are configured,
	// which is what makes it convenient for tests.
	testS3AccessKey = "tdc-test-access-key"
	testS3SecretKey = "tdc-test-secret-key"
)

// s3TestConfig returns the configuration for the test store, or skips.
func s3TestConfig(t *testing.T) storage.S3Config {
	t.Helper()

	endpoint := os.Getenv(testS3EndpointVar)
	if endpoint == "" {
		t.Skipf("set %s to run the S3 tests; see the storage documentation", testS3EndpointVar)
	}

	// A bucket per test run, so repeated runs and parallel packages do not
	// inherit each other's objects.
	bucket := fmt.Sprintf("tdc-test-%d", time.Now().UnixNano())

	config := storage.S3Config{
		Endpoint:  endpoint,
		Bucket:    bucket,
		Region:    "us-east-1",
		AccessKey: testS3AccessKey,
		SecretKey: testS3SecretKey,
		// Self-hosted stores are addressed by path; a bare host without dots
		// cannot do virtual-host style at all.
		UsePathStyle: true,
	}

	createTestBucket(t, config)
	return config
}

// createTestBucket makes the bucket the test will use.
//
// Uses minio-go rather than a hand-rolled request because bucket creation needs
// a signed request, and reimplementing SigV4 in a test would prove nothing. A
// failure here is the environment, not the code under test, so it skips rather
// than fails.
func createTestBucket(t *testing.T, config storage.S3Config) {
	t.Helper()

	host, err := hostOf(config.Endpoint)
	if err != nil {
		t.Fatalf("parsing the endpoint: %v", err)
	}

	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure:       strings.HasPrefix(config.Endpoint, "https://"),
		Region:       config.Region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Skipf("no S3 server at %s: %v", config.Endpoint, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.MakeBucket(ctx, config.Bucket, minio.MakeBucketOptions{Region: config.Region}); err != nil {
		t.Skipf("creating bucket %q at %s: %v", config.Bucket, config.Endpoint, err)
	}
}

// hostOf strips the scheme from an endpoint, which is the form minio-go wants.
func hostOf(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("endpoint %q names no host", endpoint)
	}
	return parsed.Host, nil
}

// TestAWSStore runs the shared conformance suite against the AWS SDK client.
func TestAWSStore(t *testing.T) {
	RunConformance(t, func(t *testing.T) storage.Store {
		t.Helper()

		store, err := storage.NewAWS(s3TestConfig(t))
		if err != nil {
			t.Fatalf("opening the store: %v", err)
		}
		t.Cleanup(func() { store.Close() })

		return store
	})
}

// TestMinioStore runs the same suite against minio-go.
//
// Identical to the test above but for the constructor, which is the point: the
// two are interchangeable, and holding them to one suite is what keeps them so.
func TestMinioStore(t *testing.T) {
	RunConformance(t, func(t *testing.T) storage.Store {
		t.Helper()

		store, err := storage.NewMinio(s3TestConfig(t))
		if err != nil {
			t.Fatalf("opening the store: %v", err)
		}
		t.Cleanup(func() { store.Close() })

		return store
	})
}

// TestPresignedURLsAreFollowable checks the property the whole download path
// depends on.
//
// The server hands a browser a presigned URL and redirects to it. A browser
// sends the headers it always sends and nothing more, so if a client signs a
// header the browser cannot supply, every attachment download breaks. This
// fetches the URL with a plain HTTP client — no signing, no extra headers —
// which is the closest approximation of that browser.
func TestPresignedURLsAreFollowable(t *testing.T) {
	config := s3TestConfig(t)
	body := "the bytes a browser should receive"

	for _, backend := range []struct {
		name string
		open func(storage.S3Config) (storage.Store, error)
	}{
		{"aws", func(c storage.S3Config) (storage.Store, error) { return storage.NewAWS(c) }},
		{"minio", func(c storage.S3Config) (storage.Store, error) { return storage.NewMinio(c) }},
	} {
		t.Run(backend.name, func(t *testing.T) {
			store, err := backend.open(config)
			if err != nil {
				t.Fatalf("opening the store: %v", err)
			}
			defer store.Close()

			ctx := context.Background()
			key := backend.name + "-presigned"

			if err := store.Put(ctx, key, stringReader(body), int64(len(body)), "text/plain"); err != nil {
				t.Fatalf("put: %v", err)
			}

			url, err := store.PresignGet(ctx, key, 5*time.Minute, "notes.txt")
			if err != nil {
				t.Fatalf("presign: %v", err)
			}
			if url == "" {
				t.Fatal("an S3 store returned no presigned URL")
			}

			// Deliberately a bare client: no credentials, no signing, no added
			// headers — exactly what a browser following a redirect sends.
			response, err := (&http.Client{Timeout: 15 * time.Second}).Get(url)
			if err != nil {
				t.Fatalf("following the presigned URL: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusOK {
				t.Fatalf("presigned URL gave HTTP %d; a browser redirect would fail here",
					response.StatusCode)
			}

			received := make([]byte, len(body))
			if _, err := readFull(response.Body, received); err != nil {
				t.Fatalf("reading the response: %v", err)
			}
			if string(received) != body {
				t.Errorf("presigned URL served %q, stored %q", received, body)
			}
		})
	}
}

// TestS3RefusesTraversalKeys checks that the key rules apply to the S3 stores
// too, not only to the filesystem.
//
// A key is part of a URL here rather than a path, so traversal cannot escape a
// directory — but a key that resolves differently on the client and the server
// is still a way to reach an object the caller was not authorised for, and the
// interface promises one set of rules whichever store is behind it.
func TestS3RefusesTraversalKeys(t *testing.T) {
	config := s3TestConfig(t)

	stores := map[string]func() (storage.Store, error){
		"aws":   func() (storage.Store, error) { return storage.NewAWS(config) },
		"minio": func() (storage.Store, error) { return storage.NewMinio(config) },
	}

	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			store, err := open()
			if err != nil {
				t.Fatalf("opening the store: %v", err)
			}
			defer store.Close()

			ctx := context.Background()
			for _, key := range []string{"", "../escaped", "nested/../../escaped"} {
				if err := store.Put(ctx, key, stringReader("x"), 1, "text/plain"); err == nil {
					t.Errorf("Put accepted %q", key)
				}
				if _, err := store.PresignGet(ctx, key, time.Minute, ""); err == nil {
					t.Errorf("PresignGet accepted %q", key)
				}
			}
		})
	}
}

// TestS3ConfigValidation covers the configuration errors an operator is most
// likely to make, without needing a server.
func TestS3ConfigValidation(t *testing.T) {
	complete := storage.S3Config{
		Endpoint:  "https://s3.example.org",
		Bucket:    "attachments",
		AccessKey: "key",
		SecretKey: "secret",
	}

	if err := complete.Validate(); err != nil {
		t.Errorf("a complete configuration was rejected: %v", err)
	}

	for name, config := range map[string]storage.S3Config{
		"no endpoint":      {Bucket: "b", AccessKey: "k", SecretKey: "s"},
		"no bucket":        {Endpoint: "https://s3.example.org", AccessKey: "k", SecretKey: "s"},
		"no access key":    {Endpoint: "https://s3.example.org", Bucket: "b", SecretKey: "s"},
		"no secret key":    {Endpoint: "https://s3.example.org", Bucket: "b", AccessKey: "k"},
		"endpoint no host": {Endpoint: "https://", Bucket: "b", AccessKey: "k", SecretKey: "s"},
		"endpoint scheme":  {Endpoint: "ftp://s3.example.org", Bucket: "b", AccessKey: "k", SecretKey: "s"},
	} {
		if err := config.Validate(); err == nil {
			t.Errorf("%s: accepted, should have been refused", name)
		}
	}

	// Every problem at once, so an operator fixes them in one pass rather than
	// one restart at a time.
	err := storage.S3Config{}.Validate()
	if err == nil {
		t.Fatal("an empty configuration was accepted")
	}
	for _, expected := range []string{"endpoint", "bucket", "access key", "secret key"} {
		if !contains(err.Error(), expected) {
			t.Errorf("the error does not mention %q: %v", expected, err)
		}
	}
}

// TestOpenSelectsBackend covers the factory's routing and its refusal of an
// unknown name.
func TestOpenSelectsBackend(t *testing.T) {
	t.Run("defaults to local", func(t *testing.T) {
		store, err := storage.Open(storage.Config{Directory: t.TempDir()})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer store.Close()

		if _, ok := store.(*storage.Local); !ok {
			t.Errorf("an unset backend gave %T, expected the local store", store)
		}
	})

	t.Run("refuses an unknown backend", func(t *testing.T) {
		_, err := storage.Open(storage.Config{Backend: "gopher"})
		if err == nil {
			t.Fatal("an unknown backend was accepted")
		}
		// The message should say what is allowed, not only that this is not.
		for _, expected := range []string{"local", "s3-aws", "s3-minio"} {
			if !contains(err.Error(), expected) {
				t.Errorf("the error does not offer %q: %v", expected, err)
			}
		}
	})
}

// Small helpers, kept here so the tests above read as prose.

func stringReader(s string) io.Reader { return strings.NewReader(s) }

func readFull(r io.Reader, into []byte) (int, error) { return io.ReadFull(r, into) }

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
