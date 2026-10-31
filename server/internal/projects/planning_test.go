package projects_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

func (f fixture) plan(t *testing.T, actor projects.Actor, access projects.Access, zone, policy string) projects.Access {
	t.Helper()
	updated, err := f.projects.SetPlanning(context.Background(), actor, access, zone, policy)
	if err != nil {
		t.Fatalf("set planning %s %s: %v", zone, policy, err)
	}
	access.Project = updated
	return access
}

func (f fixture) cycle(t *testing.T, actor projects.Actor, access projects.Access, name, starts, ends string) dbgen.Cycle {
	t.Helper()
	cycle, err := f.projects.CreateCycle(context.Background(), actor, access,
		projects.NewCycle{Name: name, StartsOn: starts, EndsOn: ends})
	if err != nil {
		t.Fatalf("create cycle %s: %v", name, err)
	}
	return cycle
}

func (f fixture) intoCycle(t *testing.T, actor projects.Actor, access projects.Access, issue dbgen.Issue, cycle *uuid.UUID) {
	t.Helper()
	if _, err := f.projects.SetIssueCycle(context.Background(), actor, access, issue, cycle); err != nil {
		t.Fatalf("put %d into cycle: %v", issue.Number, err)
	}
}

func (f fixture) cycleOf(t *testing.T, issue dbgen.Issue) *uuid.UUID {
	t.Helper()
	cycle, err := f.queries.GetIssueCycle(context.Background(), issue.ID)
	if err != nil {
		return nil
	}
	return &cycle.ID
}

func mustInstant(t *testing.T, text string) time.Time {
	t.Helper()
	instant, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return instant
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return zone
}

