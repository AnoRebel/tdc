package projects_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/projects"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

const testServerName = "tdc.test"

// fixture is a projects service over a real, cleaned database.
type fixture struct {
	projects *projects.Service
	rooms    *rooms.Service
	queries  *dbgen.Queries
	pool     *db.Pool
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	url := dbtest.URL(t, "projects")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Open(ctx, db.Config{URL: url}, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		// Projects, issues, states and events all cascade from rooms.
		for _, statement := range []string{"DELETE FROM rooms", "DELETE FROM users"} {
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

	key, err := events.NewKeyPair("test-key")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	queries := dbgen.New(pool)
	roomsService := rooms.NewService(queries, events.NewKeyring(key), testServerName, logger)
	return fixture{
		projects: projects.NewService(pool, roomsService, logger),
		rooms:    roomsService,
		queries:  queries,
		pool:     pool,
	}
}

// person makes an account that may start projects, as the default role allows.
func (f fixture) person(t *testing.T, name string) projects.Actor {
	t.Helper()
	user, err := f.queries.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID: uuid.New(), Username: name, UsernameFolded: name, DisplayName: name,
	})
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	return projects.Actor{
		ID:          user.ID,
		Permissions: authz.Resolve([]authz.Grant{{Permission: authz.PermProjectCreate, Granted: true}}),
	}
}

func (f fixture) project(t *testing.T, actor projects.Actor, key string) projects.Access {
	t.Helper()
	ctx := context.Background()
	project, err := f.projects.Create(ctx, actor, projects.NewProject{Key: key, Name: key})
	if err != nil {
		t.Fatalf("create project %s: %v", key, err)
	}
	access, err := f.projects.ProjectByID(ctx, actor, project.ID)
	if err != nil {
		t.Fatalf("read project %s: %v", key, err)
	}
	return access
}

