package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tdc.conf")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

const validDatabaseURL = "postgres://tdc:secret@localhost:5432/tdc"

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "database.url = "+validDatabaseURL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address != config.Default().Server.Address {
		t.Errorf("address = %q, want the default %q",
			cfg.Server.Address, config.Default().Server.Address)
	}
	if !cfg.Database.AutoMigrate {
		t.Error("auto_migrate = false, want the default true")
	}
}

// An operator correcting a deployment should not have to restart once per
// mistake, so every problem is reported together.
func TestLoadReportsEveryProblem(t *testing.T) {
	path := writeConfig(t, strings.Join([]string{
		"database.url = " + validDatabaseURL,
		"server.read_timeout = not-a-duration",
		"log.level = shouty",
		"database.max_connections = many",
	}, "\n"))

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load accepted invalid configuration")
	}

	var invalid *config.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("error type = %T, want *config.InvalidError", err)
	}
	if len(invalid.Problems) != 3 {
		t.Fatalf("reported %d problems, want 3: %v", len(invalid.Problems), invalid.Problems)
	}

	reported := map[string]bool{}
	for _, p := range invalid.Problems {
		reported[p.Key] = true
	}
	for _, key := range []string{"server.read_timeout", "log.level", "database.max_connections"} {
		if !reported[key] {
			t.Errorf("problem for %q not reported", key)
		}
	}
}

// The database URL is required: a server that starts without one would fail on
// its first request instead of at startup, where an operator is watching.
func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := config.Load(writeConfig(t, "server.name = TDC"))
	if err == nil {
		t.Fatal("Load accepted configuration with no database URL")
	}
	if !strings.Contains(err.Error(), "database.url") {
		t.Errorf("error does not mention database.url: %v", err)
	}
}

func TestEnvironmentOverridesFile(t *testing.T) {
	path := writeConfig(t, strings.Join([]string{
		"database.url = " + validDatabaseURL,
		"server.address = :9999",
	}, "\n"))

	t.Setenv("TDC_SERVER_ADDRESS", ":7777")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address != ":7777" {
		t.Errorf("address = %q, want the environment value %q", cfg.Server.Address, ":7777")
	}
}

func TestLoadWithoutFileUsesEnvironment(t *testing.T) {
	t.Setenv("TDC_DATABASE_URL", validDatabaseURL)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.URL != validDatabaseURL {
		t.Errorf("database url = %q, want %q", cfg.Database.URL, validDatabaseURL)
	}
}

// A path the operator gave explicitly must exist: silently ignoring it would
// run a server with configuration the operator believes was applied.
func TestLoadFailsOnMissingNamedFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "absent.conf"))
	if err == nil {
		t.Fatal("Load accepted a path that does not exist")
	}
	if !config.IsMissingFile(err) {
		t.Errorf("IsMissingFile = false for %v", err)
	}
}

func TestRedactRemovesDatabasePassword(t *testing.T) {
	redacted := config.Redact("database.url", validDatabaseURL)

	if strings.Contains(redacted, "secret") {
		t.Fatalf("redacted value still contains the password: %q", redacted)
	}
	// The host and database name must survive, so an operator can still
	// confirm what the server is pointed at.
	if !strings.Contains(redacted, "localhost:5432") || !strings.Contains(redacted, "/tdc") {
		t.Errorf("redacted value lost identifying detail: %q", redacted)
	}
}

func TestDescribeRedactsSecretsAndNamesSources(t *testing.T) {
	path := writeConfig(t, strings.Join([]string{
		"database.url = " + validDatabaseURL,
		"server.name = From File",
	}, "\n"))
	t.Setenv("TDC_LOG_LEVEL", "debug")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	sources := map[string]config.Setting{}
	for _, setting := range cfg.Describe(path) {
		sources[setting.Key] = setting
	}

	if got := sources["database.url"].Value; strings.Contains(got, "secret") {
		t.Errorf("database.url displayed with its password: %q", got)
	}
	if got := sources["server.name"].Source; got != "file" {
		t.Errorf("server.name source = %q, want %q", got, "file")
	}
	if got := sources["log.level"].Source; got != "environment" {
		t.Errorf("log.level source = %q, want %q", got, "environment")
	}
	if got := sources["server.address"].Source; got != "default" {
		t.Errorf("server.address source = %q, want %q", got, "default")
	}
}

func TestWarningsFlagUnsafeSettings(t *testing.T) {
	t.Setenv("TDC_DATABASE_URL", validDatabaseURL+"?sslmode=disable")
	t.Setenv("TDC_SERVER_EXPOSE_INTERNAL_ERRORS", "true")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	warnings := strings.Join(cfg.Warnings(), "\n")
	for _, want := range []string{"expose_internal_errors", "canonical_address", "disables TLS"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings do not mention %q:\n%s", want, warnings)
		}
	}
}
