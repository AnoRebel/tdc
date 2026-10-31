package backup

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/storage"
)

const passphrase = "correct horse battery staple"

func init() {
	// scrypt at its real cost takes a second per archive; the format is the
	// same at any cost.
	workFactor = 10
	maintenanceSettle = 10 * time.Millisecond
	heartbeatEvery = 50 * time.Millisecond
}

func connect(t *testing.T, suite string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbtest.URL(t, suite))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func localStore(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.Open(storage.Config{Backend: storage.BackendLocal, Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	return store
}

// empty truncates everything a restore would, so a database is as new.
func empty(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := readCatalogue(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := emptyEverything(ctx, tx, catalogue); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	alice, bob, admin  uuid.UUID
	general, random    uuid.UUID
	project, issue     uuid.UUID
	rootEvent, reply   string
	attachmentKey      string
	attachmentBytes    []byte
	deletedEventMarker string
}

/*
seed writes at least one row into every part, including both kinds of
reference a restore treats specially: a table referring to itself (a reply's
thread root, a suspension by another account) and a sequence-numbered key.
*/
func seed(t *testing.T, pool *pgxpool.Pool, store storage.Store) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{
		alice: uuid.New(), bob: uuid.New(), admin: uuid.New(),
		general: uuid.New(), random: uuid.New(), project: uuid.New(), issue: uuid.New(),
		rootEvent: "$root-" + uuid.New().String(), reply: "$reply-" + uuid.New().String(),
		attachmentKey:      "attachments/" + uuid.New().String(),
		attachmentBytes:    []byte("the bytes of a file somebody shared"),
		deletedEventMarker: "a message somebody will delete",
	}
	role, project2, state, priority, skill, app, sub := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_ = project2

	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO server_config (key, value, is_secret) VALUES ('registration.policy', 'invitation', false),
			('server.signing_key_seed', 'c2VlZA', true)`, nil},
		{`INSERT INTO server_statuses (id, key, label) VALUES ($1, 'lunch', 'At lunch')`, []any{uuid.New()}},
		// The admin is created after Alice and suspends her: a reference to a
		// row that comes later in key order is what the second pass is for.
		{`INSERT INTO users (id, username, username_folded) VALUES ($1, 'alice', 'alice'), ($2, 'bob', 'bob'), ($3, 'admin', 'admin')`,
			[]any{f.alice, f.bob, f.admin}},
		{`UPDATE users SET suspended_by = $1, suspended_at = NOW() WHERE id = $2`, []any{f.admin, f.alice}},
		{`INSERT INTO credentials (id, user_id, kind, secret) VALUES ($1, $2, 'password', 'argon2id-hash')`, []any{uuid.New(), f.alice}},
		{`INSERT INTO roles (id, name) VALUES ($1, 'Moderator')`, []any{role}},
		{`INSERT INTO role_permissions (role_id, permission) VALUES ($1, 'message.delete.any')`, []any{role}},
		{`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, []any{f.bob, role}},
		{`INSERT INTO rooms (id, name, home_server) VALUES ($1, 'general', 'localhost'), ($2, 'random', 'localhost'),
			($3, 'project', 'localhost'), ($4, 'issue', 'localhost')`, []any{f.general, f.random, f.project, f.issue}},
		{`INSERT INTO room_members (room_id, user_id) VALUES ($1, $2), ($1, $3), ($4, $2)`, []any{f.general, f.alice, f.bob, f.random}},
		{`INSERT INTO events (id, room_id, sequence, sender_id, sender_address, room_address, origin_server, type, content, created_at, signature, key_id)
			VALUES ($1, $2, 1, $3, 'alice@localhost', 'general@localhost', 'localhost', 'message', '{"body":"hello"}', 1, 'sig', 'key-1')`,
			[]any{f.rootEvent, f.general, f.alice}},
		{`INSERT INTO events (id, room_id, sequence, sender_id, sender_address, room_address, origin_server, type, content, created_at, signature, key_id, thread_root_id)
			VALUES ($1, $2, 2, $3, 'bob@localhost', 'general@localhost', 'localhost', 'message', $4, 2, 'sig', 'key-1', $5)`,
			[]any{f.reply, f.general, f.bob, fmt.Sprintf(`{"body":%q}`, f.deletedEventMarker), f.rootEvent}},
		{`INSERT INTO events (id, room_id, sequence, sender_id, sender_address, room_address, origin_server, type, content, created_at, signature, key_id)
			VALUES ($1, $2, 1, $3, 'bob@localhost', 'random@localhost', 'localhost', 'message', '{"body":"elsewhere"}', 3, 'sig', 'key-1')`,
			[]any{"$random-" + uuid.New().String(), f.random, f.bob}},
		{`INSERT INTO reactions (id, event_id, room_id, user_id, user_address, emoji) VALUES ($1, $2, $3, $4, 'bob@localhost', '👍')`,
			[]any{uuid.New(), f.rootEvent, f.general, f.bob}},
		{`INSERT INTO message_views (event_id, viewer_id) VALUES ($1, $2)`, []any{f.rootEvent, f.bob}},
		{`INSERT INTO attachments (id, storage_key, uploader_id, uploader_address, room_id, event_id, filename, content_type, size_bytes)
			VALUES ($1, $2, $3, 'alice@localhost', $4, $5, 'notes.txt', 'text/plain', $6)`,
			[]any{uuid.New(), f.attachmentKey, f.alice, f.general, f.rootEvent, len(f.attachmentBytes)}},
		{`INSERT INTO projects (id, key, key_folded, name) VALUES ($1, 'TDC', 'tdc', 'The platform')`, []any{f.project}},
		{`INSERT INTO issue_states (id, project_id, name, name_folded, position) VALUES ($1, $2, 'Open', 'open', 1)`, []any{state, f.project}},
		{`INSERT INTO project_priorities (id, project_id, name, name_folded, rank) VALUES ($1, $2, 'High', 'high', 1)`, []any{priority, f.project}},
		{`INSERT INTO issues (id, project_id, number, title, state_id, priority_id) VALUES ($1, $2, 1, 'Backups', $3, $4)`,
			[]any{f.issue, f.project, state, priority}},
		{`INSERT INTO skills (id, name, name_folded) VALUES ($1, 'Go', 'go')`, []any{skill}},
		{`INSERT INTO directory_profiles (user_id) VALUES ($1)`, []any{f.alice}},
		{`INSERT INTO profile_skills (user_id, skill_id) VALUES ($1, $2)`, []any{f.alice, skill}},
		{`INSERT INTO applications (id, name, purpose) VALUES ($1, 'Deploy bot', 'Says when a deploy finishes')`, []any{app}},
		{`INSERT INTO api_tokens (id, user_id, label, token_hash) VALUES ($1, $2, 'laptop', '\x0102')`, []any{uuid.New(), f.alice}},
		{`INSERT INTO webhook_subscriptions (id, owner_id, room_id, label, url, secret, event_types, payload_version)
			VALUES ($1, $2, $3, 'ci', 'https://ci.example.org/hook', 'a-webhook-signing-secret', ARRAY['message.created'], 1)`,
			[]any{sub, f.alice, f.general}},
		{`INSERT INTO audit_entries (actor_id, actor_label, action) VALUES ($1, 'admin', 'user.suspended'), (NULL, 'system', 'server.setup.completed')`,
			[]any{f.admin}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement.sql)
		}
	}

	if err := store.Put(ctx, f.attachmentKey, bytes.NewReader(f.attachmentBytes),
		int64(len(f.attachmentBytes)), "text/plain"); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	return f
}

