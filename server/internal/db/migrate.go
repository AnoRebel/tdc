package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	// Registers the pgx driver with golang-migrate for its side effect.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// migrations are embedded so that a released binary carries its own schema and
// an operator never has to place files alongside it.
//
//go:embed migrations/*.sql
var migrations embed.FS

// ErrSchemaNewerThanBinary reports a database migrated by a newer release than
// the running binary.
//
// Continuing would mean operating on a schema this build does not understand,
// so the server refuses to start instead. This is what makes a rollback safe:
// the old binary declines rather than corrupting data.
var ErrSchemaNewerThanBinary = errors.New("database schema is newer than this binary")

// Migrate brings the database up to the schema this binary expects.
//
// Migrations run in order and each runs in its own transaction where Postgres
// allows it, so a failure leaves the schema at its previous state rather than
// half-applied. Upgrading across several releases at once is supported: every
// pending migration is applied in sequence.
func Migrate(ctx context.Context, databaseURL string, log *slog.Logger) error {
	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	defer func() { _ = source.Close() }()

	expected, err := latestMigrationVersion()
	if err != nil {
		return err
	}

	migrator, err := migrate.NewWithSourceInstance("iofs", source, migrateURL(databaseURL))
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	defer func() {
		sourceErr, dbErr := migrator.Close()
		if sourceErr != nil {
			log.Warn("closing migration source", slog.String("error", sourceErr.Error()))
		}
		if dbErr != nil {
			log.Warn("closing migration database", slog.String("error", dbErr.Error()))
		}
	}()

	current, dirty, err := migrator.Version()
	switch {
	case errors.Is(err, migrate.ErrNilVersion):
		current = 0
	case err != nil:
		return fmt.Errorf("read schema version: %w", err)
	}

	if dirty {
		// A previous run failed partway. Continuing could apply a migration on
		// top of a half-applied one, so require an operator to look.
		return fmt.Errorf(
			"database is marked dirty at version %d: a previous migration failed and needs manual inspection",
			current,
		)
	}

	if current > expected {
		return fmt.Errorf("%w: database is at version %d, this binary expects %d",
			ErrSchemaNewerThanBinary, current, expected)
	}

	if current == expected {
		log.Info("database schema up to date", slog.Uint64("version", uint64(current)))
		return nil
	}

	log.Info("applying migrations",
		slog.Uint64("from", uint64(current)),
		slog.Uint64("to", uint64(expected)),
	)

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	applied, _, err := migrator.Version()
	if err != nil {
		return fmt.Errorf("confirm schema version: %w", err)
	}

	log.Info("migrations applied", slog.Uint64("version", uint64(applied)))
	return nil
}

// migrateURL adapts a normal Postgres URL to the scheme golang-migrate's pgx
// driver registers itself under ("pgx5").
//
// Operators write an ordinary postgres:// URL — the same one every other tool
// accepts — and the translation happens here rather than leaking an unusual
// scheme into configuration and documentation.
func migrateURL(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		// Leave it alone: migrate will report the problem with more context
		// than we can add here.
		return databaseURL
	}
	switch parsed.Scheme {
	case "postgres", "postgresql":
		parsed.Scheme = "pgx5"
		return parsed.String()
	default:
		return databaseURL
	}
}

// LatestSchemaVersion is the schema version this binary expects, derived from
// the migrations it carries. It is exported for tests that need to assert or
// restore the expected version.
func LatestSchemaVersion() uint {
	version, err := latestMigrationVersion()
	if err != nil {
		return 0
	}
	return version
}

// latestMigrationVersion is the highest version embedded in this binary.
func latestMigrationVersion() (uint, error) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return 0, fmt.Errorf("list migrations: %w", err)
	}

	var highest uint
	for _, entry := range entries {
		var version uint
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &version); err != nil {
			continue
		}
		if version > highest {
			highest = version
		}
	}

	if highest == 0 {
		return 0, errors.New("no migrations found in embedded filesystem")
	}
	return highest, nil
}
