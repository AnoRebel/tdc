package projects_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
Erasing somebody leaves their projects replayable.

Erasure empties every event the person sent, and the account's deletion takes
their memberships and assignments with it, recording nothing. Without more, the
stream stops at their first emptied event and no longer describes the project.
The account's deletion marks the project for a baseline, and once the server
has appended it the stream reproduces the rows and the progress trend replays
again.
*/
func TestErasingSomebodyLeavesTheirProjectsReplayable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	amina := f.person(t, "amina")
	bola := f.person(t, "bola")
	access := f.project(t, amina, "ERA")
	if err := f.projects.SetMember(ctx, amina, access, "bola", "member"); err != nil {
		t.Fatal(err)
	}
	theirs, err := f.projects.ProjectByID(ctx, bola, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	issue := f.issue(t, bola, theirs, "Written by somebody who leaves")
	if _, _, err := f.projects.Assign(ctx, amina, access, issue, "bola"); err != nil {
		t.Fatal(err)
	}
	f.issue(t, amina, access, "Written by somebody who stays")

	if _, err := f.rooms.Erase(ctx, bola.ID); err != nil {
		t.Fatal(err)
	}

	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projects.RowsFromStream(stream); err == nil {
		t.Fatal("the stream replayed past an emptied event, so this test shows nothing")
	}

	completed, err := f.projects.CompleteStreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("the erasure left %d projects to be given a baseline, want 1", completed)
	}

	stream, err = f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projects.RowsFromStream(stream)
	if err != nil {
		t.Fatalf("after the baseline the stream still does not replay: %v", err)
	}
	current, err := f.projects.CurrentRows(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameRows(t, "the stream's projection", projected, "the tables", current)

	done := map[uuid.UUID]bool{}
	for _, state := range f.states(t, access) {
		done[state.ID] = state.IsCompletion
	}
	counts, err := projects.ReplayProgress(stream, done, projects.ProgressScope{}, []int64{time.Now().UnixMilli() + 1})
	if err != nil {
		t.Fatalf("the progress trend does not replay: %v", err)
	}
	if counts[0].Total != 2 {
		t.Errorf("the trend counts %d issues at the end, want both of them", counts[0].Total)
	}
}
