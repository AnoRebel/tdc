package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tablesOf lists the tables a set of parts holds.
func tablesOf(parts []Part) []string {
	var names []string
	for _, part := range parts {
		for _, spec := range partTables[part] {
			names = append(names, spec.name)
		}
	}
	return names
}

func assertSameTables(t *testing.T, source, target *pgxpool.Pool, tables []string) {
	t.Helper()
	for _, table := range tables {
		if want, got := fingerprint(t, source, table), fingerprint(t, target, table); want != got {
			t.Errorf("%s differs after the round trip", table)
		}
	}
}

/*
Every part, alone, into an empty database: the archive holds what the part and
its dependencies need, and putting it back gives the same rows, the same files
and sequences that carry on from where they were.
*/
func TestEachPartRoundTripsIntoAnEmptyDatabase(t *testing.T) {
	source := connect(t, "backup_source")
	target := connect(t, "backup_target")
	ctx := context.Background()

	empty(t, source)
	sourceStore := localStore(t)
	f := seed(t, source, sourceStore)

	for _, part := range Parts {
		t.Run(string(part), func(t *testing.T) {
			empty(t, target)
			targetStore := localStore(t)

			archive, manifest, summary := export(t, source, sourceStore, Selection{Parts: []Part{part}})
			if !slices.Contains(manifest.Parts, part) {
				t.Fatalf("manifest parts %v do not include %s", manifest.Parts, part)
			}
			for _, needed := range dependencies[part] {
				if !slices.Contains(manifest.Parts, needed) {
					t.Errorf("%s was backed up without %s, which it needs", part, needed)
				}
			}
			if part == PartFiles && summary.Files != 1 {
				t.Errorf("files part wrote %d files, want 1", summary.Files)
			}

			result, err := Restore(ctx, target, targetStore, bytes.NewReader(archive), RestoreOptions{
				Mode: ModeMerge, Passphrase: passphrase, Parts: manifest.Parts,
			})
			if err != nil {
				t.Fatalf("restore: %v", err)
			}
			for _, table := range result.Tables {
				if table.Skipped != 0 {
					t.Errorf("%s: %d rows skipped restoring into an empty database", table.Table, table.Skipped)
				}
			}

			assertSameTables(t, source, target, tablesOf(manifest.Parts))

			if slices.Contains(manifest.Parts, PartFiles) {
				body, _, err := targetStore.Get(ctx, f.attachmentKey)
				if err != nil {
					t.Fatalf("restored file: %v", err)
				}
				got, _ := io.ReadAll(body)
				_ = body.Close()
				if !bytes.Equal(got, f.attachmentBytes) {
					t.Errorf("restored file holds %q", got)
				}
			}

			if part == PartAudit {
				// The sequence carries on past the restored entries rather than
				// colliding with the first of them.
				if _, err := target.Exec(ctx, `INSERT INTO audit_entries (actor_label, action) VALUES ('test', 'after.restore')`); err != nil {
					t.Errorf("writing after a restore: %v", err)
				}
			}
		})
	}
}

