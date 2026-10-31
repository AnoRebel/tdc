package backup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnoRebel/tdc/server/internal/storage"
)

// Progress is how far a backup or restore has got.
type Progress struct {
	Phase       string `json:"phase"`
	Table       string `json:"table,omitempty"`
	TablesDone  int    `json:"tablesDone"`
	TablesTotal int    `json:"tablesTotal"`
	Rows        int64  `json:"rows"`
	Files       int64  `json:"files"`
	Bytes       int64  `json:"bytes"`
}

// ProgressFunc hears about progress. Returning an error stops the work; that
// is how cancelling reaches it.
type ProgressFunc func(Progress) error

// ExportOptions are what a backup is taken with.
type ExportOptions struct {
	Selection  Selection
	Passphrase string
	// ServerName and ServerVersion are written into the manifest.
	ServerName    string
	ServerVersion string
	// MaxBytes stops a backup whose archive grows past it. Zero is no limit.
	MaxBytes int64
	// WorkDirectory is where tables are spooled before going into the
	// archive. Empty is the system's temporary directory.
	WorkDirectory string
	Progress      ProgressFunc
}

/*
Export writes an archive of what the selection names to out.

Every table is read inside one REPEATABLE READ, READ ONLY transaction, so the
archive is the database at one instant: a message and its reactions, a role and
whoever holds it. Each table is spooled to a temporary file — a tar entry needs
its size before its bytes — and nothing is held in memory but one row at a
time.
*/
func Export(ctx context.Context, pool *pgxpool.Pool, store storage.Store, opts ExportOptions, out io.Writer) (manifest Manifest, summary Summary, err error) {
	if err := CheckPassphrase(opts.Passphrase); err != nil {
		return Manifest{}, Summary{}, err
	}
	selection, err := opts.Selection.Resolve()
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	if out == nil {
		return Manifest{}, Summary{}, fmt.Errorf("%w: nowhere to write", ErrInvalid)
	}
	report := opts.Progress
	if report == nil {
		report = func(Progress) error { return nil }
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Manifest{}, Summary{}, fmt.Errorf("backup: starting the snapshot: %w", err)
	}
	// Read only, so there is nothing to keep: ending it is all a rollback does.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	schemaVersion, err := currentSchemaVersion(ctx, tx)
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	catalogue, err := readCatalogue(ctx, tx)
	if err != nil {
		return Manifest{}, Summary{}, err
	}

	// Which tables, in which order, each with its part.
	tablePart := map[string]Part{}
	specs := map[string]tableSpec{}
	var names []string
	for _, part := range selection.Parts {
		for _, spec := range partTables[part] {
			tablePart[spec.name] = part
			specs[spec.name] = spec
			names = append(names, spec.name)
		}
	}
	order, err := ordered(names, catalogue)
	if err != nil {
		return Manifest{}, Summary{}, err
	}

	manifest = Manifest{
		Format:        FormatName,
		FormatVersion: FormatVersion,
		SchemaVersion: schemaVersion,
		ServerVersion: opts.ServerVersion,
		ServerName:    opts.ServerName,
		CreatedAt:     time.Now().UTC(),
		Parts:         selection.Parts,
		Rooms:         selection.Rooms,
	}

	counted := &countingWriter{out: out, limit: opts.MaxBytes}
	archive, err := newArchiveWriter(counted, opts.Passphrase, manifest.CreatedAt)
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	if err := archive.addJSON(manifestName, manifest); err != nil {
		return Manifest{}, Summary{}, limitError(counted, err)
	}

	progress := Progress{Phase: "tables", TablesTotal: len(order)}
	for _, table := range order {
		progress.Table = table
		if err := report(progress); err != nil {
			return Manifest{}, Summary{}, err
		}

		rows, err := exportTable(ctx, tx, archive, tablePart[table], specs[table], catalogue[table], selection.Rooms, opts.WorkDirectory)
		if err != nil {
			return Manifest{}, Summary{}, limitError(counted, err)
		}
		summary.Tables = append(summary.Tables, TableCount{Part: tablePart[table], Table: table, Rows: rows})
		progress.TablesDone++
		progress.Rows += rows
		progress.Bytes = counted.written
	}

	if selection.Has(PartFiles) {
		progress.Phase, progress.Table = "files", ""
		if err := exportFiles(ctx, tx, store, archive, selection.Rooms, &summary, &progress, counted, report); err != nil {
			return Manifest{}, Summary{}, limitError(counted, err)
		}
	}

	if err := archive.addJSON(summaryName, summary); err != nil {
		return Manifest{}, Summary{}, limitError(counted, err)
	}
	if err := archive.Close(); err != nil {
		return Manifest{}, Summary{}, limitError(counted, err)
	}

	progress.Phase, progress.Bytes = "done", counted.written
	_ = report(progress)
	return manifest, summary, nil
}

