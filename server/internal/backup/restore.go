package backup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnoRebel/tdc/server/internal/storage"
)

// Mode is how a restore treats what is already there.
type Mode string

const (
	/*
		ModeReplace empties the server and puts the archive in its place, in
		one transaction. Only from an archive of everything.
	*/
	ModeReplace Mode = "replace"
	/*
		ModeMerge adds what is missing and overwrites nothing, apart from
		settings, where the archive's value wins.
	*/
	ModeMerge Mode = "merge"
)

// SigningKeySetting is where the server keeps its signing key. A selective
// restore never touches it: the key is the server's identity to its peers.
const SigningKeySetting = "server.signing_key_seed"

/*
NotPartial says, for each part that cannot be restored on its own, why.

Shown to an administrator as the reason, so each is written to be read by one.
*/
var NotPartial = map[Part]string{
	PartAccounts: "accounts are bound up with every grant, membership, session and message; " +
		"putting back some of them beside the ones there now would mix two servers' idea of who somebody is",
	PartProjects: "a project is a room, each of its issues is a room, and they reference one another; " +
		"a subset would leave issues pointing at states, cycles and projects that are not there",
	PartDirectory: "profiles, engagements and agreements reference projects, rooms and each other, " +
		"and agreements cannot be changed once made",
	PartIntegrations: "tokens and webhooks belong to accounts and channels as they were when the backup was taken; " +
		"restored alone they would grant access to whatever now has the same identifiers",
	PartAudit: "the audit log is append-only and numbered in order; " +
		"inserting old entries among newer ones would misnumber both",
}

// RestoreOptions say what to restore and how.
type RestoreOptions struct {
	Mode       Mode
	Passphrase string
	// Parts to restore, for a merge: settings, channels, files.
	Parts []Part
	// Rooms narrows a merge of channels. Empty is every room in the archive.
	Rooms []uuid.UUID
	// DryRun does everything and keeps nothing.
	DryRun   bool
	Progress ProgressFunc
	// BeforeCommit runs in the restore's transaction just before it commits,
	// so a record of the restore is written into what it restored.
	BeforeCommit func(ctx context.Context, tx pgx.Tx) error
}

// TableResult is what a restore did, or would do, to one table.
type TableResult struct {
	Part      Part   `json:"part"`
	Table     string `json:"table"`
	InArchive int64  `json:"inArchive"`
	Added     int64  `json:"added"`
	// Skipped rows already existed, or need something that is not there.
	Skipped int64 `json:"skipped"`
	// Current is how many rows the table holds now, shown beside a replace.
	Current int64 `json:"current"`
}

// SettingChange is one setting a restore changes, or would.
type SettingChange struct {
	Key     string `json:"key"`
	Current string `json:"current"`
	Archive string `json:"archive"`
}

// RestoreResult is what a restore did, or a preview says it would do.
type RestoreResult struct {
	Mode     Mode            `json:"mode"`
	DryRun   bool            `json:"dryRun"`
	Manifest Manifest        `json:"manifest"`
	Tables   []TableResult   `json:"tables"`
	Settings []SettingChange `json:"settings"`
	// FilesWritten are written (or would be); FilesPresent were already in
	// storage and left alone; FilesMissing were missing when the archive was
	// made.
	FilesWritten int64    `json:"filesWritten"`
	FilesPresent int64    `json:"filesPresent"`
	FilesMissing []string `json:"filesMissing,omitempty"`
	// RestartRequired is set after a replace: running processes hold things,
	// such as the signing key, that the restore has changed under them.
	RestartRequired bool `json:"restartRequired"`
}

// batchRows and batchBytes bound one statement.
const (
	batchRows  = 500
	batchBytes = 4 << 20
)

// Inspect opens an archive and reads it to the end, counting what it holds.
func Inspect(ctx context.Context, in io.Reader, passphrase string) (Manifest, Summary, error) {
	reader, err := Open(in, passphrase)
	if err != nil {
		return Manifest{}, Summary{}, err
	}
	var summary Summary
	for {
		if err := ctx.Err(); err != nil {
			return Manifest{}, Summary{}, err
		}
		entry, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return reader.Manifest, summary, nil
		}
		if err != nil {
			return Manifest{}, Summary{}, err
		}
		if entry.Summary != nil {
			summary = *entry.Summary
			continue
		}
		if _, err := io.Copy(io.Discard, entry.Body); err != nil {
			return Manifest{}, Summary{}, fmt.Errorf("%w: %w", ErrNotAnArchive, err)
		}
	}
}