// The whole server replaced, then compared table by table.
func TestReplaceRestoresEverything(t *testing.T) {
	source := connect(t, "backup_source")
	target := connect(t, "backup_target")
	ctx := context.Background()

	empty(t, source)
	sourceStore := localStore(t)
	seed(t, source, sourceStore)

	// Something already in the target, which a replace must remove.
	empty(t, target)
	if _, err := target.Exec(ctx, `INSERT INTO users (id, username, username_folded) VALUES ($1, 'stranger', 'stranger')`,
		uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at)
		SELECT gen_random_uuid(), id, '\x01', NOW() + interval '1 day' FROM users`); err != nil {
		t.Fatal(err)
	}

	archive, _, _ := export(t, source, sourceStore, Selection{Parts: Parts})

	preview, err := Restore(ctx, target, localStore(t), bytes.NewReader(archive), RestoreOptions{
		Mode: ModeReplace, Passphrase: passphrase, DryRun: true,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if fingerprint(t, target, "users") == fingerprint(t, source, "users") {
		t.Fatal("a preview changed the database")
	}
	for _, table := range preview.Tables {
		if table.Table == "users" && (table.InArchive != 3 || table.Current != 1) {
			t.Errorf("preview of users: %+v", table)
		}
	}

	targetStore := localStore(t)
	result, err := Restore(ctx, target, targetStore, bytes.NewReader(archive), RestoreOptions{
		Mode: ModeReplace, Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if !result.RestartRequired {
		t.Error("a replace should require a restart")
	}
	assertSameTables(t, source, target, tablesOf(Parts))

	var sessions int
	_ = target.QueryRow(ctx, "SELECT COUNT(*) FROM sessions").Scan(&sessions)
	if sessions != 0 {
		t.Errorf("%d sessions survived a replace", sessions)
	}
}

// A replace that fails part way leaves every table as it was.
func TestFailedReplaceChangesNothing(t *testing.T) {
	source := connect(t, "backup_source")
	target := connect(t, "backup_target")
	ctx := context.Background()

	empty(t, source)
	sourceStore := localStore(t)
	seed(t, source, sourceStore)
	archive, _, _ := export(t, source, sourceStore, Selection{Parts: Parts})

	empty(t, target)
	if _, err := target.Exec(ctx, `INSERT INTO users (id, username, username_folded) VALUES ($1, 'stranger', 'stranger')`,
		uuid.New()); err != nil {
		t.Fatal(err)
	}
	before := fingerprint(t, target, "users")

	stop := errors.New("stopped part way")
	_, err := Restore(ctx, target, localStore(t), bytes.NewReader(archive), RestoreOptions{
		Mode: ModeReplace, Passphrase: passphrase,
		Progress: func(p Progress) error {
			if p.Table == "events" {
				return stop
			}
			return nil
		},
	})
	if !errors.Is(err, stop) {
		t.Fatalf("restore error = %v, want the stop", err)
	}
	if after := fingerprint(t, target, "users"); after != before {
		t.Error("a failed replace changed the users table")
	}
}

/*
A channel restored selectively: a deleted message comes back, a message edited
since is left as it is now, nothing outside the chosen channel is touched, and
the preview's counts are the restore's.
*/
func TestSelectiveChannelRestore(t *testing.T) {
	pool := connect(t, "backup_source")
	ctx := context.Background()

	empty(t, pool)
	store := localStore(t)
	f := seed(t, pool, store)
	archive, _, _ := export(t, pool, store, Selection{Parts: Parts})

	// After the backup: the reply is deleted, the root edited, the file lost,
	// and the other channel's message deleted too.
	if _, err := pool.Exec(ctx, `DELETE FROM events WHERE id = $1`, f.reply); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE events SET content = '{"body":"edited"}' WHERE id = $1`, f.rootEvent); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM events WHERE room_id = $1`, f.random); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, f.attachmentKey); err != nil {
		t.Fatal(err)
	}

	options := RestoreOptions{
		Mode: ModeMerge, Passphrase: passphrase, Parts: []Part{PartChannels, PartFiles},
		Rooms: []uuid.UUID{f.general}, DryRun: true,
	}
	preview, err := Restore(ctx, pool, store, bytes.NewReader(archive), options)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var exists bool
	_ = pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM events WHERE id = $1)", f.reply).Scan(&exists)
	if exists {
		t.Fatal("a preview restored the message")
	}

	options.DryRun = false
	result, err := Restore(ctx, pool, store, bytes.NewReader(archive), options)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	added := func(r RestoreResult, table string) int64 {
		for _, entry := range r.Tables {
			if entry.Table == table {
				return entry.Added
			}
		}
		return -1
	}
	if added(preview, "events") != 1 || added(result, "events") != 1 {
		t.Errorf("events added: preview %d, restore %d, want 1 and 1",
			added(preview, "events"), added(result, "events"))
	}
	if preview.FilesWritten != 1 || result.FilesWritten != 1 {
		t.Errorf("files written: preview %d, restore %d", preview.FilesWritten, result.FilesWritten)
	}

	var body, root string
	if err := pool.QueryRow(ctx, `SELECT content->>'body', COALESCE(thread_root_id, '') FROM events WHERE id = $1`,
		f.reply).Scan(&body, &root); err != nil {
		t.Fatalf("the deleted message did not come back: %v", err)
	}
	if body != f.deletedEventMarker || root != f.rootEvent {
		t.Errorf("restored reply: body %q root %q", body, root)
	}
	_ = pool.QueryRow(ctx, `SELECT content->>'body' FROM events WHERE id = $1`, f.rootEvent).Scan(&body)
	if body != "edited" {
		t.Errorf("an existing message was overwritten: %q", body)
	}
	var elsewhere int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE room_id = $1`, f.random).Scan(&elsewhere)
	if elsewhere != 0 {
		t.Error("a channel that was not chosen was restored")
	}
	if _, err := store.Stat(ctx, f.attachmentKey); err != nil {
		t.Errorf("the file did not come back: %v", err)
	}
}

