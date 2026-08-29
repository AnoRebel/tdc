package db_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// testDatabaseURL returns the database to test against, or skips.
//
// These tests need a real Postgres: the value of testing a data layer against
// a mock is close to zero, since the mistakes worth catching are the ones the
// database itself rejects.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TDC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TDC_TEST_DATABASE_URL to run database tests")
	}
	return url
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestOpenConnects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, db.Config{URL: testDatabaseURL(t)}, discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	if err := pool.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestOpenRejectsBadURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A misconfigured database must fail at startup rather than on the first
	// request, so an operator finds out immediately.
	if _, err := db.Open(ctx, db.Config{URL: "not-a-url"}, discardLogger()); err == nil {
		t.Fatal("Open with an invalid URL returned no error")
	}
}

// Migrating twice must be safe: servers restart, and every restart runs
// migrations.
func TestMigrateIsIdempotent(t *testing.T) {
	url := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := db.Migrate(ctx, url, discardLogger()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := db.Migrate(ctx, url, discardLogger()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

// The generated query layer must round-trip through a real database.
func TestGeneratedQueriesRoundTrip(t *testing.T) {
	url := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := db.Migrate(ctx, url, discardLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pool, err := db.Open(ctx, db.Config{URL: url}, discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	queries := dbgen.New(pool)
	const key = "test.round.trip"
	t.Cleanup(func() {
		_ = queries.DeleteServerConfigValue(context.Background(), key)
	})

	actor := "test"
	if err := queries.SetServerConfigValue(ctx, dbgen.SetServerConfigValueParams{
		Key:       key,
		Value:     "first",
		IsSecret:  false,
		UpdatedBy: &actor,
	}); err != nil {
		t.Fatalf("SetServerConfigValue: %v", err)
	}

	stored, err := queries.GetServerConfigValue(ctx, key)
	if err != nil {
		t.Fatalf("GetServerConfigValue: %v", err)
	}
	if stored.Value != "first" {
		t.Errorf("value = %q, want %q", stored.Value, "first")
	}
	if stored.UpdatedBy == nil || *stored.UpdatedBy != actor {
		t.Errorf("updatedBy = %v, want %q", stored.UpdatedBy, actor)
	}

	// Upsert must replace rather than duplicate: the key is the identity.
	if err := queries.SetServerConfigValue(ctx, dbgen.SetServerConfigValueParams{
		Key:       key,
		Value:     "second",
		IsSecret:  true,
		UpdatedBy: &actor,
	}); err != nil {
		t.Fatalf("SetServerConfigValue (update): %v", err)
	}

	updated, err := queries.GetServerConfigValue(ctx, key)
	if err != nil {
		t.Fatalf("GetServerConfigValue after update: %v", err)
	}
	if updated.Value != "second" {
		t.Errorf("value after update = %q, want %q", updated.Value, "second")
	}
	if !updated.IsSecret {
		t.Error("isSecret = false after update, want true")
	}
}

// A binary must refuse a database migrated by a newer release rather than
// operating on a schema it does not understand. This is what makes rolling
// back to a previous version safe.
func TestMigrateRefusesNewerSchema(t *testing.T) {
	url := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := db.Migrate(ctx, url, discardLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pool, err := db.Open(ctx, db.Config{URL: url}, discardLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Registered as cleanup rather than deferred, so that it runs after the
	// version-restoring cleanup below: t.Cleanup functions run last-registered
	// first, whereas a defer here would close the pool before they run.
	t.Cleanup(pool.Close)

	// Simulate a newer release having migrated this database.
	//
	// Cleanup restores the version by re-deriving it from the migrations this
	// binary carries, rather than from whatever was read at the start: if a
	// previous run of this test failed to clean up, the value read here would
	// itself be the corrupted one, and restoring it would leave the database
	// permanently unusable for every later test.
	const impossibleVersion = 1_000_000
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET version = $1", impossibleVersion); err != nil {
		t.Fatalf("raise schema version: %v", err)
	}
	t.Cleanup(func() {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer restoreCancel()
		if _, err := pool.Exec(restoreCtx,
			"UPDATE schema_migrations SET version = $1, dirty = FALSE",
			db.LatestSchemaVersion(),
		); err != nil {
			t.Errorf("restore schema version: %v", err)
		}
	})

	err = db.Migrate(ctx, url, discardLogger())
	if err == nil {
		t.Fatal("Migrate accepted a schema newer than the binary")
	}
	if !errors.Is(err, db.ErrSchemaNewerThanBinary) {
		t.Fatalf("error = %v, want ErrSchemaNewerThanBinary", err)
	}
}
