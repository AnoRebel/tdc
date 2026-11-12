package config_test

import (
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

// TestStorageDefaultsToLocal checks that a server with no storage settings
// still works, which is what lets a small deployment skip the object store.
func TestStorageDefaultsToLocal(t *testing.T) {
	defaults := config.Default()

	if defaults.Storage.Backend != "local" {
		t.Errorf("the default backend is %q, expected local", defaults.Storage.Backend)
	}
	if defaults.Storage.Directory == "" {
		t.Error("the local backend has no default directory, so it cannot start unconfigured")
	}
}

// TestStorageCredentialsAreRedacted checks that object-store credentials never
// appear in the administrative view of configuration.
func TestStorageCredentialsAreRedacted(t *testing.T) {
	for _, key := range []string{"storage.s3.access_key", "storage.s3.secret_key"} {
		secret := "AKIAREALLOOKINGSECRET"
		if shown := config.Redact(key, secret); strings.Contains(shown, secret) {
			t.Errorf("%s was shown in full as %q", key, shown)
		}
	}

	// A non-secret setting is still shown, or the view is useless.
	if shown := config.Redact("storage.s3.bucket", "attachments"); shown != "attachments" {
		t.Errorf("the bucket name was redacted to %q; it is not a secret", shown)
	}
	if shown := config.Redact("storage.s3.endpoint", "https://s3.example.org"); shown != "https://s3.example.org" {
		t.Errorf("the endpoint was redacted to %q; it is not a secret", shown)
	}
}

// TestStorageAppearsInDescribe checks that the storage settings reach the
// administrative view, with the credentials redacted there.
//
// A setting that is loaded but never described is invisible to the operator
// who has to debug it, so the two lists have to stay in step.
func TestStorageAppearsInDescribe(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.Backend = "s3-minio"
	cfg.Storage.S3.Endpoint = "https://s3.example.org"
	cfg.Storage.S3.Bucket = "attachments"
	cfg.Storage.S3.AccessKey = "AKIAVISIBLEIDENTIFIER"
	cfg.Storage.S3.SecretKey = "a-real-looking-secret"

	described := map[string]config.Setting{}
	for _, setting := range cfg.Describe("") {
		described[setting.Key] = setting
	}

	for _, key := range []string{
		"storage.backend",
		"storage.directory",
		"storage.s3.endpoint",
		"storage.s3.bucket",
		"storage.s3.region",
		"storage.s3.access_key",
		"storage.s3.secret_key",
		"storage.s3.path_style",
	} {
		if _, ok := described[key]; !ok {
			t.Errorf("%s is missing from the administrative view", key)
		}
	}

	if shown := described["storage.s3.secret_key"].Value; strings.Contains(shown, "a-real-looking-secret") {
		t.Errorf("the secret key is shown in full as %q", shown)
	}
	if !described["storage.s3.secret_key"].Secret {
		t.Error("the secret key is not marked as a secret")
	}
	if shown := described["storage.s3.bucket"].Value; shown != "attachments" {
		t.Errorf("the bucket shows as %q; it is not a secret", shown)
	}
}

// TestStorageEnvironmentBinding checks that the documented variables actually
// reach the fields they name.
//
// The loader derives variable names from the struct shape, so a nested field
// that is never exercised can silently go unbound — the operator sets it, the
// server ignores it, and nothing says so.
func TestStorageEnvironmentBinding(t *testing.T) {
	t.Setenv("TDC_DATABASE_URL", "postgres://tdc:tdc@127.0.0.1:5432/tdc")
	t.Setenv("TDC_STORAGE_BACKEND", "s3-minio")
	t.Setenv("TDC_STORAGE_S3_ENDPOINT", "https://s3.example.org")
	t.Setenv("TDC_STORAGE_S3_BUCKET", "attachments")
	t.Setenv("TDC_STORAGE_S3_ACCESS_KEY", "an-access-key")
	t.Setenv("TDC_STORAGE_S3_SECRET_KEY", "a-secret-key")

	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, check := range []struct{ name, got, want string }{
		{"TDC_STORAGE_BACKEND", loaded.Storage.Backend, "s3-minio"},
		{"TDC_STORAGE_S3_ENDPOINT", loaded.Storage.S3.Endpoint, "https://s3.example.org"},
		{"TDC_STORAGE_S3_BUCKET", loaded.Storage.S3.Bucket, "attachments"},
		{"TDC_STORAGE_S3_ACCESS_KEY", loaded.Storage.S3.AccessKey, "an-access-key"},
		{"TDC_STORAGE_S3_SECRET_KEY", loaded.Storage.S3.SecretKey, "a-secret-key"},
	} {
		if check.got != check.want {
			t.Errorf("%s did not bind: got %q, set %q", check.name, check.got, check.want)
		}
	}
}
