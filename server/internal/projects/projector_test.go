package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/projects"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
What the Service does that is not a change to a project's rows, so a method
missing from both this and the writes the scenario below makes is a method
nobody has said is either — which is how a new write path that records
nothing would otherwise slip in unnoticed.
*/
var notWrites = map[string]string{
	"Board": "reads", "Column": "reads", "CurrentRows": "reads", "CycleByID": "reads", "Cycles": "reads",
	"DiscussionVisible": "reads", "History": "reads", "IsDiscussion": "reads", "IssueByID": "reads",
	"IssueByIdentifier": "reads", "IssuesFromRoom": "reads", "IssuesInCycle": "reads",
	"IssuesInMilestone": "reads", "Labels": "reads", "ListIssues": "reads", "ListVisible": "reads",
	"Members": "reads", "MilestoneByID": "reads", "MilestoneCycles": "reads", "Milestones": "reads",
	"MyIssues": "reads", "OriginOf": "reads", "Priorities": "reads", "Progress": "reads",
	"ProjectByID": "reads", "ProjectByKey": "reads", "ProjectHistory": "reads", "Relations": "reads",
	"RemoteAccess": "reads", "RemoteMembers": "reads",
	"States": "reads", "Stream": "reads", "Visible": "reads",

	"SaveView":             "a saved view is its owner's and never part of the project",
	"UpdateView":           "a saved view is its owner's and never part of the project",
	"DeleteView":           "a saved view is its owner's and never part of the project",
	"OpenView":             "reads a saved view",
	"Views":                "reads saved views",
	"Participate":          "following a discussion is a person's own setting",
	"AdmitMentioned":       "admits to a discussion room, recorded in that room's own log",
	"Reindex":              "rewrites the search index, which is worked out from the rows",
	"OnAppended":           "wiring, done once while the server is built",
	"MaintainPeriodically": "runs CloseEndedCycles and CompleteStreams on a timer",

	"Replicate":         "writes a replica, from its stream, never a project of this server's",
	"DiscardReplica":    "removes a replica",
	"AcceptOffer":       "starts a replica",
	"SyncReplicas":      "bookkeeping of which servers hold a replica",
	"ClaimReplicaWork":  "bookkeeping of which servers hold a replica",
	"OfferFor":          "reads",
	"ReplicaOffered":    "bookkeeping of which servers hold a replica",
	"ReplicaWorkFailed": "bookkeeping of which servers hold a replica",
	"ForgetReplica":     "bookkeeping of which servers hold a replica",
}

// step is one change the scenario makes, named by the method it goes through.
type step struct {
	method string
	run    func(t *testing.T)
	// unchanged is a step that records without changing any row.
	unchanged bool
}

