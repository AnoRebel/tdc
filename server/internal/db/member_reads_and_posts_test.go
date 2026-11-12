package db_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

/*
TestAnExistingMemberRoleGainsReadAndPost checks the upgrade, not the install.

Sending and reading are now refused without channel.post and channel.read. A
fresh server's Member role is created with both; a server that already existed
has a Member role made before either was enforced, and without this migration
everybody on it would be locked out of every channel the moment it upgraded.

So the database is taken back to the version before, given a Member role the way
an existing server has one — including a denial an operator already made for one
channel, which the upgrade must leave alone — and only then brought up to date.
*/
func TestAnExistingMemberRoleGainsReadAndPost(t *testing.T) {
	ctx := context.Background()
	databaseURL := dbtest.URL(t, "member_reads_and_posts")

	// The same files the binary embeds, read from where they are kept.
	source, err := iofs.New(os.DirFS("migrations"), ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	migrator, err := migrate.NewWithSourceInstance("iofs", source, pgx5(t, databaseURL))
	if err != nil {
		t.Fatalf("open migrator: %v", err)
	}
	if err := migrator.Migrate(104); err != nil {
		t.Fatalf("migrating to the version before: %v", err)
	}
	if sourceErr, dbErr := migrator.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("closing migrator: %v, %v", sourceErr, dbErr)
	}

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const (
		member       = "00000000-0000-7000-8000-000000000001"
		announcement = "00000000-0000-7000-8000-0000000000a1"
	)
	// The suite's database outlives a run, so the last run's role goes first.
	if _, err := conn.Exec(ctx, `DELETE FROM roles WHERE name = 'Member'`); err != nil {
		t.Fatalf("clearing the last run's role: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO roles (id, name, built_in) VALUES ($1, 'Member', TRUE)`, member); err != nil {
		t.Fatalf("creating the Member role an existing server has: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO role_permissions (role_id, permission, scope_id, granted)
		 VALUES ($1, 'channel.create', NULL, TRUE),
		        ($1, 'channel.post', $2, FALSE)`, member, announcement); err != nil {
		t.Fatalf("granting what an existing Member role holds: %v", err)
	}

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, databaseURL, quiet); err != nil {
		t.Fatalf("upgrading: %v", err)
	}

	held := func(permission string, scope *string) (granted, present bool) {
		err := conn.QueryRow(ctx,
			`SELECT granted FROM role_permissions
			 WHERE role_id = $1 AND permission = $2 AND scope_id IS NOT DISTINCT FROM $3::uuid`,
			member, permission, scope).Scan(&granted)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false
		}
		if err != nil {
			t.Fatalf("reading %s: %v", permission, err)
		}
		return granted, true
	}

	for _, permission := range []string{"channel.read", "channel.post"} {
		if granted, present := held(permission, nil); !present || !granted {
			t.Errorf("after the upgrade the Member role does not hold %s at server scope; "+
				"everybody on an existing server would be locked out", permission)
		}
	}

	scope := announcement
	if granted, present := held("channel.post", &scope); !present || granted {
		t.Error("the upgrade overrode a channel an operator had already made read-only")
	}
}

// pgx5 names the driver golang-migrate registers for pgx, which db imports.
func pgx5(t *testing.T, databaseURL string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.Scheme = "pgx5"
	return parsed.String()
}
