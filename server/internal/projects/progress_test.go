package projects_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
What progress reports equals what the history says, exactly, for the project,
a cycle and a milestone alike.

The figures are counted from the issues as they are; the trend is replayed
from the event log alone. Today's point of the trend and the figures must
agree — after moves, after an issue changes cycle, after one is deleted —
because there is no stored number for either to drift from.
*/
func TestProgressIsWhatTheHistorySays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.plan(t, owner, f.project(t, owner, "PROG"), "Africa/Nairobi", "")
	now := time.Now()

	states := f.states(t, access)
	var done, doing dbgen.IssueState
	for _, state := range states {
		switch {
		case state.IsCompletion:
			done = state
		case state.Position > 0:
			doing = state
		}
	}
	cycle := f.cycle(t, owner, access, "Now", dayFrom(now, -3), dayFrom(now, 10))
	milestone, err := f.projects.CreateMilestone(ctx, owner, access, projects.NewMilestone{Name: "Beta"})
	if err != nil {
		t.Fatal(err)
	}

	var issues []dbgen.Issue
	for _, title := range []string{"One", "Two", "Three", "Four", "Five", "Six"} {
		issues = append(issues, f.issue(t, owner, access, title))
	}
	for _, issue := range issues[:4] {
		f.intoCycle(t, owner, access, issue, &cycle.ID)
	}
	for _, issue := range issues[2:5] {
		if _, err := f.projects.SetIssueMilestone(ctx, owner, access, issue, &milestone.ID); err != nil {
			t.Fatal(err)
		}
	}
	move := func(issue dbgen.Issue, to dbgen.IssueState) {
		t.Helper()
		current, err := f.queries.GetIssueByID(ctx, issue.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.projects.Transition(ctx, owner, access, current, to.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	move(issues[0], done)
	move(issues[2], doing)
	move(issues[2], done)
	move(issues[4], done)
	move(issues[0], doing) // reopened
	move(issues[0], done)
	// Out of the cycle, and one deleted: both must leave the counts.
	f.intoCycle(t, owner, access, issues[1], nil)
	if err := f.projects.Delete(ctx, owner, access, issues[3]); err != nil {
		t.Fatal(err)
	}

	from, _ := projects.ParseDay(dayFrom(now, -2))
	to, _ := projects.ParseDay(dayFrom(now, 5))
	for _, scope := range []struct {
		name                string
		scope               projects.ProgressScope
		wantTotal, wantDone int
	}{
		{"project", projects.ProgressScope{}, 5, 3},
		{"cycle", projects.ProgressScope{Cycle: &cycle.ID}, 2, 2},
		{"milestone", projects.ProgressScope{Milestone: &milestone.ID}, 2, 2},
	} {
		progress, err := f.projects.Progress(ctx, access, scope.scope, from, to, now)
		if err != nil {
			t.Fatalf("%s: %v", scope.name, err)
		}
		if progress.Total != scope.wantTotal || progress.Completed != scope.wantDone {
			t.Errorf("%s reports %d of %d, want %d of %d", scope.name,
				progress.Completed, progress.Total, scope.wantDone, scope.wantTotal)
		}
		last := progress.Trend[len(progress.Trend)-1]
		if last.Total != progress.Total || last.Completed != progress.Completed {
			t.Errorf("%s: the history says %d of %d, the issues %d of %d", scope.name,
				last.Completed, last.Total, progress.Completed, progress.Total)
		}
		// The future is not reported: the trend stops at today.
		if progress.To != dayFrom(now.In(projects.Zone(access.Project)), 0) || len(progress.Trend) != 3 {
			t.Errorf("%s runs to %s with %d points", scope.name, progress.To, len(progress.Trend))
		}
	}
}

/*
The trend is the history at each moment, not today's answer repeated.

Boundaries placed between events show the counts as they were then: before
anything was finished, between two moves, after a reopening.
*/
func TestTheTrendIsReplayedAtEachMoment(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.project(t, owner, "TREND")
	states := f.states(t, access)
	todo := states[0]
	var done dbgen.IssueState
	for _, state := range states {
		if state.IsCompletion {
			done = state
		}
	}
	// Apart in time, so a boundary just after one event is before the next.
	apart := func() { time.Sleep(3 * time.Millisecond) }

	first := f.issue(t, owner, access, "First")
	apart()
	second := f.issue(t, owner, access, "Second")
	apart()
	moved, err := f.projects.Transition(ctx, owner, access, first, done.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	apart()
	if _, err := f.projects.Transition(ctx, owner, access, moved, todo.ID, nil); err != nil {
		t.Fatal(err)
	}
	apart()
	if err := f.projects.Delete(ctx, owner, access, second); err != nil {
		t.Fatal(err)
	}

	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	// After each of the issue events: an instant just past it.
	var after []int64
	for _, event := range stream {
		switch event.Type {
		case "issue.created", "issue.state_changed", "issue.deleted":
			after = append(after, event.CreatedAt+1)
		}
	}
	doneStates := map[uuid.UUID]bool{done.ID: true}
	counts, err := projects.ReplayProgress(stream, doneStates, projects.ProgressScope{}, after)
	if err != nil {
		t.Fatal(err)
	}
	want := []projects.Counted{{1, 0}, {2, 0}, {2, 1}, {2, 0}, {1, 0}}
	if len(counts) != len(want) {
		t.Fatalf("%d points, want %d", len(counts), len(want))
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Errorf("after event %d the history says %+v, want %+v", i, counts[i], want[i])
		}
	}
}