/*
A project's stream is complete: every way the Service changes a project's rows
records the change, and the stream alone reproduces the rows exactly.

One project, taken through every write the Service offers — every kind of
event a project's stream can hold — and checked after each step: if the rows
changed, the stream must have grown, and the projector applied to the stream
must arrive at exactly the rows the tables hold. The first catches a write that
records nothing; the second, one that records too little to reproduce it.

Then the stream alone is replayed into a second, empty database, and the board,
the lists and the progress figures are read from both and must be identical.
*/
func TestAProjectsStreamReproducesItsRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now()

	amina := f.person(t, "amina")
	f.person(t, "bola")
	f.person(t, "chausiku")

	var access projects.Access
	refresh := func(t *testing.T) {
		t.Helper()
		var err error
		if access, err = f.projects.ProjectByID(ctx, amina, access.Project.ID); err != nil {
			t.Fatal(err)
		}
	}
	must := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stateNamed := func(t *testing.T, name string) uuid.UUID {
		t.Helper()
		for _, state := range f.states(t, access) {
			if state.Name == name {
				return state.ID
			}
		}
		t.Fatalf("no state %q", name)
		return uuid.Nil()
	}
	issueNamed := map[string]dbgen.Issue{}
	issue := func(t *testing.T, title string) dbgen.Issue {
		t.Helper()
		current, err := f.queries.GetIssueByID(ctx, issueNamed[title].ID)
		must(t, err)
		return current
	}
	labels := map[string]uuid.UUID{}
	priorities := map[string]uuid.UUID{}
	cycles := map[string]uuid.UUID{}
	milestones := map[string]uuid.UUID{}
	text := func(value string) *string { return &value }
	listPriorities := func(t *testing.T) []dbgen.ProjectPriority {
		t.Helper()
		current, err := f.projects.Priorities(ctx, access.Project.ID)
		must(t, err)
		return current
	}

	steps := []step{
		{method: "Create", run: func(t *testing.T) {
			project, err := f.projects.Create(ctx, amina, projects.NewProject{
				Key: "FED", Name: "Federation", Description: "Sharing projects between servers",
			})
			must(t, err)
			access.Project = project
			refresh(t)
		}},
		{method: "SetPlanning", run: func(t *testing.T) {
			_, err := f.projects.SetPlanning(ctx, amina, access, "Africa/Dar_es_Salaam", "carry-forward")
			must(t, err)
			refresh(t)
		}},
		{method: "Update", run: func(t *testing.T) {
			name, description, visibility := "Shared projects", "Projects across servers", "private"
			_, err := f.projects.Update(ctx, amina, access, projects.ProjectChanges{
				Name: &name, Description: &description, Visibility: &visibility,
			})
			must(t, err)
			refresh(t)
		}},
		{method: "SetMember", run: func(t *testing.T) {
			must(t, f.projects.SetMember(ctx, amina, access, "bola", "member"))
			must(t, f.projects.SetMember(ctx, amina, access, "chausiku", "member"))
		}},
		{method: "SetMember", run: func(t *testing.T) {
			must(t, f.projects.SetMember(ctx, amina, access, "chausiku", "admin"))
		}},
		{method: "RemoveMember", run: func(t *testing.T) {
			must(t, f.projects.RemoveMember(ctx, amina, access, "chausiku"))
		}},
		{method: "SetFederated", run: func(t *testing.T) {
			must(t, f.projects.SetFederated(ctx, amina, access, true))
			refresh(t)
		}},
		// Somebody on another server, kept by address as the stream names them.
		{method: "SetMember", run: func(t *testing.T) {
			must(t, f.projects.SetMember(ctx, amina, access, "dalila@elsewhere.example", "member"))
			must(t, f.projects.SetMember(ctx, amina, access, "esi@elsewhere.example", "admin"))
		}},
		{method: "SetMember", run: func(t *testing.T) {
			must(t, f.projects.SetMember(ctx, amina, access, "esi@elsewhere.example", "member"))
		}},
		{method: "RemoveMember", run: func(t *testing.T) {
			must(t, f.projects.RemoveMember(ctx, amina, access, "esi@elsewhere.example"))
		}},
		{method: "AddState", run: func(t *testing.T) {
			_, err := f.projects.AddState(ctx, amina, access, projects.NewState{Name: "Review"})
			must(t, err)
			_, err = f.projects.AddState(ctx, amina, access, projects.NewState{Name: "Abandoned", IsCompletion: true})
			must(t, err)
		}},
		{method: "UpdateState", run: func(t *testing.T) {
			name := "In review"
			_, err := f.projects.UpdateState(ctx, amina, access, stateNamed(t, "Review"), projects.StateChanges{Name: &name})
			must(t, err)
		}},
		{method: "ReorderStates", run: func(t *testing.T) {
			states := f.states(t, access)
			order := make([]uuid.UUID, 0, len(states))
			for _, state := range states {
				order = append(order, state.ID)
			}
			// In review before Done.
			order[2], order[3] = order[3], order[2]
			_, err := f.projects.ReorderStates(ctx, amina, access, order)
			must(t, err)
		}},
		{method: "AddLabel", run: func(t *testing.T) {
			for _, name := range []string{"backend", "frontend", "obsolete"} {
				label, err := f.projects.AddLabel(ctx, amina, access, name, "#2563eb")
				must(t, err)
				labels[name] = label.ID
			}
		}},
		{method: "UpdateLabel", run: func(t *testing.T) {
			_, err := f.projects.UpdateLabel(ctx, amina, access, labels["frontend"], text("interface"), text("#16a34a"))
			must(t, err)
		}},
		{method: "AddPriority", run: func(t *testing.T) {
			for _, name := range []string{"Someday", "Never"} {
				priority, err := f.projects.AddPriority(ctx, amina, access, name)
				must(t, err)
				priorities[name] = priority.ID
			}
			for _, priority := range listPriorities(t) {
				priorities[priority.Name] = priority.ID
			}
		}},
		{method: "RenamePriority", run: func(t *testing.T) {
			_, err := f.projects.RenamePriority(ctx, amina, access, priorities["Someday"], "Eventually")
			must(t, err)
		}},
		{method: "ReorderPriorities", run: func(t *testing.T) {
			current := listPriorities(t)
			order := make([]uuid.UUID, 0, len(current))
			for _, priority := range current {
				order = append(order, priority.ID)
			}
			slices.Reverse(order)
			_, err := f.projects.ReorderPriorities(ctx, amina, access, order)
			must(t, err)
		}},
		{method: "CreateCycle", run: func(t *testing.T) {
			for _, cycle := range []projects.NewCycle{
				{Name: "Ended", StartsOn: dayFrom(now, -20), EndsOn: dayFrom(now, -7)},
				{Name: "Current", StartsOn: dayFrom(now, -6), EndsOn: dayFrom(now, 7)},
				{Name: "Next", StartsOn: dayFrom(now, 8), EndsOn: dayFrom(now, 21)},
				{Name: "Unwanted", StartsOn: dayFrom(now, 30), EndsOn: dayFrom(now, 40)},
			} {
				created, err := f.projects.CreateCycle(ctx, amina, access, cycle)
				must(t, err)
				cycles[cycle.Name] = created.ID
			}
		}},
		{method: "UpdateCycle", run: func(t *testing.T) {
			_, err := f.projects.UpdateCycle(ctx, amina, access, cycles["Next"],
				projects.NewCycle{Name: "Following", StartsOn: dayFrom(now, 8), EndsOn: dayFrom(now, 22)})
			must(t, err)
		}},
		{method: "CreateMilestone", run: func(t *testing.T) {
			for _, milestone := range []projects.NewMilestone{
				{Name: "Replicas", Description: "A project on two servers", TargetDate: dayFrom(now, 30)},
				{Name: "Someday", Description: "No date yet"},
				{Name: "Dropped", TargetDate: dayFrom(now, 60)},
			} {
				created, err := f.projects.CreateMilestone(ctx, amina, access, milestone)
				must(t, err)
				milestones[milestone.Name] = created.ID
			}
		}},
		{method: "UpdateMilestone", run: func(t *testing.T) {
			_, err := f.projects.UpdateMilestone(ctx, amina, access, milestones["Replicas"], projects.NewMilestone{
				Name: "Replicas live", Description: "A project read on two servers", TargetDate: dayFrom(now, 35),
			})
			must(t, err)
		}},
		// The same days, at other instants, for every cycle and milestone.
		{method: "SetPlanning", run: func(t *testing.T) {
			_, err := f.projects.SetPlanning(ctx, amina, access, "America/Sao_Paulo", "carry-forward")
			must(t, err)
			refresh(t)
		}},
		{method: "DeleteMilestone", run: func(t *testing.T) {
			must(t, f.projects.DeleteMilestone(ctx, amina, access, milestones["Dropped"]))
		}},
		{method: "CreateIssue", run: func(t *testing.T) {
			review := stateNamed(t, "In review")
			for _, input := range []projects.NewIssue{
				{Title: "Audit the write paths", Description: "Every one"},
				{Title: "Add the missing events"},
				{Title: "Write the projector", StateID: &review},
				{Title: "Replay into an empty database"},
				{Title: "Run it in CI"},
				{Title: "A mistake"},
				{Title: "Left behind"},
			} {
				created, err := f.projects.CreateIssue(ctx, amina, access, input)
				must(t, err)
				issueNamed[input.Title] = created
			}
		}},
		{method: "Edit", run: func(t *testing.T) {
			_, err := f.projects.Edit(ctx, amina, access, issue(t, "Add the missing events"), projects.IssueChanges{
				Title: text("Add every missing event"), Description: text("Labels, priorities, the rest"),
			})
			must(t, err)
		}},
		{method: "Transition", run: func(t *testing.T) {
			_, err := f.projects.Transition(ctx, amina, access, issue(t, "Audit the write paths"), stateNamed(t, "Done"), nil)
			must(t, err)
		}},
		{method: "Assign", run: func(t *testing.T) {
			for _, title := range []string{"Write the projector", "Run it in CI"} {
				_, _, err := f.projects.Assign(ctx, amina, access, issue(t, title), "bola")
				must(t, err)
			}
			_, _, err := f.projects.Assign(ctx, amina, access, issue(t, "Write the projector"), "amina")
			must(t, err)
		}},
		/*
			Somebody on another server, acting through a request their
			server signed: the home records the change as itself, naming
			them, and the projection still agrees with the tables.
		*/
		{method: "CreateIssue", run: func(t *testing.T) {
			theirs, err := f.projects.RemoteAccess(ctx, access.Project.ID, "dalila@elsewhere.example")
			must(t, err)
			created, err := f.projects.CreateIssue(ctx, projects.Actor{Address: "dalila@elsewhere.example"}, theirs,
				projects.NewIssue{Title: "Filed from elsewhere"})
			must(t, err)
			issueNamed["Filed from elsewhere"] = created
		}},
		{method: "Transition", run: func(t *testing.T) {
			theirs, err := f.projects.RemoteAccess(ctx, access.Project.ID, "dalila@elsewhere.example")
			must(t, err)
			_, err = f.projects.Transition(ctx, projects.Actor{Address: "dalila@elsewhere.example"}, theirs,
				issue(t, "Filed from elsewhere"), stateNamed(t, "In progress"), nil)
			must(t, err)
		}},
		{method: "Assign", run: func(t *testing.T) {
			for _, title := range []string{"Filed from elsewhere", "Run it in CI"} {
				_, _, err := f.projects.Assign(ctx, amina, access, issue(t, title), "dalila@elsewhere.example")
				must(t, err)
			}
		}},
		{method: "Unassign", run: func(t *testing.T) {
			must(t, f.projects.Unassign(ctx, amina, access, issue(t, "Run it in CI"), "dalila@elsewhere.example"))
		}},
		{method: "Unassign", run: func(t *testing.T) {
			must(t, f.projects.Unassign(ctx, amina, access, issue(t, "Run it in CI"), "bola"))
		}},
		{method: "SetLabel", run: func(t *testing.T) {
			for title, label := range map[string]string{
				"Write the projector": "backend", "Run it in CI": "frontend", "Left behind": "obsolete",
			} {
				_, err := f.projects.SetLabel(ctx, amina, access, issue(t, title), labels[label], true)
				must(t, err)
			}
			_, err := f.projects.SetLabel(ctx, amina, access, issue(t, "Write the projector"), labels["obsolete"], true)
			must(t, err)
		}},
		{method: "SetLabel", run: func(t *testing.T) {
			_, err := f.projects.SetLabel(ctx, amina, access, issue(t, "Write the projector"), labels["obsolete"], false)
			must(t, err)
		}},
		{method: "RemoveLabel", run: func(t *testing.T) {
			must(t, f.projects.RemoveLabel(ctx, amina, access, labels["obsolete"]))
		}},
		{method: "SetPriority", run: func(t *testing.T) {
			for title, priority := range map[string]string{
				"Write the projector": "Urgent", "Add the missing events": "High", "Left behind": "Never",
			} {
				id := priorities[priority]
				_, err := f.projects.SetPriority(ctx, amina, access, issue(t, title), &id)
				must(t, err)
			}
		}},
		{method: "RemovePriority", run: func(t *testing.T) {
			must(t, f.projects.RemovePriority(ctx, amina, access, priorities["Never"]))
		}},
		{method: "Relate", run: func(t *testing.T) {
			must(t, f.projects.Relate(ctx, amina, access, issue(t, "Write the projector"), "blocks", "FED-4"))
			must(t, f.projects.Relate(ctx, amina, access, issue(t, "Audit the write paths"), "parent", "FED-2"))
			must(t, f.projects.Relate(ctx, amina, access, issue(t, "Run it in CI"), "blocked_by", "FED-4"))
		}},
		{method: "Unrelate", run: func(t *testing.T) {
			must(t, f.projects.Unrelate(ctx, amina, access, issue(t, "Run it in CI"), "blocked_by", "FED-4"))
		}},
		{method: "SetIssueCycle", run: func(t *testing.T) {
			for title, cycle := range map[string]string{
				"Audit the write paths": "Ended", "Add the missing events": "Ended", "Write the projector": "Current",
				"A mistake": "Unwanted", "Left behind": "Current",
			} {
				id := cycles[cycle]
				_, err := f.projects.SetIssueCycle(ctx, amina, access, issue(t, title), &id)
				must(t, err)
			}
		}},
		{method: "SetIssueCycle", run: func(t *testing.T) {
			_, err := f.projects.SetIssueCycle(ctx, amina, access, issue(t, "Left behind"), nil)
			must(t, err)
		}},
		{method: "SetIssueMilestone", run: func(t *testing.T) {
			for title, milestone := range map[string]string{
				"Write the projector": "Replicas", "Replay into an empty database": "Replicas",
				"Run it in CI": "Someday", "A mistake": "Someday",
			} {
				id := milestones[milestone]
				_, err := f.projects.SetIssueMilestone(ctx, amina, access, issue(t, title), &id)
				must(t, err)
			}
		}},
		{method: "SetIssueMilestone", run: func(t *testing.T) {
			_, err := f.projects.SetIssueMilestone(ctx, amina, access, issue(t, "Run it in CI"), nil)
			must(t, err)
		}},
		{method: "Delete", run: func(t *testing.T) {
			must(t, f.projects.Delete(ctx, amina, access, issue(t, "A mistake")))
		}},
		// The deleted issue is still in it, and the database's cascade takes
		// it out with the cycle: no event says so, and the projection must
		// still agree.
		{method: "DeleteCycle", run: func(t *testing.T) {
			must(t, f.projects.DeleteCycle(ctx, amina, access, cycles["Unwanted"]))
		}},
		{method: "Bulk", run: func(t *testing.T) {
			results, err := f.projects.Bulk(ctx, amina, access, []string{"FED-4", "FED-5"},
				projects.BulkChange{Kind: "state", Value: stateNamed(t, "In progress").String()})
			must(t, err)
			for _, result := range results {
				if !result.Changed {
					t.Fatalf("bulk change: %+v", result)
				}
			}
		}},
		{method: "RemoveState", run: func(t *testing.T) {
			// In review still holds an issue, which moves to In progress.
			to := stateNamed(t, "In progress")
			must(t, f.projects.RemoveState(ctx, amina, access, stateNamed(t, "In review"), &to))
		}},
		{method: "CloseEndedCycles", run: func(t *testing.T) {
			closed, err := f.projects.CloseEndedCycles(ctx, now)
			must(t, err)
			if closed != 1 {
				t.Fatalf("closed %d cycles, want 1", closed)
			}
		}},
		/*
			A project made before every change was an event, as far as the
			server can tell: its baseline is appended, and from then on the
			replay starts there.
		*/
		{method: "CompleteStreams", unchanged: true, run: func(t *testing.T) {
			_, err := f.pool.Exec(ctx, "UPDATE projects SET stream_complete = FALSE WHERE id = $1", access.Project.ID)
			must(t, err)
			completed, err := f.projects.CompleteStreams(ctx)
			must(t, err)
			if completed != 1 {
				t.Fatalf("completed %d streams, want 1", completed)
			}
		}},
		{method: "Transition", run: func(t *testing.T) {
			_, err := f.projects.Transition(ctx, amina, access, issue(t, "Left behind"), stateNamed(t, "Abandoned"), nil)
			must(t, err)
		}},
	}

	exercised := map[string]bool{}
	var before projects.Rows
	var length int
	for i, step := range steps {
		t.Run(strconv.Itoa(i)+"-"+step.method, func(t *testing.T) {
			step.run(t)
			exercised[step.method] = true

			after, err := f.projects.CurrentRows(ctx, access.Project.ID)
			must(t, err)
			stream, err := f.projects.Stream(ctx, access.Project.ID)
			must(t, err)

			changed := i == 0 || encoded(t, before) != encoded(t, after)
			if !changed && !step.unchanged {
				t.Fatalf("%s changed no row, so this step proves nothing", step.method)
			}
			if changed && len(stream) == length {
				t.Fatalf("%s changed the project's rows and recorded nothing in its stream", step.method)
			}

			projected, err := projects.RowsFromStream(stream)
			must(t, err)
			sameRows(t, "the stream's projection", projected, "the tables", after)
			before, length = after, len(stream)
		})
		if t.Failed() {
			return
		}
	}

	t.Run("every write is exercised and every method accounted for", func(t *testing.T) {
		service := reflect.TypeOf(f.projects)
		for i := range service.NumMethod() {
			name := service.Method(i).Name
			_, accounted := notWrites[name]
			if !accounted && !exercised[name] {
				t.Errorf("Service.%s is neither a write this test makes nor listed as one that is not", name)
			}
		}
	})

	t.Run("every kind of event is in the stream", func(t *testing.T) {
		stream, err := f.projects.Stream(ctx, access.Project.ID)
		must(t, err)
		recorded := map[string]bool{}
		for _, event := range stream {
			recorded[event.Type] = true
		}
		for _, kind := range projectEventTypes(t) {
			if !recorded[kind] {
				t.Errorf("the scenario never records a %s, so its projection is untested", kind)
			}
		}
	})

	t.Run("every event is signed and in one gapless order", func(t *testing.T) {
		published, err := events.KeyringFromPublished(f.rooms.Keyring().Published())
		must(t, err)
		stream, err := f.projects.Stream(ctx, access.Project.ID)
		must(t, err)
		for i, event := range stream {
			if event.Sequence != int64(i+1) {
				t.Errorf("%s has sequence %d at position %d", event.Type, event.Sequence, i+1)
			}
			if err := published.Verify(rooms.Verifiable(event)); err != nil {
				t.Errorf("%s at %d does not verify: %v", event.Type, event.Sequence, err)
			}
		}
	})

	t.Run("replayed into an empty database, it reads the same", func(t *testing.T) {
		replica := newReplica(t)
		stream, err := f.projects.Stream(ctx, access.Project.ID)
		must(t, err)
		projected, err := projects.RowsFromStream(stream)
		must(t, err)
		replica.write(t, f, projected, stream)

		home, err := f.projects.CurrentRows(ctx, access.Project.ID)
		must(t, err)
		rebuilt, err := replica.projects.CurrentRows(ctx, access.Project.ID)
		must(t, err)
		sameRows(t, "the rebuilt database", rebuilt, "the home database", home)

		compareReads(t, now, f.projects, access, replica.projects, replica.access(t, amina.ID, access.Project.ID))
	})
}