// fingerprint is a table's contents as one value, for comparing two databases.
func fingerprint(t *testing.T, pool *pgxpool.Pool, table string) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(context.Background(),
		"SELECT COALESCE(md5(string_agg(row_to_json(t)::text, '\n' ORDER BY row_to_json(t)::text)), 'empty') FROM "+
			quote(table)+" t").Scan(&value); err != nil {
		t.Fatalf("fingerprint %s: %v", table, err)
	}
	return value
}

func export(t *testing.T, pool *pgxpool.Pool, store storage.Store, selection Selection) ([]byte, Manifest, Summary) {
	t.Helper()
	var archive bytes.Buffer
	manifest, summary, err := Export(context.Background(), pool, store, ExportOptions{
		Selection: selection, Passphrase: passphrase, ServerName: "test", ServerVersion: "test",
	}, &archive)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return archive.Bytes(), manifest, summary
}

func TestEveryTableIsInAPartOrExcluded(t *testing.T) {
	pool := connect(t, "backup_source")
	catalogue, err := readCatalogue(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}

	for name := range catalogue {
		_, _, inPart := partOf(name)
		_, isExcluded := excluded[name]
		_, isOperational := operational[name]
		if !inPart && !isExcluded && !isOperational {
			t.Errorf("table %s is in no backup part and not excluded; add it to partTables or excluded in parts.go", name)
		}
	}
	for _, part := range Parts {
		for _, spec := range partTables[part] {
			if _, ok := catalogue[spec.name]; !ok {
				t.Errorf("part %s names %s, which does not exist", part, spec.name)
			}
		}
	}
	for name := range excluded {
		if _, ok := catalogue[name]; !ok {
			t.Errorf("excluded table %s does not exist", name)
		}
	}
}