// Accounts are refused partially once the server holds any.
func TestAccountsAreNotRestoredPartially(t *testing.T) {
	pool := connect(t, "backup_source")
	ctx := context.Background()
	empty(t, pool)
	store := localStore(t)
	seed(t, pool, store)
	archive, _, _ := export(t, pool, store, Selection{Parts: Parts})

	_, err := Restore(ctx, pool, store, bytes.NewReader(archive), RestoreOptions{
		Mode: ModeMerge, Passphrase: passphrase, Parts: []Part{PartAccounts},
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "replacing the whole server") {
		t.Fatalf("restoring accounts alone: %v", err)
	}
}

// Without the passphrase nothing can be read, and nothing is in the clear.
func TestArchivesAreUnreadableWithoutThePassphrase(t *testing.T) {
	pool := connect(t, "backup_source")
	empty(t, pool)
	store := localStore(t)
	f := seed(t, pool, store)
	archive, _, _ := export(t, pool, store, Selection{Parts: Parts})

	for _, secret := range []string{"a-webhook-signing-secret", "argon2id-hash", f.deletedEventMarker,
		string(f.attachmentBytes), "alice", FormatName} {
		if bytes.Contains(archive, []byte(secret)) {
			t.Errorf("the archive holds %q in the clear", secret)
		}
	}

	if _, err := Open(bytes.NewReader(archive), "not the passphrase at all"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}
	if _, err := Open(bytes.NewReader(archive), passphrase); err != nil {
		t.Errorf("right passphrase: %v", err)
	}
	if _, err := Open(strings.NewReader("not an archive"), passphrase); !errors.Is(err, ErrNotAnArchive) {
		t.Errorf("not an archive: %v", err)
	}
	if err := CheckPassphrase("short"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a short passphrase was accepted")
	}
}

// Choosing channels takes those channels and no others.
func TestChosenChannelsOnly(t *testing.T) {
	pool := connect(t, "backup_source")
	empty(t, pool)
	store := localStore(t)
	f := seed(t, pool, store)

	archive, manifest, summary := export(t, pool, store, Selection{Parts: []Part{PartChannels}, Rooms: []uuid.UUID{f.random}})
	if len(manifest.Rooms) != 1 || manifest.Rooms[0] != f.random {
		t.Errorf("manifest rooms %v", manifest.Rooms)
	}
	for _, table := range summary.Tables {
		want := map[string]int64{"rooms": 1, "events": 1, "room_members": 1, "reactions": 0, "message_views": 0, "users": 3}
		if expected, ok := want[table.Table]; ok && table.Rows != expected {
			t.Errorf("%s: %d rows, want %d", table.Table, table.Rows, expected)
		}
	}
	reader, err := Open(bytes.NewReader(archive), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	for {
		entry, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(entry.Body)
		if bytes.Contains(content, []byte(f.general.String())) && entry.Table != "" && entry.Part == PartChannels {
			t.Errorf("%s holds the channel that was not chosen", entry.Table)
		}
	}

	if _, err := (Selection{Parts: []Part{PartProjects}, Rooms: []uuid.UUID{f.random}}).Resolve(); !errors.Is(err, ErrInvalid) {
		t.Error("projects with only some channels was accepted")
	}
}

// An archive from another schema version is refused, naming the release.
func TestOtherSchemaVersionIsRefused(t *testing.T) {
	err := checkRestore(Manifest{SchemaVersion: 1, ServerVersion: "v0.1.0", Parts: Parts}, RestoreOptions{Mode: ModeReplace}, 2)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "v0.1.0") {
		t.Errorf("another schema version: %v", err)
	}
	err = checkRestore(Manifest{SchemaVersion: 2, Parts: []Part{PartSettings}}, RestoreOptions{Mode: ModeReplace}, 2)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("replace from a partial archive: %v", err)
	}
}