func pgDate(day projects.Day) pgtype.Date {
	return pgtype.Date{Time: time.Date(day.Year, day.Month, day.Date, 0, 0, 0, 0, time.UTC), Valid: true}
}

func encoded(t *testing.T, value any) string {
	t.Helper()
	text, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// sameRows fails, naming the first line that differs, when two projections
// of a project disagree.
func sameRows(t *testing.T, gotName string, got projects.Rows, wantName string, want projects.Rows) {
	t.Helper()
	gotText, wantText := encoded(t, got), encoded(t, want)
	if gotText == wantText {
		return
	}
	gotLines, wantLines := strings.Split(gotText, "\n"), strings.Split(wantText, "\n")
	for i := range min(len(gotLines), len(wantLines)) {
		if gotLines[i] != wantLines[i] {
			from := max(0, i-6)
			t.Fatalf("%s and %s differ at line %d:\n%s:\n%s\n%s:\n%s", gotName, wantName, i,
				gotName, strings.Join(gotLines[from:min(len(gotLines), i+3)], "\n"),
				wantName, strings.Join(wantLines[from:min(len(wantLines), i+3)], "\n"))
		}
	}
	t.Fatalf("%s has %d lines and %s %d", gotName, len(gotLines), wantName, len(wantLines))
}

/*
projectEventTypes is every kind of event a project's stream can hold, read
from the events package's own source rather than from a list kept here — so a
kind added there without a projection, or without being exercised above, fails
this package's tests rather than going unnoticed.
*/
func projectEventTypes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../events/*.go")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	fileSet := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && (strings.HasPrefix(value, "project.") || strings.HasPrefix(value, "issue.")) {
				kinds = append(kinds, value)
			}
			return true
		})
	}
	if len(kinds) < 20 {
		t.Fatalf("found only %d project event types in ../events; is the source where it was?", len(kinds))
	}
	return kinds
}

/*
Every kind of event a project's stream can hold has a projection. A new kind
without one is refused by the projector, which is right for a stream that holds
it, and fails here before any stream does.
*/
func TestEveryProjectEventHasAProjection(t *testing.T) {
	for _, kind := range projectEventTypes(t) {
		rows := projects.Rows{}
		err := projects.Apply(&rows, dbgen.Event{ID: "probe", Type: kind, Content: []byte(`{}`)})
		if errors.Is(err, projects.ErrUnknownEvent) {
			t.Errorf("%s has no projection", kind)
		}
	}
	err := projects.Apply(&projects.Rows{}, dbgen.Event{ID: "probe", Type: "project.invented", Content: []byte(`{}`)})
	if !errors.Is(err, projects.ErrUnknownEvent) {
		t.Errorf("an unknown kind of event was applied rather than refused: %v", err)
	}
}

// replica is a second, empty database, and a projects service over it.
type replica struct {
	pool     *db.Pool
	queries  *dbgen.Queries
	projects *projects.Service
}

func newReplica(t *testing.T) replica {
	t.Helper()
	return newReplicaOn(t, "projectsreplica", testServerName)
}

// newReplicaOn is a second database, under its own suite name, for a server
// called server.
func newReplicaOn(t *testing.T, suite, server string) replica {
	t.Helper()
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{URL: dbtest.URL(t, suite)}, dbtest.Logger())
	if err != nil {
		t.Fatal(err)
	}
	clean := func() {
		for _, statement := range []string{"DELETE FROM rooms", "DELETE FROM users"} {
			if _, err := pool.Exec(context.Background(), statement); err != nil {
				t.Errorf("clean: %v", err)
			}
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})
	key, err := events.NewKeyPair("replica-key")
	if err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(pool)
	roomsService := rooms.NewService(queries, events.NewKeyring(key), server, dbtest.Logger())
	return replica{pool: pool, queries: queries, projects: projects.NewService(pool, roomsService, dbtest.Logger())}
}