func (f fixture) states(t *testing.T, access projects.Access) []dbgen.IssueState {
	t.Helper()
	states, err := f.projects.States(context.Background(), access.Project.ID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	return states
}

func (f fixture) issue(t *testing.T, actor projects.Actor, access projects.Access, title string) dbgen.Issue {
	t.Helper()
	issue, err := f.projects.CreateIssue(context.Background(), actor, access, projects.NewIssue{Title: title})
	if err != nil {
		t.Fatalf("file %q: %v", title, err)
	}
	return issue
}

/*
Every event in a project's stream verifies against nothing but the server's
published keys, and the stream is one gapless order.

Checked against a keyring rebuilt from what the server publishes rather than
the one it signs with, because that is all a peer or an auditor would have.
*/
func TestAProjectsStreamIsSignedAndOrdered(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	amina := f.person(t, "amina")
	access := f.project(t, amina, "SIGN")
	states := f.states(t, access)

	issue := f.issue(t, amina, access, "Signed")
	for _, state := range []dbgen.IssueState{states[1], states[2], states[0]} {
		if _, err := f.projects.Transition(ctx, amina, access, issue, state.ID, nil); err != nil {
			t.Fatalf("move: %v", err)
		}
	}

	published, err := events.KeyringFromPublished(f.rooms.Keyring().Published())
	if err != nil {
		t.Fatalf("published keyring: %v", err)
	}

	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if len(stream) < 5 {
		t.Fatalf("the stream holds %d events; want creation, three states, an issue and three moves", len(stream))
	}
	for i, event := range stream {
		if event.Sequence != int64(i+1) {
			t.Errorf("event %d has sequence %d: the stream has a gap or a repeat", i, event.Sequence)
		}
		if err := published.Verify(rooms.Verifiable(event)); err != nil {
			t.Errorf("%s at %d does not verify: %v", event.Type, event.Sequence, err)
		}
	}

	// And one altered in the database stops verifying — which is what makes
	// the signatures worth checking at all.
	moved := stream[len(stream)-1]
	if _, err := f.pool.Exec(ctx,
		`UPDATE events SET content = jsonb_set(content, '{newName}', '"Forged"') WHERE id = $1`, moved.ID); err != nil {
		t.Fatalf("alter event: %v", err)
	}
	altered, err := f.queries.GetEventByID(ctx, moved.ID)
	if err != nil {
		t.Fatalf("read altered event: %v", err)
	}
	if err := published.Verify(rooms.Verifiable(altered)); err == nil {
		t.Error("an event altered in the database still verifies")
	}
}

/*
The issue's columns and the event justifying them are one write.

The project's stream is made unwritable — its home server changed, which the
append refuses — and a move is attempted. It must fail entirely: an issue
showing a state its history never recorded is the drift the design exists to
rule out.
*/
func TestAColumnNeverChangesWithoutItsEvent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	amina := f.person(t, "amina")
	access := f.project(t, amina, "ATOM")
	states := f.states(t, access)
	issue := f.issue(t, amina, access, "All or nothing")

	before, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE rooms SET home_server = 'elsewhere.example' WHERE id = $1`,
		access.Project.ID); err != nil {
		t.Fatalf("make stream unwritable: %v", err)
	}

	_, err = f.projects.Transition(ctx, amina, access, issue, states[2].ID, nil)
	if !errors.Is(err, projects.ErrReplicatedEvent) {
		t.Fatalf("moving with an unwritable stream: %v, want ErrReplicatedEvent", err)
	}
	title := "Renamed"
	if _, err := f.projects.Edit(ctx, amina, access, issue, projects.IssueChanges{Title: &title}); err == nil {
		t.Fatal("editing with an unwritable stream succeeded")
	}
	if _, err := f.projects.CreateIssue(ctx, amina, access, projects.NewIssue{Title: "Orphan"}); err == nil {
		t.Fatal("filing with an unwritable stream succeeded")
	}

	after, err := f.queries.GetIssueByID(ctx, issue.ID)
	if err != nil {
		t.Fatalf("read issue: %v", err)
	}
	if after.StateID != issue.StateID || after.Title != issue.Title {
		t.Errorf("the issue changed without its event: state %v→%v, title %q→%q",
			issue.StateID, after.StateID, issue.Title, after.Title)
	}
	if _, err := f.queries.GetIssueByNumber(ctx, dbgen.GetIssueByNumberParams{
		ProjectID: access.Project.ID, Number: 2,
	}); err == nil {
		t.Error("an issue was filed without its creation being recorded")
	}

	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if len(stream) != len(before) {
		t.Errorf("the stream grew from %d to %d events during failed writes", len(before), len(stream))
	}
}

/*
Replaying a project's stream from nothing reproduces every issue's columns.

Done after a mixture of everything that writes them — filing, moving, renaming,
describing, deleting, and removing a state whose issues had to move — because
each is a separate place a column could be written without its event.
*/
func TestReplayingTheStreamReproducesEveryIssue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	amina := f.person(t, "amina")
	access := f.project(t, amina, "PLAY")
	states := f.states(t, access)

	first := f.issue(t, amina, access, "First")
	second := f.issue(t, amina, access, "Second")
	third := f.issue(t, amina, access, "Third")

	if _, err := f.projects.Transition(ctx, amina, access, first, states[1].ID, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := f.projects.Transition(ctx, amina, access, second, states[2].ID, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	title, description := "Second, renamed", "Now with a description"
	if _, err := f.projects.Edit(ctx, amina, access, second,
		projects.IssueChanges{Title: &title, Description: &description}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := f.projects.Delete(ctx, amina, access, third); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// The first state still holds nothing live but the deleted third issue;
	// removing it moves that one too, and records it.
	moveTo := states[1].ID
	if err := f.projects.RemoveState(ctx, amina, access, states[0].ID, &moveTo); err != nil {
		t.Fatalf("remove state: %v", err)
	}

	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	replayed, err := projects.Replay(stream)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	for _, id := range []uuid.UUID{first.ID, second.ID, third.ID} {
		stored, err := f.queries.GetIssueByID(ctx, id)
		if err != nil {
			t.Fatalf("read issue: %v", err)
		}
		got, ok := replayed[id]
		if !ok {
			t.Fatalf("issue %d does not appear in its project's stream", stored.Number)
		}
		want := projects.Replayed{
			Number: stored.Number, Title: stored.Title, Description: stored.Description,
			StateID: stored.StateID, Deleted: stored.DeletedAt != nil,
		}
		if got != want {
			t.Errorf("issue %d replays as %+v, but its columns say %+v", stored.Number, got, want)
		}
	}
}

/*
Nothing in this feature can rewrite the log.

The stream is appended to and read; no query this feature owns updates or
deletes an event, and no code in the package reaches for the rooms package's
redaction. A test rather than a comment, because the day somebody adds "fix a
typo in the history" the answer should be a failing build.
*/
func TestNothingHereRewritesTheLog(t *testing.T) {
	rewrites := regexp.MustCompile(`(?is)(update\s+events|delete\s+from\s+events|redactevent|redact\()`)

	queries, err := filepath.Glob("../db/queries/projects_*.sql")
	if err != nil || len(queries) == 0 {
		t.Fatalf("find this feature's queries: %v (%d found)", err, len(queries))
	}
	code, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("find this package's code: %v", err)
	}

	for _, path := range append(queries, code...) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if match := rewrites.Find(contents); match != nil {
			t.Errorf("%s rewrites the event log: %q", path, match)
		}
	}
}

// A key is upper-cased, and refused when it could not survive being said
// aloud or pasted into a commit message.
func TestNormaliseKey(t *testing.T) {
	for input, want := range map[string]string{"web": "WEB", " Api2 ": "API2", "tz": "TZ"} {
		if got, err := projects.NormaliseKey(input); err != nil || got != want {
			t.Errorf("NormaliseKey(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "a", "2FA", "WEB-1", "WEB 1", "ABCDEFGHIJK", "MSÍ"} {
		if _, err := projects.NormaliseKey(input); !errors.Is(err, projects.ErrInvalidKey) {
			t.Errorf("NormaliseKey(%q) accepted", input)
		}
	}
}

func TestParseIdentifier(t *testing.T) {
	if key, number, ok := projects.ParseIdentifier("WEB-12"); !ok || key != "WEB" || number != 12 {
		t.Errorf("WEB-12 parsed as %q %d %v", key, number, ok)
	}
	for _, bad := range []string{"WEB", "WEB-", "-12", "WEB-0", "WEB-x", "WEB--1"} {
		if _, _, ok := projects.ParseIdentifier(bad); ok {
			t.Errorf("%q parsed as an identifier", bad)
		}
	}
}
