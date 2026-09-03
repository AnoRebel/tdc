package audit_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

func newTestRecorder(t *testing.T) (*audit.Recorder, *dbgen.Queries) {
	t.Helper()

	url := dbtest.URL(t, "audit")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		for _, statement := range []string{"DELETE FROM audit_entries", "DELETE FROM users"} {
			if _, err := pool.Exec(cleanCtx, statement); err != nil {
				t.Errorf("clean: %v", err)
			}
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	queries := dbgen.New(pool)
	return audit.NewRecorder(queries, logger), queries
}

func TestEntryRecordsWhoDidWhatAndWhen(t *testing.T) {
	recorder, queries := newTestRecorder(t)
	ctx := context.Background()

	// A real account: the actor is a foreign key, so that an entry cannot
	// attribute an action to an identifier that never existed.
	actor, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		ID: uuid.New(), Username: "amina", UsernameFolded: "amina", DisplayName: "Amina",
	})
	if err != nil {
		t.Fatalf("create actor: %v", err)
	}
	actorID := actor.ID
	before := time.Now().Add(-time.Second)

	if err := recorder.Record(ctx, audit.Entry{
		ActorID:    &actorID,
		ActorLabel: "amina",
		Action:     audit.ActionConfigChanged,
		TargetKind: "setting",
		TargetID:   "server.name",
		SourceIP:   netip.MustParseAddr("203.0.113.7"),
		UserAgent:  "TestClient/1.0",
		Detail:     map[string]any{"from": "TDC", "to": "TDC Tanzania"},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, listErr := recorder.List(ctx, audit.Query{})
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(entries))
	}

	entry := entries[0]
	if entry.ActorLabel != "amina" {
		t.Errorf("actorLabel = %q, want %q", entry.ActorLabel, "amina")
	}
	if entry.Action != string(audit.ActionConfigChanged) {
		t.Errorf("action = %q, want %q", entry.Action, audit.ActionConfigChanged)
	}
	if entry.TargetID != "server.name" {
		t.Errorf("targetId = %q, want %q", entry.TargetID, "server.name")
	}
	if entry.OccurredAt.Before(before) {
		t.Error("occurredAt predates the action")
	}
	// The previous and new value are what make a configuration change
	// reviewable.
	detail := string(entry.Detail)
	if !strings.Contains(detail, "TDC Tanzania") {
		t.Errorf("detail does not record the new value: %s", detail)
	}
}

// Credentials must never reach the log, even if a caller passes them by
// mistake.
func TestSensitiveDetailIsRedacted(t *testing.T) {
	recorder, _ := newTestRecorder(t)
	ctx := context.Background()

	if err := recorder.Record(ctx, audit.Entry{
		ActorLabel: "careless",
		Action:     audit.ActionPasswordChanged,
		Detail: map[string]any{
			"password":     "hunter2-the-real-one",
			"sessionToken": "a-live-token",
			"username":     "amina",
		},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := recorder.List(ctx, audit.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	detail := string(entries[0].Detail)
	for _, secret := range []string{"hunter2-the-real-one", "a-live-token"} {
		if strings.Contains(detail, secret) {
			t.Errorf("a secret reached the audit log: %s", detail)
		}
	}
	// Non-sensitive context must survive, or the entry loses its value.
	if !strings.Contains(detail, "amina") {
		t.Errorf("non-sensitive detail was removed: %s", detail)
	}
}

// A failed sign-in has no established identity, but must still be recorded.
func TestUnauthenticatedActionIsRecorded(t *testing.T) {
	recorder, _ := newTestRecorder(t)
	ctx := context.Background()

	if err := recorder.Record(ctx, audit.Entry{
		ActorLabel: "attempted-username",
		Action:     audit.ActionSignInFailed,
		SourceIP:   netip.MustParseAddr("198.51.100.4"),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := recorder.List(ctx, audit.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(entries))
	}
	if entries[0].ActorID != nil {
		t.Error("an unauthenticated action was attributed to an account")
	}
	if entries[0].ActorLabel != "attempted-username" {
		t.Error("the attempted identifier was not recorded")
	}
}

func TestEntriesAreFilterable(t *testing.T) {
	recorder, _ := newTestRecorder(t)
	ctx := context.Background()

	for _, action := range []audit.Action{
		audit.ActionSignInSucceeded,
		audit.ActionSignInFailed,
		audit.ActionConfigChanged,
	} {
		if err := recorder.Record(ctx, audit.Entry{ActorLabel: "someone", Action: action}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	wanted := string(audit.ActionSignInFailed)
	entries, err := recorder.List(ctx, audit.Query{Action: &wanted})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("filtered to %d entries, want 1", len(entries))
	}
	if entries[0].Action != wanted {
		t.Errorf("action = %q, want %q", entries[0].Action, wanted)
	}
}

// Entries must be newest first, which is what an administrator investigating
// an incident needs.
func TestEntriesAreNewestFirst(t *testing.T) {
	recorder, _ := newTestRecorder(t)
	ctx := context.Background()

	for _, label := range []string{"first", "second", "third"} {
		if err := recorder.Record(ctx, audit.Entry{
			ActorLabel: label, Action: audit.ActionSignInSucceeded,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	entries, err := recorder.List(ctx, audit.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("listed %d entries, want 3", len(entries))
	}
	if entries[0].ActorLabel != "third" {
		t.Errorf("first listed entry is %q, want the most recent", entries[0].ActorLabel)
	}
}

// Expiry must itself be recorded, so a gap in the log is explained.
func TestPruningIsItselfRecorded(t *testing.T) {
	recorder, queries := newTestRecorder(t)
	ctx := context.Background()

	if err := recorder.Record(ctx, audit.Entry{
		ActorLabel: "old", Action: audit.ActionSignInSucceeded,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Age the entry past the retention period.
	if _, err := queries.AppendAuditEntry(ctx, dbgen.AppendAuditEntryParams{
		ActorLabel: "ancient", Action: "test.old", Detail: []byte("{}"),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	removed, err := recorder.Prune(ctx, time.Nanosecond)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed == 0 {
		t.Fatal("nothing was pruned")
	}

	entries, err := recorder.List(ctx, audit.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("after pruning there are %d entries, want only the pruning record", len(entries))
	}
	if entries[0].Action != "server.audit.pruned" {
		t.Errorf("the pruning was not recorded: action = %q", entries[0].Action)
	}
	if !strings.Contains(string(entries[0].Detail), "removed") {
		t.Error("the pruning record does not say how much was removed")
	}
}

// A failure to record must be reported, so a caller can refuse the privileged
// action rather than performing it unrecorded.
func TestRecordFailureIsReported(t *testing.T) {
	recorder := audit.NewRecorder(failingStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := recorder.Record(context.Background(), audit.Entry{
		ActorLabel: "someone", Action: audit.ActionConfigChanged,
	})
	if !errors.Is(err, audit.ErrNotRecorded) {
		t.Errorf("error = %v, want ErrNotRecorded", err)
	}
}

type failingStore struct{}

func (failingStore) AppendAuditEntry(context.Context, dbgen.AppendAuditEntryParams) (dbgen.AuditEntry, error) {
	return dbgen.AuditEntry{}, errors.New("database unavailable")
}
func (failingStore) ListAuditEntries(context.Context, dbgen.ListAuditEntriesParams) ([]dbgen.AuditEntry, error) {
	return nil, errors.New("database unavailable")
}
func (failingStore) DeleteAuditEntriesBefore(context.Context, time.Time) (int64, error) {
	return 0, errors.New("database unavailable")
}
