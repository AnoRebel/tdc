package db_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

// TestMigrationsApplyFromEmpty checks that a fresh install works.
//
// Every migration is written against whatever schema its author happened to
// have, which is why an upgrade breaks for everybody else: a statement that
// depends on a column added later passes locally and fails on a clean database.
// Applying the whole sequence to an empty one is the only way to catch that.
//
// Applied twice, because a migration runner that is not idempotent turns an
// ordinary restart into an outage.
func TestMigrationsApplyFromEmpty(t *testing.T) {
	// A database of its own, dropped afterwards, so this starts genuinely empty
	// rather than on whatever the last test left behind.
	empty := dbtest.URL(t, "migrate_from_empty")
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := db.Migrate(context.Background(), empty, quiet); err != nil {
		t.Fatalf("applying migrations to an empty database: %v", err)
	}

	if err := db.Migrate(context.Background(), empty, quiet); err != nil {
		t.Fatalf("re-applying migrations: %v; the runner is not idempotent", err)
	}
}