/*
A day is the project's day, wherever the server or its reader is.

The cycle "ends on the 18th" for a team in Nairobi ends when the 18th ends in
Nairobi — 21:00 UTC — and is still the 18th when read back, whatever zone the
process happens to run in. Run three times with the process's own zone set to
Honolulu, Tokyo and a bare +03:00, because the failure this catches is a day
built or read through the process's zone: it passes wherever the developer
lives and is a day out for somebody twelve hours away.
*/
func TestDaysAreTheProjectsCalendar(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")

	for i, local := range []*time.Location{
		mustZone(t, "Pacific/Honolulu"), mustZone(t, "Asia/Tokyo"), time.FixedZone("+0300", 3*60*60),
	} {
		t.Run(local.String(), func(t *testing.T) {
			previous := time.Local
			time.Local = local
			t.Cleanup(func() { time.Local = previous })

			access := f.plan(t, owner, f.project(t, owner, string(rune('A'+i))+"CAL"), "Africa/Nairobi", "")
			cycle := f.cycle(t, owner, access, "Sprint", "2026-10-05", "2026-10-18")
			if want := mustInstant(t, "2026-10-04T21:00:00Z"); !cycle.StartsAt.Equal(want) {
				t.Errorf("starts at %s, want %s", cycle.StartsAt.UTC(), want)
			}
			if want := mustInstant(t, "2026-10-18T21:00:00Z"); !cycle.EndsAt.Equal(want) {
				t.Errorf("ends at %s, want %s", cycle.EndsAt.UTC(), want)
			}

			read, err := f.projects.CycleByID(ctx, access, cycle.ID, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			starts, _ := projects.DayOf(read.StartsOn)
			ends, _ := projects.DayOf(read.EndsOn)
			if starts.String() != "2026-10-05" || ends.String() != "2026-10-18" {
				t.Errorf("the days came back as %s to %s", starts, ends)
			}

			milestone, err := f.projects.CreateMilestone(ctx, owner, access,
				projects.NewMilestone{Name: "Launch", TargetDate: "2026-10-18"})
			if err != nil {
				t.Fatal(err)
			}
			unfinished := f.issue(t, owner, access, "Unfinished")
			if _, err := f.projects.SetIssueMilestone(ctx, owner, access, unfinished, &milestone.ID); err != nil {
				t.Fatal(err)
			}
			overdueAt := func(instant string) bool {
				t.Helper()
				read, err := f.projects.MilestoneByID(ctx, access, milestone.ID, mustInstant(t, instant))
				if err != nil {
					t.Fatal(err)
				}
				return read.Overdue
			}
			// Due when the 18th is over in Nairobi, not when it begins.
			if overdueAt("2026-10-18T20:59:59Z") || !overdueAt("2026-10-18T21:00:00Z") {
				t.Error("in Nairobi the milestone is not overdue exactly from the end of its day")
			}

			// The same days in another calendar: the instants move, the days do not.
			access = f.plan(t, owner, access, "Pacific/Honolulu", "")
			moved, err := f.projects.CycleByID(ctx, access, cycle.ID, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if want := mustInstant(t, "2026-10-19T10:00:00Z"); !moved.EndsAt.Equal(want) {
				t.Errorf("after moving to Honolulu it ends at %s, want %s", moved.EndsAt.UTC(), want)
			}
			if ends, _ := projects.DayOf(moved.EndsOn); ends.String() != "2026-10-18" {
				t.Errorf("after moving to Honolulu it ends on %s", ends)
			}

			// And the milestone is overdue from the instant its day is over there.
			if before, after := overdueAt("2026-10-19T09:59:59Z"), overdueAt("2026-10-19T10:00:00Z"); before || !after {
				t.Errorf("in Honolulu, overdue a second before the day ends: %v; at its end: %v", before, after)
			}
		})
	}
}

// historyOf reads one issue's events.
func (f fixture) historyOf(t *testing.T, access projects.Access, issue dbgen.Issue) []dbgen.Event {
	t.Helper()
	history, err := f.projects.History(context.Background(), access, issue)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

func dayFrom(now time.Time, days int) string {
	return now.UTC().AddDate(0, 0, days).Format(time.DateOnly)
}

/*
Ending a cycle under carry-forward moves every unfinished issue into the next
cycle, records each move as the server's, and touches nothing else: no state
changes, no issue disappears, and what was finished stays where it was done.
*/
func TestEndingACycleCarriesUnfinishedWorkForward(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.plan(t, owner, f.project(t, owner, "CARRY"), "UTC", "carry-forward")
	now := time.Now()

	ended := f.cycle(t, owner, access, "Ended", dayFrom(now, -20), dayFrom(now, -7))
	current := f.cycle(t, owner, access, "Current", dayFrom(now, -6), dayFrom(now, 7))
	f.cycle(t, owner, access, "Later", dayFrom(now, 8), dayFrom(now, 21))

	states := f.states(t, access)
	var done dbgen.IssueState
	for _, state := range states {
		if state.IsCompletion {
			done = state
		}
	}
	finished := f.issue(t, owner, access, "Finished")
	open := []dbgen.Issue{f.issue(t, owner, access, "Open one"), f.issue(t, owner, access, "Open two")}
	outside := f.issue(t, owner, access, "In no cycle")
	for _, issue := range append([]dbgen.Issue{finished}, open...) {
		f.intoCycle(t, owner, access, issue, &ended.ID)
	}
	moved, err := f.projects.Transition(ctx, owner, access, finished, done.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	finished = moved

	closed, err := f.projects.CloseEndedCycles(ctx, now)
	if err != nil || closed != 1 {
		t.Fatalf("closed %d cycles: %v", closed, err)
	}

	for _, issue := range open {
		if got := f.cycleOf(t, issue); got == nil || *got != current.ID {
			t.Errorf("%d is in %v, want the current cycle", issue.Number, got)
		}
		after, err := f.queries.GetIssueByID(ctx, issue.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.StateID != issue.StateID || after.DeletedAt != nil {
			t.Errorf("%d changed state or was removed when its cycle ended", issue.Number)
		}
		history := f.historyOf(t, access, issue)
		last := history[len(history)-1]
		var content map[string]any
		_ = json.Unmarshal(last.Content, &content)
		if last.Type != string(events.TypeIssueCycleChanged) || last.SenderAddress != testServerName ||
			last.SenderID != nil || content["reason"] != "cycle_ended" || content["newName"] != "Current" {
			t.Errorf("%d's last event is %s by %s: %v", issue.Number, last.Type, last.SenderAddress, content)
		}
	}
	if got := f.cycleOf(t, finished); got == nil || *got != ended.ID {
		t.Errorf("the finished issue moved out of the cycle it was finished in: %v", got)
	}
	if f.cycleOf(t, outside) != nil {
		t.Error("an issue in no cycle was put into one")
	}

	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	for _, event := range stream {
		if event.Type == string(events.TypeProjectCycleEnded) {
			_ = json.Unmarshal(event.Content, &record)
		}
	}
	unfinished, _ := record["unfinished"].([]any)
	if record["policy"] != "carry-forward" || record["carriedToName"] != "Current" || len(unfinished) != 2 {
		t.Errorf("the cycle's end was recorded as %v", record)
	}

	// Done once: a second pass finds nothing to do and changes nothing.
	recorded := len(f.historyOf(t, access, open[0]))
	if again, err := f.projects.CloseEndedCycles(ctx, now); err != nil || again != 0 {
		t.Errorf("a second pass closed %d: %v", again, err)
	}
	if count := len(f.historyOf(t, access, open[0])); count != recorded {
		t.Errorf("a second pass added to the history: %d events, was %d", count, recorded)
	}
}

// With nowhere to carry it, unfinished work leaves the ended cycle for no
// cycle at all — still there, and saying so.
func TestCarryingForwardWithNoNextCycleLeavesWorkUnplanned(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.plan(t, owner, f.project(t, owner, "LAST"), "UTC", "carry-forward")
	now := time.Now()

	ended := f.cycle(t, owner, access, "Only", dayFrom(now, -14), dayFrom(now, -1))
	issue := f.issue(t, owner, access, "Never got to it")
	f.intoCycle(t, owner, access, issue, &ended.ID)

	if _, err := f.projects.CloseEndedCycles(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got := f.cycleOf(t, issue); got != nil {
		t.Errorf("still in %v", got)
	}
	if _, err := f.queries.GetIssueByID(ctx, issue.ID); err != nil {
		t.Errorf("the issue is gone: %v", err)
	}
}

/*
Under remain, an ended cycle keeps its unfinished work: nothing moves, the
cycle is marked dealt with, and its end is still recorded with what was left.
*/
func TestEndingACycleUnderRemainLeavesWorkInIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.plan(t, owner, f.project(t, owner, "STAY"), "UTC", "remain")
	now := time.Now()

	ended := f.cycle(t, owner, access, "Ended", dayFrom(now, -20), dayFrom(now, -7))
	f.cycle(t, owner, access, "Current", dayFrom(now, -6), dayFrom(now, 7))
	issues := []dbgen.Issue{f.issue(t, owner, access, "One"), f.issue(t, owner, access, "Two")}
	for _, issue := range issues {
		f.intoCycle(t, owner, access, issue, &ended.ID)
	}

	if closed, err := f.projects.CloseEndedCycles(ctx, now); err != nil || closed != 1 {
		t.Fatalf("closed %d: %v", closed, err)
	}
	for _, issue := range issues {
		if got := f.cycleOf(t, issue); got == nil || *got != ended.ID {
			t.Errorf("%d left the cycle under remain: %v", issue.Number, got)
		}
		types := []string{}
		for _, event := range f.historyOf(t, access, issue) {
			types = append(types, event.Type)
		}
		if !slices.Equal(types, []string{"issue.created", "issue.cycle_changed"}) {
			t.Errorf("%d's history is %v", issue.Number, types)
		}
	}
	read, err := f.projects.CycleByID(ctx, access, ended.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if read.Status != "ended" || read.ClosedPolicy == nil || *read.ClosedPolicy != "remain" || read.Total != 2 {
		t.Errorf("the ended cycle reads %s, %v, %d issues", read.Status, read.ClosedPolicy, read.Total)
	}
}

// A milestone past its day is overdue and that is all: no issue in it changes.
func TestAnOverdueMilestoneChangesNoIssue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.plan(t, owner, f.project(t, owner, "LATE"), "UTC", "")
	now := time.Now()

	milestone, err := f.projects.CreateMilestone(ctx, owner, access,
		projects.NewMilestone{Name: "Beta", TargetDate: dayFrom(now, -3)})
	if err != nil {
		t.Fatal(err)
	}
	issue := f.issue(t, owner, access, "Still open")
	if _, err := f.projects.SetIssueMilestone(ctx, owner, access, issue, &milestone.ID); err != nil {
		t.Fatal(err)
	}
	before, err := f.queries.GetIssueByID(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}

	read, err := f.projects.MilestoneByID(ctx, access, milestone.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !read.Overdue || read.Total != 1 || read.Completed != 0 {
		t.Errorf("the milestone reads overdue=%v, %d of %d", read.Overdue, read.Completed, read.Total)
	}
	if _, err := f.projects.CloseEndedCycles(ctx, now); err != nil {
		t.Fatal(err)
	}
	after, err := f.queries.GetIssueByID(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.StateID != before.StateID || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Error("an issue changed because its milestone was overdue")
	}
}

// Somebody who may see a project but is not in it cannot plan its work.
func TestPlanningIsForMembers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.person(t, "owner")
	access := f.project(t, owner, "PLAN")
	cycle := f.cycle(t, owner, access, "One", "2026-11-02", "2026-11-15")
	issue := f.issue(t, owner, access, "Something")

	stranger := f.person(t, "stranger")
	theirs := projects.AccessWithRole(stranger, access.Project, "")
	if _, err := f.projects.SetIssueCycle(ctx, stranger, theirs, issue, &cycle.ID); err == nil {
		t.Error("somebody outside the project put an issue into a cycle")
	}
	if _, err := f.projects.CreateCycle(ctx, stranger, theirs,
		projects.NewCycle{Name: "Theirs", StartsOn: "2026-12-01", EndsOn: "2026-12-14"}); err == nil {
		t.Error("somebody outside the project made a cycle")
	}
	if _, err := f.projects.CreateCycle(ctx, owner, access,
		projects.NewCycle{Name: "Overlapping", StartsOn: "2026-11-10", EndsOn: "2026-11-20"}); err == nil {
		t.Error("two cycles of one project overlap")
	}
}