// exportTable spools one table's rows and adds them to the archive.
func exportTable(
	ctx context.Context, tx pgx.Tx, archive *archiveWriter,
	part Part, spec tableSpec, info *tableInfo, rooms []uuid.UUID, workDirectory string,
) (int64, error) {
	spool, err := os.CreateTemp(workDirectory, "tdc-backup-table-*.ndjson")
	if err != nil {
		return 0, fmt.Errorf("backup: spooling %s: %w", spec.name, err)
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}()

	query := "SELECT row_to_json(t)::text FROM " + quote(spec.name) + " t"
	var args []any
	if len(rooms) > 0 {
		if filter := roomFilter(spec, "t"); filter != "" {
			query += " WHERE " + filter
			args = append(args, rooms)
		}
	}
	// Ordered by key, so two archives of the same data are the same bytes
	// apart from when they were made.
	if len(info.primaryKey) > 0 {
		keys := make([]string, len(info.primaryKey))
		for i, key := range info.primaryKey {
			keys[i] = "t." + quote(key)
		}
		query += " ORDER BY " + strings.Join(keys, ", ")
	}

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("backup: reading %s: %w", spec.name, err)
	}
	defer rows.Close()

	buffered := bufio.NewWriterSize(spool, 1<<16)
	var count int64
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return 0, fmt.Errorf("backup: reading %s: %w", spec.name, err)
		}
		if _, err := buffered.WriteString(line); err != nil {
			return 0, fmt.Errorf("backup: spooling %s: %w", spec.name, err)
		}
		if err := buffered.WriteByte('\n'); err != nil {
			return 0, fmt.Errorf("backup: spooling %s: %w", spec.name, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("backup: reading %s: %w", spec.name, err)
	}
	if err := buffered.Flush(); err != nil {
		return 0, fmt.Errorf("backup: spooling %s: %w", spec.name, err)
	}

	size, err := spool.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("backup: spooling %s: %w", spec.name, err)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("backup: spooling %s: %w", spec.name, err)
	}
	if err := archive.add(tableEntryName(part, spec.name), size, spool); err != nil {
		return 0, err
	}
	return count, nil
}

/*
roomFilter narrows a table to the chosen rooms, as a condition on alias, with
the rooms as $1. Empty for a table a selection does not narrow.
*/
func roomFilter(spec tableSpec, alias string) string {
	switch {
	case spec.roomColumn != "":
		return alias + "." + quote(spec.roomColumn) + " = ANY($1::uuid[])"
	case spec.parentColumn != "":
		_, parent, _ := partOf(spec.parentTable)
		return alias + "." + quote(spec.parentColumn) + " IN (SELECT p.id FROM " +
			quote(spec.parentTable) + " p WHERE p." + quote(parent.roomColumn) + " = ANY($1::uuid[]))"
	default:
		return ""
	}
}

// exportFiles adds the bytes of every attachment and preview the snapshot names.
func exportFiles(
	ctx context.Context, tx pgx.Tx, store storage.Store, archive *archiveWriter,
	rooms []uuid.UUID, summary *Summary, progress *Progress, counted *countingWriter, report ProgressFunc,
) error {
	keys, err := attachmentKeys(ctx, tx, rooms)
	if err != nil {
		return err
	}

	for _, key := range keys {
		body, object, err := store.Get(ctx, key)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				summary.MissingFiles = append(summary.MissingFiles, key)
				continue
			}
			return fmt.Errorf("backup: reading file %s: %w", key, err)
		}
		err = archive.add(filesPrefix+key, object.Size, body)
		_ = body.Close()
		if err != nil {
			return err
		}

		summary.Files++
		summary.FileBytes += object.Size
		progress.Files++
		progress.Bytes = counted.written
		if progress.Files%25 == 0 {
			if err := report(*progress); err != nil {
				return err
			}
		}
	}
	return nil
}

// attachmentKeys lists the storage keys the chosen rooms' attachments use.
func attachmentKeys(ctx context.Context, q querier, rooms []uuid.UUID) ([]string, error) {
	// Copies of other servers' files are left out: they can be copied again
	// from where they came, the first time somebody opens one after a restore.
	query := `SELECT k FROM (
		SELECT storage_key AS k, room_id FROM attachments WHERE origin_server IS NULL
		UNION ALL
		SELECT preview_key, room_id FROM attachments
		WHERE preview_key IS NOT NULL AND preview_key <> '' AND origin_server IS NULL
	) keys`
	var args []any
	if len(rooms) > 0 {
		query += " WHERE room_id = ANY($1::uuid[])"
		args = append(args, rooms)
	}
	query += " GROUP BY k ORDER BY k"

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("backup: listing files: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("backup: listing files: %w", err)
	}
	return keys, nil
}

// currentSchemaVersion is the newest migration applied to this database.
func currentSchemaVersion(ctx context.Context, q querier) (uint, error) {
	var version int64
	var dirty bool
	if err := q.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		return 0, fmt.Errorf("backup: reading the schema version: %w", err)
	}
	if dirty {
		return 0, fmt.Errorf("backup: the schema is mid-migration (version %d); finish the upgrade first", version)
	}
	return uint(version), nil
}

// countingWriter counts what passes and refuses past a limit.
type countingWriter struct {
	out     io.Writer
	limit   int64
	written int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if w.limit > 0 && w.written+int64(len(p)) > w.limit {
		return 0, ErrTooLarge
	}
	n, err := w.out.Write(p)
	w.written += int64(n)
	return n, err
}

// limitError reports a limit as the limit rather than as whichever layer of
// the archive happened to be writing when it was reached.
func limitError(counted *countingWriter, err error) error {
	if errors.Is(err, ErrTooLarge) || (counted.limit > 0 && counted.written >= counted.limit) {
		return fmt.Errorf("%w (%d bytes)", ErrTooLarge, counted.limit)
	}
	return err
}