/*
Restore puts an archive back, or with DryRun says what putting it back would do.

A merge, previewed, is performed in a transaction that is then rolled back, so
its counts are what the restore would do rather than an estimate of it. A
replace is previewed by reading the archive through and counting what the
server holds now: performing it would take every table's strongest lock for as
long as it took, on a server people are using.
*/
func Restore(ctx context.Context, pool *pgxpool.Pool, store storage.Store, in io.Reader, opts RestoreOptions) (RestoreResult, error) {
	reader, err := Open(in, opts.Passphrase)
	if err != nil {
		return RestoreResult{}, err
	}

	schemaVersion, err := currentSchemaVersion(ctx, pool)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := checkRestore(reader.Manifest, opts, schemaVersion); err != nil {
		return RestoreResult{}, err
	}

	if opts.Mode == ModeMerge {
		if err := checkPartial(ctx, pool, opts.Parts); err != nil {
			return RestoreResult{}, err
		}
	}

	result := RestoreResult{Mode: opts.Mode, DryRun: opts.DryRun, Manifest: reader.Manifest}
	if opts.Mode == ModeReplace && opts.DryRun {
		return previewReplace(ctx, pool, reader, result)
	}

	report := opts.Progress
	if report == nil {
		report = func(Progress) error { return nil }
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RestoreResult{}, fmt.Errorf("backup: starting the restore: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	catalogue, err := readCatalogue(ctx, tx)
	if err != nil {
		return RestoreResult{}, err
	}

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE restore_keys (tbl TEXT NOT NULL, id TEXT NOT NULL) ON COMMIT DROP;
		CREATE TEMP TABLE restore_deferred (row JSONB NOT NULL) ON COMMIT DROP;
		CREATE TEMP TABLE restore_files (key TEXT PRIMARY KEY, content_type TEXT NOT NULL) ON COMMIT DROP`); err != nil {
		return RestoreResult{}, fmt.Errorf("backup: preparing the restore: %w", err)
	}

	if opts.Mode == ModeReplace {
		if err := emptyEverything(ctx, tx, catalogue); err != nil {
			return RestoreResult{}, err
		}
	}

	run := &restoreRun{
		tx: tx, catalogue: catalogue, opts: opts, store: store, result: &result,
		parents: childParents(),
	}
	progress := Progress{Phase: "tables", TablesTotal: countTables(reader.Manifest.Parts)}

	for {
		if err := ctx.Err(); err != nil {
			return RestoreResult{}, err
		}
		entry, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return RestoreResult{}, err
		}

		switch {
		case entry.Summary != nil:
			result.FilesMissing = entry.Summary.MissingFiles
		case entry.Table != "":
			progress.Table = entry.Table
			if err := report(progress); err != nil {
				return RestoreResult{}, err
			}
			table, err := run.table(ctx, entry)
			if err != nil {
				return RestoreResult{}, err
			}
			if table != nil {
				result.Tables = append(result.Tables, *table)
				progress.Rows += table.Added
			}
			progress.TablesDone++
		default:
			progress.Phase, progress.Table = "files", ""
			if err := run.file(ctx, entry); err != nil {
				return RestoreResult{}, err
			}
			progress.Files = result.FilesWritten + result.FilesPresent
			if progress.Files%25 == 0 {
				if err := report(progress); err != nil {
					return RestoreResult{}, err
				}
			}
		}
	}

	if err := moveSequences(ctx, tx, catalogue, run.restored); err != nil {
		return RestoreResult{}, err
	}

	result.RestartRequired = opts.Mode == ModeReplace
	if opts.DryRun {
		return result, nil
	}

	if opts.BeforeCommit != nil {
		if err := opts.BeforeCommit(ctx, tx); err != nil {
			return RestoreResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RestoreResult{}, fmt.Errorf("backup: committing the restore: %w", err)
	}
	progress.Phase = "done"
	_ = report(progress)
	return result, nil
}

// checkRestore refuses a restore that cannot be done as asked.
func checkRestore(manifest Manifest, opts RestoreOptions, schemaVersion uint) error {
	if manifest.SchemaVersion != schemaVersion {
		return fmt.Errorf("%w: this archive was made at schema version %d by TDC %s, and this server is at "+
			"schema version %d; restore it with the release that made it, then upgrade",
			ErrInvalid, manifest.SchemaVersion, manifest.ServerVersion, schemaVersion)
	}

	switch opts.Mode {
	case ModeReplace:
		if !manifest.Selection().Complete() {
			return fmt.Errorf("%w: replacing the whole server needs an archive of everything, "+
				"and this one holds only some of it; restore parts of it instead", ErrInvalid)
		}
		return nil
	case ModeMerge:
	default:
		return fmt.Errorf("%w: %q is not a way to restore", ErrInvalid, opts.Mode)
	}

	if len(opts.Parts) == 0 {
		return fmt.Errorf("%w: choose what to restore", ErrInvalid)
	}
	for _, part := range opts.Parts {
		if !Known(part) {
			return fmt.Errorf("%w: %q is not a part", ErrInvalid, part)
		}
		if !slices.Contains(manifest.Parts, part) {
			return fmt.Errorf("%w: this archive does not hold %s", ErrInvalid, part)
		}
	}
	if slices.Contains(opts.Parts, PartFiles) && !slices.Contains(opts.Parts, PartChannels) {
		return fmt.Errorf("%w: files are restored with the channels they belong to", ErrInvalid)
	}
	if len(opts.Rooms) > 0 {
		if !slices.Contains(opts.Parts, PartChannels) {
			return fmt.Errorf("%w: channels can only be chosen when restoring channels", ErrInvalid)
		}
		if len(manifest.Rooms) > 0 {
			for _, room := range opts.Rooms {
				if !slices.Contains(manifest.Rooms, room) {
					return fmt.Errorf("%w: this archive does not hold channel %s", ErrInvalid, room)
				}
			}
		}
	}
	return nil
}

/*
checkPartial refuses a merge of a part that cannot be restored alongside what
is there — unless nothing is there. Every reason in NotPartial is about mixing
an archive's rows with a server's own, and a server holding none of that part
yet, such as a new one being restored from its backup, has nothing to mix with.
*/
func checkPartial(ctx context.Context, q querier, parts []Part) error {
	for _, part := range parts {
		reason, refused := NotPartial[part]
		if !refused {
			continue
		}
		for _, spec := range partTables[part] {
			var holds bool
			if err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+quote(spec.name)+")").Scan(&holds); err != nil {
				return fmt.Errorf("backup: checking %s: %w", spec.name, err)
			}
			if holds {
				return fmt.Errorf("%w: %s can only be restored by replacing the whole server, "+
					"or into a server that holds none yet: %s", ErrInvalid, part, reason)
			}
		}
	}
	return nil
}

// previewReplace reads an archive through and sets it beside what is here now.
func previewReplace(ctx context.Context, pool *pgxpool.Pool, reader *Reader, result RestoreResult) (RestoreResult, error) {
	for {
		if err := ctx.Err(); err != nil {
			return RestoreResult{}, err
		}
		entry, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return RestoreResult{}, err
		}
		switch {
		case entry.Summary != nil:
			result.FilesMissing = entry.Summary.MissingFiles
		case entry.Table != "":
			rows, err := countLines(entry.Body)
			if err != nil {
				return RestoreResult{}, err
			}
			current, err := countRows(ctx, pool, entry.Table)
			if err != nil {
				return RestoreResult{}, err
			}
			result.Tables = append(result.Tables, TableResult{
				Part: entry.Part, Table: entry.Table, InArchive: rows, Added: rows, Current: current,
			})
		default:
			if _, err := io.Copy(io.Discard, entry.Body); err != nil {
				return RestoreResult{}, fmt.Errorf("%w: %w", ErrNotAnArchive, err)
			}
			result.FilesWritten++
		}
	}
	result.RestartRequired = true
	return result, nil
}

func countLines(body io.Reader) (int64, error) {
	reader := bufio.NewReaderSize(body, 1<<16)
	var count int64
	for {
		line, err := reader.ReadSlice('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			count++
		}
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return 0, fmt.Errorf("%w: %w", ErrNotAnArchive, err)
		}
	}
}

func countRows(ctx context.Context, q querier, table string) (int64, error) {
	if _, _, known := partOf(table); !known {
		return 0, fmt.Errorf("%w: unexpected table %s", ErrNotAnArchive, table)
	}
	var count int64
	if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM "+quote(table)).Scan(&count); err != nil {
		return 0, fmt.Errorf("backup: counting %s: %w", table, err)
	}
	return count, nil
}

func countTables(parts []Part) int {
	total := 0
	for _, part := range parts {
		total += len(partTables[part])
	}
	return total
}

/*
emptyEverything truncates every table a backup holds or excludes, in one
statement, so references between them are no obstacle. The operational tables
are left alone: a restore that erased its own record, or the list of archives
it could be undone with, could not be accounted for.
*/
func emptyEverything(ctx context.Context, tx pgx.Tx, catalogue map[string]*tableInfo) error {
	var names []string
	for name := range catalogue {
		if _, keep := operational[name]; keep {
			continue
		}
		names = append(names, quote(name))
	}
	slices.Sort(names)
	if _, err := tx.Exec(ctx, "TRUNCATE "+strings.Join(names, ", ")); err != nil {
		return fmt.Errorf("backup: emptying the database: %w", err)
	}
	return nil
}

// childParents maps a parent table to its children's specs, for narrowing a
// merge to chosen rooms.
func childParents() map[string]bool {
	parents := map[string]bool{}
	for _, specs := range partTables {
		for _, spec := range specs {
			if spec.parentTable != "" {
				parents[spec.parentTable] = true
			}
		}
	}
	return parents
}

// restoreRun is one restore's state as it reads through an archive.
type restoreRun struct {
	tx        pgx.Tx
	catalogue map[string]*tableInfo
	opts      RestoreOptions
	store     storage.Store
	result    *RestoreResult
	// parents are tables whose keys are remembered when narrowing to rooms.
	parents map[string]bool
	// restored lists tables written to, for moving their sequences.
	restored []string
	// filesReady is set once the files allowed in are known.
	filesReady bool
}

func (r *restoreRun) wants(part Part) bool {
	return r.opts.Mode == ModeReplace || slices.Contains(r.opts.Parts, part)
}

// table restores one table's entry, or reads past it.
func (r *restoreRun) table(ctx context.Context, entry Entry) (*TableResult, error) {
	part, spec, known := partOf(entry.Table)
	// Only tables a part names, under the part that names them. An archive
	// is a file somebody uploaded; it does not get to name the operational
	// tables, or put rows into one part under another's name.
	if !known || part != entry.Part {
		return nil, fmt.Errorf("%w: unexpected table %s in %s", ErrNotAnArchive, entry.Table, entry.Part)
	}
	info, exists := r.catalogue[entry.Table]
	if !exists {
		return nil, fmt.Errorf("%w: this server has no table %s", ErrInvalid, entry.Table)
	}

	if !r.wants(part) {
		_, err := io.Copy(io.Discard, entry.Body)
		return nil, err
	}

	result := &TableResult{Part: part, Table: entry.Table}
	narrow := r.opts.Mode == ModeMerge && len(r.opts.Rooms) > 0 && part == PartChannels

	var err error
	if r.opts.Mode == ModeMerge && entry.Table == "server_config" {
		err = r.settings(ctx, info, entry.Body, result)
	} else {
		err = r.rows(ctx, info, spec, entry.Body, narrow, result)
	}
	if err != nil {
		return nil, err
	}
	r.restored = append(r.restored, entry.Table)
	return result, nil
}

// rows inserts a table's rows in batches, then fills its self-references.
func (r *restoreRun) rows(ctx context.Context, info *tableInfo, spec tableSpec, body io.Reader, narrow bool, result *TableResult) error {
	statement, matches, remember, defer_ := r.statements(info, spec, narrow)
	args := func(batch []byte) []any {
		if narrow {
			return []any{string(batch), r.opts.Rooms}
		}
		return []any{string(batch)}
	}

	err := batches(body, func(batch []byte, count int64) error {
		if narrow {
			var matched int64
			if err := r.tx.QueryRow(ctx, matches, args(batch)...).Scan(&matched); err != nil {
				return fmt.Errorf("backup: restoring %s: %w", info.name, err)
			}
			result.InArchive += matched
		} else {
			result.InArchive += count
		}

		tag, err := r.tx.Exec(ctx, statement, args(batch)...)
		if err != nil {
			return fmt.Errorf("backup: restoring %s: %w", info.name, err)
		}
		result.Added += tag.RowsAffected()

		if remember != "" {
			if _, err := r.tx.Exec(ctx, remember, args(batch)...); err != nil {
				return fmt.Errorf("backup: restoring %s: %w", info.name, err)
			}
		}
		if defer_ != "" {
			if _, err := r.tx.Exec(ctx, defer_, args(batch)...); err != nil {
				return fmt.Errorf("backup: restoring %s: %w", info.name, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	result.Skipped = result.InArchive - result.Added

	return r.selfReferences(ctx, info)
}

/*
statements builds what restoring one table takes.

	insert:   the rows, with each reference checked against what is there
	matches:  how many rows a channel selection lets through
	remember: the keys of rows a channel selection let through, for children
	deferred: rows with a reference to their own table, for the second pass
*/
func (r *restoreRun) statements(info *tableInfo, spec tableSpec, narrow bool) (insert, matches, remember, deferred string) {
	self := map[string]bool{}
	for _, ref := range info.selfReferences() {
		self[ref.column] = true
	}
	optional := map[string]reference{}
	var conditions []string
	for _, ref := range info.references {
		if self[ref.column] {
			continue
		}
		exists := "EXISTS (SELECT 1 FROM " + quote(ref.table) + " x WHERE x." + quote(ref.refColumn) +
			" = r." + quote(ref.column) + ")"
		if ref.required {
			conditions = append(conditions, "(r."+quote(ref.column)+" IS NULL OR "+exists+")")
		} else {
			optional[ref.column] = ref
		}
	}

	var selection string
	if narrow {
		selection = restoreFilter(spec)
		conditions = append(conditions, selection)
	}

	names := make([]string, len(info.columns))
	values := make([]string, len(info.columns))
	for i, col := range info.columns {
		name := quote(col.name)
		names[i] = name
		switch ref, isOptional := optional[col.name]; {
		case self[col.name]:
			values[i] = "NULL"
		case isOptional:
			values[i] = "CASE WHEN EXISTS (SELECT 1 FROM " + quote(ref.table) + " x WHERE x." +
				quote(ref.refColumn) + " = r." + name + ") THEN r." + name + " END"
		default:
			values[i] = "r." + name
		}
	}

	source := " FROM json_populate_recordset(NULL::" + quote(info.name) + ", $1::json) r"
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	insert = "INSERT INTO " + quote(info.name) + " (" + strings.Join(names, ", ") + ") SELECT " +
		strings.Join(values, ", ") + source + where + " ON CONFLICT DO NOTHING"

	if narrow {
		matches = "SELECT COUNT(*)" + source + " WHERE " + selection
		if r.parents[info.name] {
			remember = "INSERT INTO restore_keys SELECT '" + info.name + "', r.id::text" + source +
				" WHERE " + selection
		}
	}

	if len(self) > 0 {
		var nonNull []string
		for column := range self {
			nonNull = append(nonNull, "r."+quote(column)+" IS NOT NULL")
		}
		slices.Sort(nonNull)
		deferred = "INSERT INTO restore_deferred SELECT to_jsonb(r)" + source +
			" WHERE (" + strings.Join(nonNull, " OR ") + ")"
		if narrow {
			deferred += " AND " + selection
		}
	}
	return insert, matches, remember, deferred
}

// restoreFilter narrows rows being restored to the chosen rooms, as $2.
func restoreFilter(spec tableSpec) string {
	switch {
	case spec.roomColumn != "":
		return "r." + quote(spec.roomColumn) + " = ANY($2::uuid[])"
	case spec.parentColumn != "":
		// $2 is named although the parent's keys already carry the choice,
		// so every narrowed statement takes the same arguments.
		return "($2::uuid[] IS NOT NULL AND r." + quote(spec.parentColumn) +
			"::text IN (SELECT id FROM restore_keys WHERE tbl = '" + spec.parentTable + "'))"
	default:
		return "$2::uuid[] IS NOT NULL"
	}
}

/*
selfReferences fills in references a table makes to itself.

Inserted empty in the first pass, because the row referred to may come later
in the archive. Filled only where empty and only where the target now exists,
so a merge never changes a row that was already there.
*/
func (r *restoreRun) selfReferences(ctx context.Context, info *tableInfo) error {
	refs := info.selfReferences()
	if len(refs) == 0 {
		return nil
	}
	if len(info.primaryKey) == 0 {
		return fmt.Errorf("backup: %s refers to itself and has no key", info.name)
	}

	var keys []string
	for _, key := range info.primaryKey {
		keys = append(keys, "t."+quote(key)+" = d."+quote(key))
	}
	for _, ref := range refs {
		column := quote(ref.column)
		statement := "UPDATE " + quote(info.name) + " t SET " + column + " = d." + column +
			" FROM (SELECT (jsonb_populate_record(NULL::" + quote(info.name) + ", row)).* FROM restore_deferred) d" +
			" WHERE " + strings.Join(keys, " AND ") +
			" AND t." + column + " IS NULL AND d." + column + " IS NOT NULL" +
			" AND EXISTS (SELECT 1 FROM " + quote(info.name) + " x WHERE x." + quote(ref.refColumn) + " = d." + column + ")"
		if _, err := r.tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("backup: restoring %s: %w", info.name, err)
		}
	}
	if _, err := r.tx.Exec(ctx, "TRUNCATE restore_deferred"); err != nil {
		return fmt.Errorf("backup: restoring %s: %w", info.name, err)
	}
	return nil
}

/*
settings restores stored settings, the archive's value winning.

The signing key is never written by a merge: it is the server's identity to its
peers, and changing it under a running process would sign events with one key
while the database said another.
*/
func (r *restoreRun) settings(ctx context.Context, info *tableInfo, body io.Reader, result *TableResult) error {
	var names, updates []string
	for _, col := range info.columns {
		names = append(names, quote(col.name))
		if col.name != "key" {
			updates = append(updates, quote(col.name)+" = EXCLUDED."+quote(col.name))
		}
	}
	// The signing key only where there is none: a new server being restored
	// from its backup takes its identity back, and a running one keeps its own.
	keep := "(r.key <> $2 OR NOT EXISTS (SELECT 1 FROM server_config k WHERE k.key = $2))"
	source := " FROM json_populate_recordset(NULL::server_config, $1::json) r WHERE " + keep

	return batches(body, func(batch []byte, count int64) error {
		result.InArchive += count

		rows, err := r.tx.Query(ctx, `SELECT r.key, r.value, r.is_secret, COALESCE(c.value, ''), c.key IS NOT NULL`+
			` FROM json_populate_recordset(NULL::server_config, $1::json) r`+
			` LEFT JOIN server_config c ON c.key = r.key WHERE `+keep+` ORDER BY r.key`,
			string(batch), SigningKeySetting)
		if err != nil {
			return fmt.Errorf("backup: comparing settings: %w", err)
		}
		for rows.Next() {
			var key, value, current string
			var secret, present bool
			if err := rows.Scan(&key, &value, &secret, &current, &present); err != nil {
				rows.Close()
				return fmt.Errorf("backup: comparing settings: %w", err)
			}
			if present && value == current {
				result.Skipped++
				continue
			}
			result.Added++
			if secret {
				// A credential stays out of what an administrator is shown.
				value, current = "••••••••", "••••••••"
			}
			r.result.Settings = append(r.result.Settings, SettingChange{Key: key, Current: current, Archive: value})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("backup: comparing settings: %w", err)
		}

		if _, err := r.tx.Exec(ctx, "INSERT INTO server_config ("+strings.Join(names, ", ")+") SELECT r.*"+
			source+" ON CONFLICT (key) DO UPDATE SET "+strings.Join(updates, ", "),
			string(batch), SigningKeySetting); err != nil {
			return fmt.Errorf("backup: restoring settings: %w", err)
		}
		return nil
	})
}

/*
file writes one stored file back.

Only files the restored attachments name are accepted — an archive is
something somebody uploaded, and it does not get to write whatever keys it
likes into storage. A merge writes only what is missing; a replace writes
everything, which over an unchanged store rewrites the same bytes.
*/
func (r *restoreRun) file(ctx context.Context, entry Entry) error {
	if !r.wants(PartFiles) {
		_, err := io.Copy(io.Discard, entry.Body)
		return err
	}
	if !r.filesReady {
		if err := r.allowFiles(ctx); err != nil {
			return err
		}
		r.filesReady = true
	}

	var contentType string
	err := r.tx.QueryRow(ctx, "SELECT content_type FROM restore_files WHERE key = $1", entry.Key).Scan(&contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err := io.Copy(io.Discard, entry.Body)
		return err
	}
	if err != nil {
		return fmt.Errorf("backup: restoring file %s: %w", entry.Key, err)
	}

	if r.opts.Mode == ModeMerge {
		if _, err := r.store.Stat(ctx, entry.Key); err == nil {
			r.result.FilesPresent++
			_, err := io.Copy(io.Discard, entry.Body)
			return err
		} else if !errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("backup: checking file %s: %w", entry.Key, err)
		}
	}

	r.result.FilesWritten++
	if r.opts.DryRun {
		_, err := io.Copy(io.Discard, entry.Body)
		return err
	}
	if err := r.store.Put(ctx, entry.Key, entry.Body, entry.Size, contentType); err != nil {
		return fmt.Errorf("backup: writing file %s: %w", entry.Key, err)
	}
	return nil
}

// allowFiles records which storage keys the restored attachments name.
func (r *restoreRun) allowFiles(ctx context.Context) error {
	// Null, rather than empty, is every room.
	var rooms []uuid.UUID
	if r.opts.Mode == ModeMerge && len(r.opts.Rooms) > 0 {
		rooms = r.opts.Rooms
	}
	// Copies of other servers' files were not archived, and are copied again
	// on first view rather than reported missing.
	_, err := r.tx.Exec(ctx, `INSERT INTO restore_files (key, content_type)
		SELECT storage_key, content_type FROM attachments
		WHERE ($1::uuid[] IS NULL OR room_id = ANY($1::uuid[])) AND origin_server IS NULL
		UNION ALL
		SELECT preview_key, COALESCE(NULLIF(preview_type, ''), 'application/octet-stream') FROM attachments
		WHERE preview_key IS NOT NULL AND preview_key <> '' AND origin_server IS NULL
		  AND ($1::uuid[] IS NULL OR room_id = ANY($1::uuid[]))
		ON CONFLICT (key) DO NOTHING`, rooms)
	if err != nil {
		return fmt.Errorf("backup: listing files to restore: %w", err)
	}
	return nil
}

// moveSequences sets every restored table's sequences past what it holds, so
// the next row written does not collide with a restored one.
func moveSequences(ctx context.Context, tx pgx.Tx, catalogue map[string]*tableInfo, tables []string) error {
	for _, table := range tables {
		for _, col := range catalogue[table].serials {
			if _, err := tx.Exec(ctx, "SELECT setval(pg_get_serial_sequence($1, $2), "+
				"COALESCE((SELECT MAX("+quote(col)+") FROM "+quote(table)+"), 0) + 1, false)",
				quote(table), col); err != nil {
				return fmt.Errorf("backup: moving %s's sequence: %w", table, err)
			}
		}
	}
	return nil
}

// batches reads newline-delimited rows and hands them on as JSON arrays.
func batches(body io.Reader, handle func(batch []byte, count int64) error) error {
	reader := bufio.NewReaderSize(body, 1<<16)
	var batch bytes.Buffer
	var count int64

	flush := func() error {
		if count == 0 {
			return nil
		}
		batch.WriteByte(']')
		err := handle(batch.Bytes(), count)
		batch.Reset()
		count = 0
		return err
	}

	for {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			if count == 0 {
				batch.WriteByte('[')
			} else {
				batch.WriteByte(',')
			}
			batch.Write(line)
			count++
			if count >= batchRows || batch.Len() >= batchBytes {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return flush()
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrNotAnArchive, err)
		}
	}
}
