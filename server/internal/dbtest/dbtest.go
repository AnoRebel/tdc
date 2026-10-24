// Package dbtest gives each test package its own database.
//
// Every package's fixtures would otherwise collide: the packages run in
// parallel by default, and each clears the tables it uses, so one package
// deletes another's users midway through a test. Sharing one database also
// means a test that corrupts state — the migration test deliberately does —
// breaks unrelated packages.
//
// Each caller gets a database named after its package, created on demand from
// the configured server and migrated to the current schema.
package dbtest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db"
)

// Logger returns a logger that discards output, so test runs stay readable.
func Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// URL returns a migrated database reserved for the named suite, or skips the
// test when no database is configured.
//
// The name should be short and unique to the calling package.
func URL(t *testing.T, suite string) string {
	t.Helper()

	base := os.Getenv("TDC_TEST_DATABASE_URL")
	if base == "" {
		/*
			A skipped test passes, so in CI a missing database would turn
			every test that needs one green without running it — the replay
			of a project's stream among them, which is what stops an event
			type without a projection from being merged.
		*/
		if os.Getenv("CI") != "" {
			t.Fatal("TDC_TEST_DATABASE_URL is not set, and in CI a database test must run rather than skip")
		}
		t.Skip("set TDC_TEST_DATABASE_URL to run database tests")
	}

	/*
		Named per suite, and optionally per run.

		Two runs of the same package share its database, so a second run
		started while the first is going deletes the first's fixtures and each
		fails in ways that look like real bugs. TDC_TEST_DATABASE_PREFIX gives
		a run a namespace of its own — which is what lets several checkouts of
		this repository, each on a different branch with different migrations,
		test at once against one Postgres without migrating each other's
		databases out from under them.
	*/
	prefix := os.Getenv("TDC_TEST_DATABASE_PREFIX")
	if prefix == "" {
		prefix = "tdc_test_"
	}
	name := prefix + suite
	target, err := databaseURL(base, name)
	if err != nil {
		t.Fatalf("derive test database url: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := ensureDatabase(ctx, base, name); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	if err := db.Migrate(ctx, target, Logger()); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	return target
}

// databaseURL rewrites a connection string to name a different database.
func databaseURL(base, name string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

// ensureDatabase creates the database if it does not exist.
func ensureDatabase(ctx context.Context, base, name string) error {
	// Connect to the server's default database to issue CREATE DATABASE,
	// which cannot run inside a transaction or against the target itself.
	adminURL, err := databaseURL(base, "postgres")
	if err != nil {
		return err
	}

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect to administer databases: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check for database: %w", err)
	}
	if exists {
		return nil
	}

	// The name is derived from a package-local constant, not from input, and
	// CREATE DATABASE does not accept a parameter for its target.
	if _, err := conn.Exec(ctx, `CREATE DATABASE "`+strings.ReplaceAll(name, `"`, "")+`"`); err != nil {
		// A parallel package may have created it between the check and here.
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create database: %w", err)
		}
	}

	return nil
}