func (r replica) access(t *testing.T, userID, projectID uuid.UUID) projects.Access {
	t.Helper()
	access, err := r.projects.ProjectByID(context.Background(), projects.Actor{ID: userID, Permissions: authz.Resolve(nil)}, projectID)
	if err != nil {
		t.Fatal(err)
	}
	return access
}

/*
write puts a projection into the empty database, and the stream beside it.

Nothing is read from the home database but its people, who would on a real
replica be the people the stream names: they are copied with the accounts they
have there, which is what lets the copied events keep naming their senders.
*/
func (r replica) write(t *testing.T, home fixture, rows projects.Rows, stream []dbgen.Event) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	people, err := home.pool.Query(ctx, "SELECT id, username FROM users")
	must(err)
	for people.Next() {
		var id uuid.UUID
		var username string
		must(people.Scan(&id, &username))
		_, err := r.queries.CreateUser(ctx, dbgen.CreateUserParams{
			ID: id, Username: username, UsernameFolded: username, DisplayName: username,
		})
		must(err)
	}
	must(people.Err())
	who := func(address string) *uuid.UUID {
		if address == "" {
			return nil
		}
		name, _, _ := strings.Cut(address, "@")
		user, err := r.queries.GetUserByUsername(ctx, name)
		must(err)
		return &user.ID
	}

	project := rows.Project
	_, err = r.queries.CreateRoom(ctx, dbgen.CreateRoomParams{
		ID: project.ID, Name: project.Name, Kind: "project", Visibility: "private", HomeServer: testServerName,
	})
	must(err)
	_, err = r.queries.CreateProject(ctx, dbgen.CreateProjectParams{
		ID: project.ID, Key: project.Key, KeyFolded: strings.ToLower(project.Key), Name: project.Name,
		Description: project.Description, Visibility: project.Visibility, CreatedBy: who(project.CreatedBy),
	})
	must(err)
	_, err = r.queries.SetProjectPlanning(ctx, dbgen.SetProjectPlanningParams{
		ID: project.ID, TimeZone: project.TimeZone, CyclePolicy: project.CyclePolicy,
	})
	must(err)
	_, err = r.pool.Exec(ctx, "UPDATE projects SET next_issue_number = $2 WHERE id = $1", project.ID, project.NextIssueNumber)
	must(err)

	_, err = r.queries.SetProjectFederated(ctx, dbgen.SetProjectFederatedParams{ID: project.ID, Federated: project.Federated})
	must(err)

	for address, member := range rows.Members {
		if _, server, _ := strings.Cut(address, "@"); server != testServerName {
			must(r.queries.AddProjectRemoteMember(ctx, dbgen.AddProjectRemoteMemberParams{
				ProjectID: project.ID, Address: address, HomeServer: server, Role: member.Role, AddedBy: who(member.AddedBy),
			}))
			continue
		}
		must(r.queries.AddProjectMember(ctx, dbgen.AddProjectMemberParams{
			ProjectID: project.ID, UserID: *who(address), Role: member.Role, AddedBy: who(member.AddedBy),
		}))
	}
	for id, state := range rows.States {
		_, err := r.queries.CreateIssueState(ctx, dbgen.CreateIssueStateParams{
			ID: id, ProjectID: project.ID, Name: state.Name, NameFolded: strings.ToLower(state.Name),
			Position: state.Position, IsCompletion: state.IsCompletion,
		})
		must(err)
	}
	for id, label := range rows.Labels {
		_, err := r.queries.CreateProjectLabel(ctx, dbgen.CreateProjectLabelParams{
			ID: id, ProjectID: project.ID, Name: label.Name, NameFolded: strings.ToLower(label.Name), Colour: label.Colour,
		})
		must(err)
	}
	for id, priority := range rows.Priorities {
		_, err := r.queries.CreateProjectPriority(ctx, dbgen.CreateProjectPriorityParams{
			ID: id, ProjectID: project.ID, Name: priority.Name, NameFolded: strings.ToLower(priority.Name), Rank: priority.Rank,
		})
		must(err)
	}
	for id, cycle := range rows.Cycles {
		starts, err := projects.ParseDay(cycle.StartsOn)
		must(err)
		ends, err := projects.ParseDay(cycle.EndsOn)
		must(err)
		_, err = r.queries.CreateCycle(ctx, dbgen.CreateCycleParams{
			ID: id, ProjectID: project.ID, Name: cycle.Name, StartsOn: pgDate(starts), EndsOn: pgDate(ends),
			StartsAt: cycle.StartsAt, EndsAt: cycle.EndsAt, CreatedBy: who(cycle.CreatedBy),
		})
		must(err)
		if cycle.ClosedAt != nil {
			policy := cycle.ClosedPolicy
			must(r.queries.CloseCycle(ctx, dbgen.CloseCycleParams{ID: id, ClosedAt: cycle.ClosedAt, ClosedPolicy: &policy}))
		}
	}
	for id, milestone := range rows.Milestones {
		params := dbgen.CreateMilestoneParams{
			ID: id, ProjectID: project.ID, Name: milestone.Name, Description: milestone.Description,
			DueAt: milestone.DueAt, CreatedBy: who(milestone.CreatedBy),
		}
		if milestone.TargetDate != "" {
			day, err := projects.ParseDay(milestone.TargetDate)
			must(err)
			params.TargetDate = pgDate(day)
		}
		_, err := r.queries.CreateMilestone(ctx, params)
		must(err)
	}

	for id, issue := range rows.Issues {
		key := project.Key + "-" + strconv.FormatInt(issue.Number, 10)
		_, err := r.queries.CreateRoom(ctx, dbgen.CreateRoomParams{
			ID: id, Name: key, Topic: issue.Title, Kind: "issue-discussion", Visibility: "private", HomeServer: testServerName,
		})
		must(err)
		params := dbgen.CreateIssueParams{
			ID: id, ProjectID: project.ID, Number: issue.Number, Title: issue.Title,
			Description: issue.Description, StateID: issue.State,
		}
		if _, server, _ := strings.Cut(issue.Author, "@"); server != testServerName {
			params.AuthorAddress = &issue.Author
		} else {
			params.AuthorID = who(issue.Author)
		}
		_, err = r.queries.CreateIssue(ctx, params)
		must(err)
		if issue.Priority != nil {
			_, err := r.queries.SetIssuePriority(ctx, dbgen.SetIssuePriorityParams{ID: id, PriorityID: issue.Priority})
			must(err)
		}
		if issue.Deleted {
			_, err := r.queries.MarkIssueDeleted(ctx, dbgen.MarkIssueDeletedParams{ID: id, DeletedBy: who(issue.DeletedBy)})
			must(err)
		} else {
			must(r.queries.IndexIssue(ctx, dbgen.IndexIssueParams{
				IssueID: id, ProjectID: project.ID, Title: issue.Title, Description: issue.Description,
			}))
		}
		for label := range issue.Labels {
			_, err := r.queries.LabelIssue(ctx, dbgen.LabelIssueParams{IssueID: id, LabelID: label})
			must(err)
		}
		for assignee, by := range issue.Assignees {
			if _, server, _ := strings.Cut(assignee, "@"); server != testServerName {
				_, err := r.queries.AssignRemoteToIssue(ctx, dbgen.AssignRemoteToIssueParams{
					IssueID: id, Address: assignee, HomeServer: server, AssignedBy: who(by),
				})
				must(err)
				continue
			}
			_, err := r.queries.AssignIssue(ctx, dbgen.AssignIssueParams{IssueID: id, UserID: *who(assignee), AssignedBy: who(by)})
			must(err)
		}
		if issue.Cycle != nil {
			must(r.queries.SetIssueCycle(ctx, dbgen.SetIssueCycleParams{IssueID: id, CycleID: *issue.Cycle}))
		}
		if issue.Milestone != nil {
			must(r.queries.SetIssueMilestone(ctx, dbgen.SetIssueMilestoneParams{IssueID: id, MilestoneID: *issue.Milestone}))
		}
	}
	for _, relation := range rows.Relations {
		_, err := r.queries.RelateIssues(ctx, dbgen.RelateIssuesParams{
			FromIssue: relation.From, ToIssue: relation.To, Kind: relation.Kind, CreatedBy: who(relation.CreatedBy),
		})
		must(err)
	}

	// The stream, exactly as the home server ordered and signed it.
	for _, event := range stream {
		_, err := r.queries.AppendEvent(ctx, dbgen.AppendEventParams{
			ID: event.ID, RoomID: event.RoomID, Sequence: event.Sequence, SenderID: event.SenderID,
			SenderAddress: event.SenderAddress, RoomAddress: event.RoomAddress, OriginServer: event.OriginServer,
			Type: event.Type, Content: event.Content, ContentHash: event.ContentHash, CreatedAt: event.CreatedAt,
			Nonce: event.Nonce, Signature: event.Signature, KeyID: event.KeyID,
		})
		must(err)
	}
}

