package db_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

/*
TestDirectoryLocationsMoveToTheAccount checks the upgrade that retired the
directory's own location.

Each location becomes the account's, at a visibility no looser than the
profile's details had: what members could see stays members-only, what sat in
a hidden profile becomes hidden. Nothing may become more visible by upgrading.
*/
func TestDirectoryLocationsMoveToTheAccount(t *testing.T) {
	ctx := context.Background()
	databaseURL := dbtest.URL(t, "person_location")

	source, err := iofs.New(os.DirFS("migrations"), ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	migrator, err := migrate.NewWithSourceInstance("iofs", source, pgx5(t, databaseURL))
	if err != nil {
		t.Fatalf("open migrator: %v", err)
	}
	if err := migrator.Migrate(442); err != nil {
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

	people := []struct {
		id, name, visibility, details, location, want string
	}{
		{"00000000-0000-7000-8000-00000000c001", "loc_open", "public", "public", "Arusha", "public"},
		{"00000000-0000-7000-8000-00000000c002", "loc_members", "public", "members", "Moshi", "members"},
		// Public details in a members-only profile were seen by members only.
		{"00000000-0000-7000-8000-00000000c003", "loc_capped", "members", "public", "Tanga", "members"},
		{"00000000-0000-7000-8000-00000000c004", "loc_hidden", "hidden", "public", "Mbeya", "hidden"},
		{"00000000-0000-7000-8000-00000000c005", "loc_none", "public", "public", "  ", ""},
	}
	// The suite's database outlives a run, so the last run's people go first.
	for _, p := range people {
		if _, err := conn.Exec(ctx, `DELETE FROM users WHERE id = $1`, p.id); err != nil {
			t.Fatalf("clearing the last run: %v", err)
		}
		if _, err := conn.Exec(ctx,
			`INSERT INTO users (id, username, username_folded) VALUES ($1, $2, $2)`, p.id, p.name); err != nil {
			t.Fatalf("creating %s: %v", p.name, err)
		}
		if _, err := conn.Exec(ctx,
			`INSERT INTO directory_profiles (user_id, visibility, details_visibility, location)
			 VALUES ($1, $2, $3, $4)`, p.id, p.visibility, p.details, p.location); err != nil {
			t.Fatalf("creating %s's profile: %v", p.name, err)
		}
	}

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, databaseURL, quiet); err != nil {
		t.Fatalf("upgrading: %v", err)
	}

	for _, p := range people {
		var place, visibility, mode string
		err := conn.QueryRow(ctx,
			`SELECT place, visibility, mode FROM user_locations WHERE user_id = $1`, p.id).
			Scan(&place, &visibility, &mode)
		if p.want == "" {
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("%s had no location and was given one (%q, %v)", p.name, place, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("reading %s's location: %v", p.name, err)
		}
		if place != p.location || visibility != p.want || mode != "manual" {
			t.Errorf("%s moved as %q %s %s; want %q %s manual", p.name, place, visibility, mode, p.location, p.want)
		}
	}

	var stillThere bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		                WHERE table_name = 'directory_profiles' AND column_name = 'location')`).
		Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere {
		t.Error("the directory still has a location of its own")
	}
}
