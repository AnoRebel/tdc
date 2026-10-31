package projects_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

const replicaServerName = "replica.test"

/*
copyStream stores a home server's project events on a replica, as delivery
would: the same identifiers, the same order, nothing re-signed. The project's
room is adopted first, under the home's identifier, the first time.
*/
func copyStream(t *testing.T, r replica, project dbgen.Project, stream []dbgen.Event) dbgen.Room {
	t.Helper()
	ctx := context.Background()
	if err := r.queries.AdoptReplicaRoom(ctx, dbgen.AdoptReplicaRoomParams{
		ID: project.ID, Name: project.Name, Kind: "project", Visibility: "private", HomeServer: testServerName,
	}); err != nil {
		t.Fatal(err)
	}
	for _, event := range stream {
		if _, err := r.queries.StoreForeignEvent(ctx, dbgen.StoreForeignEventParams{
			ID: event.ID, RoomID: project.ID, Sequence: event.Sequence, SenderAddress: event.SenderAddress,
			RoomAddress: event.RoomAddress, OriginServer: event.OriginServer, Type: event.Type,
			Content: event.Content, ContentHash: event.ContentHash, CreatedAt: event.CreatedAt,
			Nonce: event.Nonce, Signature: event.Signature, KeyID: event.KeyID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	room, err := r.queries.GetRoomByID(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	return room
}

// withoutAttribution is rows with who did what removed — the columns a
// replica fills only for its own people — and the key, which names the home.
func withoutAttribution(rows projects.Rows) projects.Rows {
	rows.Project.Key, rows.Project.CreatedBy = "", ""
	for address, member := range rows.Members {
		member.AddedBy = ""
		rows.Members[address] = member
	}
	for id, row := range rows.Labels {
		rows.Labels[id] = row
	}
	for id, cycle := range rows.Cycles {
		cycle.CreatedBy = ""
		rows.Cycles[id] = cycle
	}
	for id, milestone := range rows.Milestones {
		milestone.CreatedBy = ""
		rows.Milestones[id] = milestone
	}
	for id, issue := range rows.Issues {
		issue.DeletedBy = ""
		for assignee := range issue.Assignees {
			issue.Assignees[assignee] = ""
		}
		rows.Issues[id] = issue
	}
	for key, relation := range rows.Relations {
		relation.CreatedBy = ""
		rows.Relations[key] = relation
	}
	return rows
}

/*
A replica's tables are what its stream says, whether it followed the stream as
it grew or was built from it in one go. People of the replica's own server are
its members and assignees by account; everybody else by address. The project is
read on the replica under a key naming its home, by one of the replica's own
people, and a change to anything it holds — a state removed, a label taken off
— reaches the replica as it did the home.
*/
func TestAReplicaHoldsWhatItsStreamSays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	amina := f.person(t, "amina")
	access := f.project(t, amina, "FED")
	must(f.projects.SetFederated(ctx, amina, access, true))
	access, err := f.projects.ProjectByID(ctx, amina, access.Project.ID)
	must(err)
	must(f.projects.SetMember(ctx, amina, access, "dalila@"+replicaServerName, "member"))
	must(f.projects.SetMember(ctx, amina, access, "esi@elsewhere.example", "admin"))
	label, err := f.projects.AddLabel(ctx, amina, access, "backend", "#2563eb")
	must(err)
	review, err := f.projects.AddState(ctx, amina, access, projects.NewState{Name: "Review"})
	must(err)
	first := f.issue(t, amina, access, "Replicate the stream")
	second := f.issue(t, amina, access, "Show it on the replica")
	_, _, err = f.projects.Assign(ctx, amina, access, first, "amina")
	must(err)
	_, err = f.projects.SetLabel(ctx, amina, access, first, label.ID, true)
	must(err)
	_, err = f.projects.Transition(ctx, amina, access, second, review.ID, nil)
	must(err)

	stream, err := f.projects.Stream(ctx, access.Project.ID)
	must(err)
	half := len(stream) / 2

	// One replica follows along, given the stream in two deliveries.
	live := newReplicaOn(t, "projectsreplica", replicaServerName)
	_, err = live.queries.CreateUser(ctx, dbgen.CreateUserParams{
		ID: uuid.New(), Username: "dalila", UsernameFolded: "dalila", DisplayName: "Dalila",
	})
	must(err)
	room := copyStream(t, live, access.Project, stream[:half])
	must(live.projects.Replicate(ctx, room))
	room = copyStream(t, live, access.Project, stream)
	must(live.projects.Replicate(ctx, room))
	if held, err := live.projects.CurrentRows(ctx, access.Project.ID); err != nil || len(held.States) != 4 {
		t.Fatalf("before the home changes anything, the replica holds %d states (%v), want 4", len(held.States), err)
	}

	// Then the home changes things the replica already holds.
	inProgress := uuid.Nil
	for _, state := range f.states(t, access) {
		if state.Name == "In progress" {
			inProgress = state.ID
		}
	}
	must(f.projects.RemoveState(ctx, amina, access, review.ID, &inProgress))
	_, err = f.projects.SetLabel(ctx, amina, access, first, label.ID, false)
	must(err)
	stream, err = f.projects.Stream(ctx, access.Project.ID)
	must(err)
	room = copyStream(t, live, access.Project, stream)
	must(live.projects.Replicate(ctx, room))

	// The other is built from the whole stream at once.
	rebuilt := newReplicaOn(t, "projectsreplicarebuilt", replicaServerName)
	_, err = rebuilt.queries.CreateUser(ctx, dbgen.CreateUserParams{
		ID: uuid.New(), Username: "dalila", UsernameFolded: "dalila", DisplayName: "Dalila",
	})
	must(err)
	must(rebuilt.projects.Replicate(ctx, copyStream(t, rebuilt, access.Project, stream)))

	// A replica's members elsewhere and its switch are its home's to act on:
	// the replica offers its home's project to nobody.
	must(live.projects.SyncReplicas(ctx))
	var offers int
	must(live.pool.QueryRow(ctx, "SELECT count(*) FROM project_replicas").Scan(&offers))
	if offers != 0 {
		t.Errorf("the replica means to offer its home's project to %d servers", offers)
	}

	followed, err := live.projects.CurrentRows(ctx, access.Project.ID)
	must(err)
	whole, err := rebuilt.projects.CurrentRows(ctx, access.Project.ID)
	must(err)
	sameRows(t, "the replica that followed along", followed, "the replica built at once", whole)

	home, err := f.projects.CurrentRows(ctx, access.Project.ID)
	must(err)
	sameRows(t, "the replica", withoutAttribution(followed), "the home", withoutAttribution(home))
	if _, held := followed.States[review.ID]; held {
		t.Error("the replica still holds a state its home removed")
	}

	dalila, err := live.queries.GetUserByUsername(ctx, "dalila")
	must(err)
	reader := projects.Actor{ID: dalila.ID, Permissions: authz.Resolve(nil)}
	seen, err := live.projects.ProjectByKey(ctx, reader, "FED@"+testServerName)
	if err != nil {
		t.Fatalf("a member on the replica cannot see the project under its home's key: %v", err)
	}
	if !projects.IsReplica(seen.Project) || projects.HomeOf(seen.Project) != testServerName || seen.Role != "member" {
		t.Errorf("the replica's project is %+v as %q", seen.Project, seen.Role)
	}
	for _, permission := range []authz.Permission{authz.PermIssueCreate, authz.PermIssueTransition} {
		if decision := seen.Allows(permission); decision.Allowed || !strings.Contains(decision.Reason, testServerName) {
			t.Errorf("on the replica, %s is answered %+v; changes are made on its home", permission, decision)
		}
	}
	board, err := live.projects.Board(ctx, seen, projects.BoardFilter{}, 0)
	must(err)
	cards := 0
	for _, column := range board.Columns {
		cards += len(column.Cards)
	}
	if cards != 2 {
		t.Errorf("the replica's board shows %d issues, want 2", cards)
	}
	if _, _, err := live.projects.IssueByIdentifier(ctx, reader, "FED@"+testServerName+"-1"); err != nil {
		t.Errorf("an issue cannot be found by its identifier on the replica: %v", err)
	}
	if _, _, err := live.projects.IssueByIdentifier(ctx, reader, "FED-1"); err == nil {
		t.Error("FED-1 names the replica's issue, as though the project were this server's")
	}
}

/*
A replica meets an event it has no rule for and stops there, saying why on the
project, holding what it held. Discarding it removes it and its issues'
discussions — at its home's word, and nobody else's.
*/
func TestAReplicaStopsAtWhatItCannotApplyAndIsDiscardedByItsHome(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	amina := f.person(t, "amina")
	access := f.project(t, amina, "STP")
	f.issue(t, amina, access, "Something to hold")
	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}

	r := newReplicaOn(t, "projectsreplica", replicaServerName)
	room := copyStream(t, r, access.Project, stream)
	if err := r.projects.Replicate(ctx, room); err != nil {
		t.Fatal(err)
	}
	before, err := r.projects.CurrentRows(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}

	unknown := stream[len(stream)-1]
	unknown.ID, unknown.Sequence, unknown.Type = "$from-a-newer-server", unknown.Sequence+1, "project.something_new"
	copyStream(t, r, access.Project, []dbgen.Event{unknown})
	if err := r.projects.Replicate(ctx, room); err == nil {
		t.Fatal("a replica applied a stream holding an event it has no rule for")
	}
	project, err := r.queries.GetProjectByID(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if project.ReplicaError == "" {
		t.Error("the replica stopped without saying why")
	}
	after, err := r.projects.CurrentRows(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameRows(t, "the stopped replica", after, "the replica before", before)

	if removed, err := r.projects.DiscardReplica(ctx, access.Project.ID, "somebody.else"); err != nil || removed {
		t.Fatalf("another server had the replica discarded: %v %v", removed, err)
	}
	if removed, err := r.projects.DiscardReplica(ctx, access.Project.ID, testServerName); err != nil || !removed {
		t.Fatalf("the home server could not have its replica discarded: %v %v", removed, err)
	}
	var left int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM rooms").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d rooms remain after the replica was discarded", left)
	}
}