/*
compareReads reads what people are shown — the board, the lists, progress,
cycles, milestones and the project's configuration — from two databases, and
fails on any difference.

The board and progress are compared as the JSON the routes send. Rows read
for lists are compared without when each row was written, which is this
database's fact rather than the project's.
*/
func compareReads(t *testing.T, now time.Time, home *projects.Service, homeAccess projects.Access, rebuilt *projects.Service, rebuiltAccess projects.Access) {
	t.Helper()
	ctx := context.Background()
	same := func(what string, read func(*projects.Service, projects.Access) (any, error)) {
		t.Helper()
		want, err := read(home, homeAccess)
		if err != nil {
			t.Fatalf("%s on the home database: %v", what, err)
		}
		got, err := read(rebuilt, rebuiltAccess)
		if err != nil {
			t.Fatalf("%s on the rebuilt database: %v", what, err)
		}
		if encoded(t, got) != encoded(t, want) {
			t.Errorf("%s differs:\nrebuilt: %s\nhome:    %s", what, encoded(t, got), encoded(t, want))
		}
	}

	same("the board", func(s *projects.Service, a projects.Access) (any, error) {
		return s.Board(ctx, a, projects.BoardFilter{}, 0)
	})
	same("the board this cycle", func(s *projects.Service, a projects.Access) (any, error) {
		return s.Board(ctx, a, projects.BoardFilter{Cycle: projects.CycleCurrent}, 0)
	})
	same("the board for one label", func(s *projects.Service, a projects.Access) (any, error) {
		return s.Board(ctx, a, projects.BoardFilter{Label: "backend"}, 0)
	})

	issueList := func(issues []dbgen.Issue) any {
		type listed struct {
			Number       int64
			Title, State string
			Priority     *uuid.UUID
			Deleted      bool
		}
		out := make([]listed, 0, len(issues))
		for _, issue := range issues {
			out = append(out, listed{issue.Number, issue.Title, issue.StateID.String(), issue.PriorityID, issue.DeletedAt != nil})
		}
		return out
	}
	same("the issue list", func(s *projects.Service, a projects.Access) (any, error) {
		issues, err := s.ListIssues(ctx, a, projects.IssueFilter{})
		return issueList(issues), err
	})
	same("the issue list by priority", func(s *projects.Service, a projects.Access) (any, error) {
		issues, err := s.ListIssues(ctx, a, projects.IssueFilter{ByPriority: true})
		return issueList(issues), err
	})

	zone := projects.Zone(homeAccess.Project)
	today := now.In(zone)
	to := projects.Day{Year: today.Year(), Month: today.Month(), Date: today.Day()}
	from, err := projects.ParseDay(today.AddDate(0, 0, -30).Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	same("the project's progress", func(s *projects.Service, a projects.Access) (any, error) {
		return s.Progress(ctx, a, projects.ProgressScope{}, from, to, now)
	})

	cycles, err := home.Cycles(ctx, homeAccess, now)
	if err != nil {
		t.Fatal(err)
	}
	same("the cycles", func(s *projects.Service, a projects.Access) (any, error) {
		cycles, err := s.Cycles(ctx, a, now)
		type shown struct {
			ID               uuid.UUID
			Name, Status     string
			Total, Completed int
			StartsAt, EndsAt time.Time
		}
		out := make([]shown, 0, len(cycles))
		for _, cycle := range cycles {
			out = append(out, shown{cycle.ID, cycle.Name, cycle.Status, cycle.Total, cycle.Completed, cycle.StartsAt.UTC(), cycle.EndsAt.UTC()})
		}
		return out, err
	})
	for _, cycle := range cycles {
		id := cycle.ID
		same("progress of cycle "+cycle.Name, func(s *projects.Service, a projects.Access) (any, error) {
			return s.Progress(ctx, a, projects.ProgressScope{Cycle: &id}, from, to, now)
		})
	}

	milestones, err := home.Milestones(ctx, homeAccess, now)
	if err != nil {
		t.Fatal(err)
	}
	same("the milestones", func(s *projects.Service, a projects.Access) (any, error) {
		milestones, err := s.Milestones(ctx, a, now)
		type shown struct {
			ID                uuid.UUID
			Name, Description string
			Total, Completed  int
			Overdue           bool
		}
		out := make([]shown, 0, len(milestones))
		for _, milestone := range milestones {
			out = append(out, shown{milestone.ID, milestone.Name, milestone.Description, milestone.Total, milestone.Completed, milestone.Overdue})
		}
		return out, err
	})
	for _, milestone := range milestones {
		id := milestone.ID
		same("progress of milestone "+milestone.Name, func(s *projects.Service, a projects.Access) (any, error) {
			return s.Progress(ctx, a, projects.ProgressScope{Milestone: &id}, from, to, now)
		})
	}

	same("the states", func(s *projects.Service, a projects.Access) (any, error) {
		states, err := s.States(ctx, a.Project.ID)
		names := make([]string, 0, len(states))
		for _, state := range states {
			names = append(names, state.ID.String()+" "+state.Name+" "+strconv.FormatBool(state.IsCompletion))
		}
		return names, err
	})
	same("the priorities", func(s *projects.Service, a projects.Access) (any, error) {
		priorities, err := s.Priorities(ctx, a.Project.ID)
		names := make([]string, 0, len(priorities))
		for _, priority := range priorities {
			names = append(names, priority.ID.String()+" "+priority.Name)
		}
		return names, err
	})
	same("the members", func(s *projects.Service, a projects.Access) (any, error) {
		members, err := s.Members(ctx, a)
		names := make([]string, 0, len(members))
		for _, member := range members {
			names = append(names, member.Username+" "+member.Role)
		}
		return names, err
	})
}
