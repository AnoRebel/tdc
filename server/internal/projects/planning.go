package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	// The zone database, inside the binary. A project's calendar is an IANA
	// zone, and a server image with no /usr/share/zoneinfo — a scratch
	// container — must still know where Dar es Salaam's day ends.
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// Errors a caller is expected to handle.
var (
	ErrTimeZone          = errors.New("that is not a time zone this server knows; use an IANA name such as Africa/Dar_es_Salaam")
	ErrCyclePolicy       = errors.New("a cycle's unfinished work either carries forward or remains")
	ErrCycleNotFound     = errors.New("no such cycle in this project")
	ErrCycleName         = errors.New("a cycle needs a name of at most sixty characters")
	ErrCycleDates        = errors.New("a cycle runs from a start date to an end date no earlier, and for at most a year")
	ErrCycleOverlap      = errors.New("that overlaps another of this project's cycles")
	ErrCycleInUse        = errors.New("issues are in this cycle; move them out before removing it")
	ErrMilestoneNotFound = errors.New("no such milestone in this project")
	ErrMilestoneName     = errors.New("a milestone needs a name of at most eighty characters")
	ErrDate              = errors.New("a date is written as YYYY-MM-DD")
)

/*
Day is a date in a project's calendar, with no time and no zone: the 18th.

People plan in days. "The cycle ends on the 18th" is true in the team's
calendar, and the instant the 18th is over differs between Dar es Salaam and
Honolulu by thirteen hours. So the day is what is stored, as the person chose
it, and the instants are worked out from it in the project's zone — never the
other way round, which is how a date typed in one zone and stored as midnight
UTC comes back as the 17th for somebody west of Greenwich.
*/
type Day struct {
	Year  int
	Month time.Month
	Date  int
}

// ParseDay reads YYYY-MM-DD.
func ParseDay(text string) (Day, error) {
	parsed, err := time.Parse(time.DateOnly, strings.TrimSpace(text))
	if err != nil {
		return Day{}, ErrDate
	}
	return Day{parsed.Year(), parsed.Month(), parsed.Day()}, nil
}

func (d Day) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Date)
}

/*
StartIn is the first instant of the day in a zone.

Built from the day's own numbers in that zone, deliberately: this is the one
place a wall-clock reconstruction is the right answer, because "midnight on
the 18th in Nairobi" is exactly what is meant. Where midnight does not exist —
a zone that springs forward at 00:00 — Go moves to the first instant that does.
*/
func (d Day) StartIn(zone *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Date, 0, 0, 0, 0, zone)
}

// EndIn is the first instant after the day, in a zone: when it is over.
func (d Day) EndIn(zone *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Date+1, 0, 0, 0, 0, zone)
}

func (d Day) pg() pgtype.Date {
	return pgtype.Date{Time: time.Date(d.Year, d.Month, d.Date, 0, 0, 0, 0, time.UTC), Valid: true}
}

func dayOf(value pgtype.Date) Day {
	return Day{value.Time.Year(), value.Time.Month(), value.Time.Day()}
}

// DayOf reads a stored date back as the day it is.
func DayOf(value pgtype.Date) (Day, bool) {
	if !value.Valid {
		return Day{}, false
	}
	return dayOf(value), true
}

func (d Day) before(other Day) bool {
	return d.StartIn(time.UTC).Before(other.StartIn(time.UTC))
}

// Zone is a project's calendar: its zone, or UTC if what is stored is
// somehow unknown to this server.
func Zone(project dbgen.Project) *time.Location {
	zone, err := time.LoadLocation(project.TimeZone)
	if err != nil {
		return time.UTC
	}
	return zone
}

/*
SetPlanning sets a project's calendar and what happens to unfinished work when
a cycle ends.

Changing the zone keeps every cycle and milestone on the same days and moves
the instants those days begin and end at, in the same transaction — so a cycle
that ends on the 18th still does, in the new calendar.
*/
func (s *Service) SetPlanning(ctx context.Context, actor Actor, access Access, timeZone, policy string) (dbgen.Project, error) {
	if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
		return dbgen.Project{}, RefusalError{decision}
	}
	timeZone = strings.TrimSpace(timeZone)
	if timeZone == "" {
		timeZone = access.Project.TimeZone
	}
	zone, err := time.LoadLocation(timeZone)
	// "Local" is this process's zone, which is not a calendar a team chose.
	if err != nil || timeZone == "Local" {
		return dbgen.Project{}, ErrTimeZone
	}
	if policy == "" {
		policy = access.Project.CyclePolicy
	}
	if policy != "carry-forward" && policy != "remain" {
		return dbgen.Project{}, ErrCyclePolicy
	}

	var updated dbgen.Project
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		if err := q.LockProjectStream(ctx, access.Project.ID); err != nil {
			return fmt.Errorf("lock project: %w", err)
		}
		var err error
		updated, err = q.SetProjectPlanning(ctx, dbgen.SetProjectPlanningParams{
			ID: access.Project.ID, TimeZone: timeZone, CyclePolicy: policy,
		})
		if err != nil {
			return fmt.Errorf("set planning: %w", err)
		}
		if timeZone != access.Project.TimeZone {
			if err := moveInstants(ctx, q, access.Project.ID, zone); err != nil {
				return err
			}
		}
		changes := map[string]any{}
		if timeZone != access.Project.TimeZone {
			changes["timeZone"] = map[string]any{"previous": access.Project.TimeZone, "new": timeZone}
		}
		if policy != access.Project.CyclePolicy {
			changes["cyclePolicy"] = map[string]any{"previous": access.Project.CyclePolicy, "new": policy}
		}
		if len(changes) == 0 {
			return nil
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectUpdated, changes)
	})
	return updated, err
}

// moveInstants recomputes every cycle's and milestone's instants in a new
// zone, keeping their days.
func moveInstants(ctx context.Context, q *dbgen.Queries, projectID uuid.UUID, zone *time.Location) error {
	cycles, err := q.ListCyclesOfProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list cycles: %w", err)
	}
	for _, cycle := range cycles {
		if err := q.SetCycleInstants(ctx, dbgen.SetCycleInstantsParams{
			ID:       cycle.ID,
			StartsAt: dayOf(cycle.StartsOn).StartIn(zone),
			EndsAt:   dayOf(cycle.EndsOn).EndIn(zone),
		}); err != nil {
			return fmt.Errorf("move cycle: %w", err)
		}
	}
	milestones, err := q.ListMilestonesOfProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list milestones: %w", err)
	}
	for _, milestone := range milestones {
		day, ok := DayOf(milestone.TargetDate)
		if !ok {
			continue
		}
		due := day.EndIn(zone)
		if err := q.SetMilestoneDue(ctx, dbgen.SetMilestoneDueParams{ID: milestone.ID, DueAt: &due}); err != nil {
			return fmt.Errorf("move milestone: %w", err)
		}
	}
	return nil
}

// Cycle is a cycle with what is in it, as of when it was read.
type Cycle struct {
	dbgen.Cycle
	// Status is "upcoming", "active" or "ended".
	Status    string
	Total     int
	Completed int
}

func statusAt(cycle dbgen.Cycle, now time.Time) string {
	switch {
	case cycle.ClosedAt != nil || !now.Before(cycle.EndsAt):
		return "ended"
	case now.Before(cycle.StartsAt):
		return "upcoming"
	}
	return "active"
}

// Cycles lists a project's cycles in order, with their counts.
func (s *Service) Cycles(ctx context.Context, access Access, now time.Time) ([]Cycle, error) {
	rows, err := s.queries.ListCyclesOfProject(ctx, access.Project.ID)
	if err != nil {
		return nil, fmt.Errorf("list cycles: %w", err)
	}
	counts, err := s.queries.CountCycleIssues(ctx, access.Project.ID)
	if err != nil {
		return nil, fmt.Errorf("count cycles: %w", err)
	}
	byCycle := map[uuid.UUID]dbgen.CountCycleIssuesRow{}
	for _, count := range counts {
		byCycle[count.CycleID] = count
	}
	cycles := make([]Cycle, 0, len(rows))
	for _, row := range rows {
		count := byCycle[row.ID]
		cycles = append(cycles, Cycle{
			Cycle: row, Status: statusAt(row, now),
			Total: int(count.Total), Completed: int(count.Completed),
		})
	}
	return cycles, nil
}

// CycleByID is one of a project's cycles, with its counts.
func (s *Service) CycleByID(ctx context.Context, access Access, id uuid.UUID, now time.Time) (Cycle, error) {
	cycles, err := s.Cycles(ctx, access, now)
	if err != nil {
		return Cycle{}, err
	}
	for _, cycle := range cycles {
		if cycle.ID == id {
			return cycle, nil
		}
	}
	return Cycle{}, ErrCycleNotFound
}

// IssuesInCycle lists what is in a cycle, newest first.
func (s *Service) IssuesInCycle(ctx context.Context, cycleID uuid.UUID) ([]dbgen.Issue, error) {
	return s.queries.ListIssuesInCycle(ctx, cycleID)
}

// NewCycle describes a cycle to make, or what to change one to.
type NewCycle struct {
	Name     string
	StartsOn string
	EndsOn   string
}

func checkCycle(input NewCycle) (string, Day, Day, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 60 {
		return "", Day{}, Day{}, ErrCycleName
	}
	starts, err := ParseDay(input.StartsOn)
	if err != nil {
		return "", Day{}, Day{}, err
	}
	ends, err := ParseDay(input.EndsOn)
	if err != nil {
		return "", Day{}, Day{}, err
	}
	if ends.before(starts) || starts.StartIn(time.UTC).AddDate(1, 0, 0).Before(ends.StartIn(time.UTC)) {
		return "", Day{}, Day{}, ErrCycleDates
	}
	return name, starts, ends, nil
}

/*
CreateCycle makes a cycle: a span of days in the project's calendar.

A project's cycles do not overlap, so "the current cycle" always means one.
They need not be back to back; a gap is a time nobody planned.
*/
func (s *Service) CreateCycle(ctx context.Context, actor Actor, access Access, input NewCycle) (dbgen.Cycle, error) {
	if decision := access.Allows(authz.PermProjectPlan); !decision.Allowed {
		return dbgen.Cycle{}, RefusalError{decision}
	}
	name, starts, ends, err := checkCycle(input)
	if err != nil {
		return dbgen.Cycle{}, err
	}
	zone := Zone(access.Project)
	var created dbgen.Cycle
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		if err := q.LockProjectStream(ctx, access.Project.ID); err != nil {
			return fmt.Errorf("lock project: %w", err)
		}
		if err := noOverlap(ctx, q, access.Project.ID, uuid.Nil(), starts.StartIn(zone), ends.EndIn(zone)); err != nil {
			return err
		}
		var err error
		created, err = q.CreateCycle(ctx, dbgen.CreateCycleParams{
			ID: uuid.New(), ProjectID: access.Project.ID, Name: name,
			StartsOn: starts.pg(), EndsOn: ends.pg(),
			StartsAt: starts.StartIn(zone), EndsAt: ends.EndIn(zone),
			CreatedBy: &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("create cycle: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectCycleAdded,
			map[string]any{
				"cycle": created.ID.String(), "name": name,
				"startsOn": starts.String(), "endsOn": ends.String(),
			})
	})
	return created, err
}

func noOverlap(ctx context.Context, q *dbgen.Queries, projectID, except uuid.UUID, starts, ends time.Time) error {
	overlapping, err := q.CountOverlappingCycles(ctx, dbgen.CountOverlappingCyclesParams{
		ProjectID: projectID, ExceptID: except, StartsAt: starts, EndsAt: ends,
	})
	if err != nil {
		return fmt.Errorf("check overlap: %w", err)
	}
	if overlapping > 0 {
		return ErrCycleOverlap
	}
	return nil
}

func cycleOf(ctx context.Context, q *dbgen.Queries, projectID, id uuid.UUID) (dbgen.Cycle, error) {
	cycle, err := q.GetCycle(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && cycle.ProjectID != projectID) {
		return dbgen.Cycle{}, ErrCycleNotFound
	}
	if err != nil {
		return dbgen.Cycle{}, fmt.Errorf("read cycle: %w", err)
	}
	return cycle, nil
}

// UpdateCycle renames a cycle or changes its days. One that has ended and
// been dealt with keeps its days: what happened at its end has happened.
func (s *Service) UpdateCycle(ctx context.Context, actor Actor, access Access, id uuid.UUID, input NewCycle) (dbgen.Cycle, error) {
	if decision := access.Allows(authz.PermProjectPlan); !decision.Allowed {
		return dbgen.Cycle{}, RefusalError{decision}
	}
	name, starts, ends, err := checkCycle(input)
	if err != nil {
		return dbgen.Cycle{}, err
	}
	zone := Zone(access.Project)
	var updated dbgen.Cycle
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		if err := q.LockProjectStream(ctx, access.Project.ID); err != nil {
			return fmt.Errorf("lock project: %w", err)
		}
		current, err := cycleOf(ctx, q, access.Project.ID, id)
		if err != nil {
			return err
		}
		if current.ClosedAt != nil && (starts != dayOf(current.StartsOn) || ends != dayOf(current.EndsOn)) {
			return ErrCycleDates
		}
		if err := noOverlap(ctx, q, access.Project.ID, id, starts.StartIn(zone), ends.EndIn(zone)); err != nil {
			return err
		}
		updated, err = q.UpdateCycle(ctx, dbgen.UpdateCycleParams{
			ID: id, Name: name, StartsOn: starts.pg(), EndsOn: ends.pg(),
			StartsAt: starts.StartIn(zone), EndsAt: ends.EndIn(zone),
		})
		if err != nil {
			return fmt.Errorf("update cycle: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectCycleChanged,
			map[string]any{
				"cycle": id.String(), "name": name,
				"startsOn": starts.String(), "endsOn": ends.String(),
				"previousName":     current.Name,
				"previousStartsOn": dayOf(current.StartsOn).String(),
				"previousEndsOn":   dayOf(current.EndsOn).String(),
			})
	})
	return updated, err
}

/*
DeleteCycle removes a cycle nothing is in.

Removing one with issues in it would take them out silently — no event on any
of them saying so — which is exactly what cycles promise never to do. They are
moved out first, each with its own record, and then the cycle can go.
*/
func (s *Service) DeleteCycle(ctx context.Context, actor Actor, access Access, id uuid.UUID) error {
	if decision := access.Allows(authz.PermProjectPlan); !decision.Allowed {
		return RefusalError{decision}
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		if err := q.LockProjectStream(ctx, access.Project.ID); err != nil {
			return fmt.Errorf("lock project: %w", err)
		}
		cycle, err := cycleOf(ctx, q, access.Project.ID, id)
		if err != nil {
			return err
		}
		inside, err := q.ListIssuesInCycle(ctx, id)
		if err != nil {
			return fmt.Errorf("list cycle: %w", err)
		}
		if len(inside) > 0 {
			return ErrCycleInUse
		}
		if err := q.DeleteCycle(ctx, id); err != nil {
			return fmt.Errorf("delete cycle: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectCycleRemoved,
			map[string]any{"cycle": id.String(), "name": cycle.Name})
	})
}

/*
SetIssueCycle puts an issue into a cycle, or with nil takes it out of the one
it is in. Reports whether anything changed.
*/
func (s *Service) SetIssueCycle(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, cycleID *uuid.UUID) (bool, error) {
	if decision := access.AllowsOn(authz.PermIssuePlan, issue); !decision.Allowed {
		return false, RefusalError{decision}
	}
	changed := false
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		locked, err := q.LockIssue(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("lock issue: %w", err)
		}
		var next *dbgen.Cycle
		if cycleID != nil {
			cycle, err := cycleOf(ctx, q, access.Project.ID, *cycleID)
			if err != nil {
				return err
			}
			next = &cycle
		}
		changed, err = s.moveToCycle(ctx, q, &actor, access.Project, locked, next, "planned")
		return err
	})
	return changed, err
}

/*
moveToCycle changes the cycle an issue is in and records it, inside the
caller's transaction. A nil actor is the server, ending a cycle by itself.
*/
func (s *Service) moveToCycle(
	ctx context.Context,
	q *dbgen.Queries,
	actor *Actor,
	project dbgen.Project,
	issue dbgen.Issue,
	next *dbgen.Cycle,
	reason string,
) (bool, error) {
	var previous *dbgen.Cycle
	current, err := q.GetIssueCycle(ctx, issue.ID)
	switch {
	case err == nil:
		previous = &current
	case !errors.Is(err, pgx.ErrNoRows):
		return false, fmt.Errorf("read issue cycle: %w", err)
	}
	if (previous == nil && next == nil) || (previous != nil && next != nil && previous.ID == next.ID) {
		return false, nil
	}

	if next == nil {
		err = q.ClearIssueCycle(ctx, issue.ID)
	} else {
		err = q.SetIssueCycle(ctx, dbgen.SetIssueCycleParams{IssueID: issue.ID, CycleID: next.ID})
	}
	if err != nil {
		return false, fmt.Errorf("set issue cycle: %w", err)
	}

	describe := func(cycle *dbgen.Cycle) (any, string) {
		if cycle == nil {
			return nil, ""
		}
		return cycle.ID.String(), cycle.Name
	}
	previousID, previousName := describe(previous)
	nextID, nextName := describe(next)
	content := issueContent(issue, project.Key, map[string]any{
		"field": "cycle", "previous": previousID, "new": nextID,
		"previousName": previousName, "newName": nextName, "reason": reason,
	})
	if actor == nil {
		stream, err := q.GetRoomByID(ctx, project.ID)
		if err != nil {
			return false, fmt.Errorf("read project stream: %w", err)
		}
		_, err = s.appendAsServer(ctx, q, stream, events.TypeIssueCycleChanged, content)
		return err == nil, err
	}
	return true, s.appendProjectEvent(ctx, q, project.ID, actor.ID, events.TypeIssueCycleChanged, content)
}

/*
CloseEndedCycles deals with every cycle that has ended and not yet been dealt
with, as of now. Returns how many it closed.

What it may do is the guarantee: for each unfinished issue in the cycle, change
which cycle it is in (carry-forward) or leave it (remain), and record that. It
never changes an issue's state, never removes an issue, and never touches a
finished one — so whichever policy a project chose, every issue is still there
afterwards, in a cycle its history explains.

Each cycle is its own transaction, locked so that two servers sharing a
database cannot both close it; a cycle somebody else is closing is skipped.
*/
func (s *Service) CloseEndedCycles(ctx context.Context, now time.Time) (int, error) {
	due, err := s.queries.ListCyclesToClose(ctx, dbgen.ListCyclesToCloseParams{Now: now, Batch: 100})
	if err != nil {
		return 0, fmt.Errorf("list ended cycles: %w", err)
	}
	closed := 0
	for _, candidate := range due {
		done, err := s.closeCycle(ctx, candidate.ID, now)
		if err != nil {
			return closed, err
		}
		if done {
			closed++
		}
	}
	return closed, nil
}

func (s *Service) closeCycle(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	// To what the database keeps, so the moment the event records is the
	// moment the row holds.
	now = now.UTC().Truncate(time.Microsecond)
	done := false
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		cycle, err := q.LockCycle(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // Closed, or being closed, by somebody else.
		}
		if err != nil {
			return fmt.Errorf("lock cycle: %w", err)
		}
		project, err := q.GetProjectByID(ctx, cycle.ProjectID)
		if err != nil {
			return fmt.Errorf("read project: %w", err)
		}
		if err := q.LockProjectStream(ctx, project.ID); err != nil {
			return fmt.Errorf("lock project: %w", err)
		}
		unfinished, err := q.ListUnfinishedIssuesInCycle(ctx, cycle.ID)
		if err != nil {
			return fmt.Errorf("list unfinished: %w", err)
		}

		policy := project.CyclePolicy
		var next *dbgen.Cycle
		if policy == "carry-forward" {
			found, err := q.NextCycleAfter(ctx, dbgen.NextCycleAfterParams{
				ProjectID: project.ID, AfterID: cycle.ID, EndedAt: cycle.EndsAt,
			})
			switch {
			case err == nil:
				next = &found
			case !errors.Is(err, pgx.ErrNoRows):
				return fmt.Errorf("find next cycle: %w", err)
			}
		}

		keys := make([]string, 0, len(unfinished))
		for _, issue := range unfinished {
			keys = append(keys, identifier(project.Key, issue.Number))
			if policy != "carry-forward" {
				continue
			}
			if _, err := s.moveToCycle(ctx, q, nil, project, issue, next, "cycle_ended"); err != nil {
				return err
			}
		}

		if err := q.CloseCycle(ctx, dbgen.CloseCycleParams{ID: cycle.ID, ClosedAt: &now, ClosedPolicy: &policy}); err != nil {
			return fmt.Errorf("close cycle: %w", err)
		}
		ended := map[string]any{
			"cycle": cycle.ID.String(), "name": cycle.Name, "policy": policy,
			"unfinished": keys, "closedAt": now.Format(time.RFC3339Nano),
		}
		if next != nil {
			ended["carriedTo"] = next.ID.String()
			ended["carriedToName"] = next.Name
		}
		stream, err := q.GetRoomByID(ctx, project.ID)
		if err != nil {
			return fmt.Errorf("read project stream: %w", err)
		}
		if _, err := s.appendAsServer(ctx, q, stream, events.TypeProjectCycleEnded, ended); err != nil {
			return err
		}
		done = true
		return nil
	})
	return done, err
}

// maintenanceInterval is how often the server looks for work of its own. A
// cycle ends at midnight in its project's calendar, and an account deleted by
// hand leaves its projects waiting for a baseline; within a minute is soon
// enough for both.
const maintenanceInterval = time.Minute

/*
MaintainPeriodically does what the server does to projects by itself, until the
context ends: closes ended cycles, and gives a baseline to every project whose
stream has stopped describing it. The first is a project's policy; the second
happens after an account is deleted or erased, which may be done by hand
against the database, so it is looked for rather than told of.
*/
func (s *Service) MaintainPeriodically(ctx context.Context) {
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		// Logged rather than fatal: the next pass tries again, and nothing
		// is lost by either being a minute late.
		if _, err := s.CloseEndedCycles(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.log.Error("closing ended cycles failed", "error", err.Error())
		}
		if _, err := s.CompleteStreams(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("recording project baselines failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Milestone is a milestone with what counts towards it, as of when it was read.
type Milestone struct {
	dbgen.Milestone
	Total     int
	Completed int
	// Overdue: its day has passed with work still unfinished. Advisory —
	// nothing about any issue changes because of it.
	Overdue bool
}

func milestoneAt(row dbgen.Milestone, total, completed int, now time.Time) Milestone {
	return Milestone{
		Milestone: row, Total: total, Completed: completed,
		Overdue: row.DueAt != nil && !now.Before(*row.DueAt) && completed < total,
	}
}

// Milestones lists a project's milestones, soonest first, with their counts.
func (s *Service) Milestones(ctx context.Context, access Access, now time.Time) ([]Milestone, error) {
	rows, err := s.queries.ListMilestonesOfProject(ctx, access.Project.ID)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	counts, err := s.queries.CountMilestoneIssues(ctx, access.Project.ID)
	if err != nil {
		return nil, fmt.Errorf("count milestones: %w", err)
	}
	byMilestone := map[uuid.UUID]dbgen.CountMilestoneIssuesRow{}
	for _, count := range counts {
		byMilestone[count.MilestoneID] = count
	}
	milestones := make([]Milestone, 0, len(rows))
	for _, row := range rows {
		count := byMilestone[row.ID]
		milestones = append(milestones, milestoneAt(row, int(count.Total), int(count.Completed), now))
	}
	return milestones, nil
}

// MilestoneByID is one of a project's milestones, with its counts.
func (s *Service) MilestoneByID(ctx context.Context, access Access, id uuid.UUID, now time.Time) (Milestone, error) {
	milestones, err := s.Milestones(ctx, access, now)
	if err != nil {
		return Milestone{}, err
	}
	for _, milestone := range milestones {
		if milestone.ID == id {
			return milestone, nil
		}
	}
	return Milestone{}, ErrMilestoneNotFound
}

// IssuesInMilestone lists what counts towards a milestone, newest first.
func (s *Service) IssuesInMilestone(ctx context.Context, milestoneID uuid.UUID) ([]dbgen.Issue, error) {
	return s.queries.ListIssuesInMilestone(ctx, milestoneID)
}

// MilestoneCycles lists the cycles a milestone's issues are in: what it spans.
func (s *Service) MilestoneCycles(ctx context.Context, milestoneID uuid.UUID) ([]dbgen.Cycle, error) {
	return s.queries.ListMilestoneCycles(ctx, milestoneID)
}

// NewMilestone describes a milestone to make, or what to change one to. An
// empty TargetDate means none.
type NewMilestone struct {
	Name        string
	Description string
	TargetDate  string
}

func checkMilestone(input NewMilestone, zone *time.Location) (string, pgtype.Date, *time.Time, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return "", pgtype.Date{}, nil, ErrMilestoneName
	}
	if strings.TrimSpace(input.TargetDate) == "" {
		return name, pgtype.Date{}, nil, nil
	}
	day, err := ParseDay(input.TargetDate)
	if err != nil {
		return "", pgtype.Date{}, nil, err
	}
	due := day.EndIn(zone)
	return name, day.pg(), &due, nil
}

// CreateMilestone makes a milestone, with or without a day to reach it by.
func (s *Service) CreateMilestone(ctx context.Context, actor Actor, access Access, input NewMilestone) (dbgen.Milestone, error) {
	if decision := access.Allows(authz.PermProjectPlan); !decision.Allowed {
		return dbgen.Milestone{}, RefusalError{decision}
	}
	name, target, due, err := checkMilestone(input, Zone(access.Project))
	if err != nil {
		return dbgen.Milestone{}, err
	}
	var created dbgen.Milestone
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		var err error
		created, err = q.CreateMilestone(ctx, dbgen.CreateMilestoneParams{
			ID: uuid.New(), ProjectID: access.Project.ID, Name: name,
			Description: strings.TrimSpace(input.Description), TargetDate: target, DueAt: due,
			CreatedBy: &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("create milestone: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectMilestoneAdded,
			map[string]any{
				"milestone": created.ID.String(), "name": name, "description": created.Description,
				"targetDate": dateText(target),
			})
	})
	return created, err
}

func dateText(value pgtype.Date) any {
	day, ok := DayOf(value)
	if !ok {
		return nil
	}
	return day.String()
}

func milestoneOf(ctx context.Context, q *dbgen.Queries, projectID, id uuid.UUID) (dbgen.Milestone, error) {
	milestone, err := q.GetMilestone(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && milestone.ProjectID != projectID) {
		return dbgen.Milestone{}, ErrMilestoneNotFound
	}
	if err != nil {
		return dbgen.Milestone{}, fmt.Errorf("read milestone: %w", err)
	}
	return milestone, nil
}

// UpdateMilestone renames a milestone, or changes its description or day.
func (s *Service) UpdateMilestone(ctx context.Context, actor Actor, access Access, id uuid.UUID, input NewMilestone) (dbgen.Milestone, error) {
	if decision := access.Allows(authz.PermProjectPlan); !decision.Allowed {
		return dbgen.Milestone{}, RefusalError{decision}
	}
	name, target, due, err := checkMilestone(input, Zone(access.Project))
	if err != nil {
		return dbgen.Milestone{}, err
	}
	var updated dbgen.Milestone
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		current, err := milestoneOf(ctx, q, access.Project.ID, id)
		if err != nil {
			return err
		}
		updated, err = q.UpdateMilestone(ctx, dbgen.UpdateMilestoneParams{
			ID: id, Name: name, Description: strings.TrimSpace(input.Description),
			TargetDate: target, DueAt: due,
		})
		if err != nil {
			return fmt.Errorf("update milestone: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectMilestoneChanged,
			map[string]any{
				"milestone": id.String(), "name": name, "description": updated.Description,
				"targetDate":   dateText(target),
				"previousName": current.Name, "previousTargetDate": dateText(current.TargetDate),
			})
	})
	return updated, err
}

// DeleteMilestone removes a milestone nothing counts towards, for the same
// reason a cycle with issues in it cannot be removed.
func (s *Service) DeleteMilestone(ctx context.Context, actor Actor, access Access, id uuid.UUID) error {
	if decision := access.Allows(authz.PermProjectPlan); !decision.Allowed {
		return RefusalError{decision}
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		milestone, err := milestoneOf(ctx, q, access.Project.ID, id)
		if err != nil {
			return err
		}
		inside, err := q.ListIssuesInMilestone(ctx, id)
		if err != nil {
			return fmt.Errorf("list milestone: %w", err)
		}
		if len(inside) > 0 {
			return ErrCycleInUse
		}
		if err := q.DeleteMilestone(ctx, id); err != nil {
			return fmt.Errorf("delete milestone: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectMilestoneRemoved,
			map[string]any{"milestone": id.String(), "name": milestone.Name})
	})
}

// SetIssueMilestone counts an issue towards a milestone, or with nil towards
// none. Reports whether anything changed.
func (s *Service) SetIssueMilestone(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, milestoneID *uuid.UUID) (bool, error) {
	if decision := access.AllowsOn(authz.PermIssuePlan, issue); !decision.Allowed {
		return false, RefusalError{decision}
	}
	changed := false
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		locked, err := q.LockIssue(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("lock issue: %w", err)
		}
		var next *dbgen.Milestone
		if milestoneID != nil {
			milestone, err := milestoneOf(ctx, q, access.Project.ID, *milestoneID)
			if err != nil {
				return err
			}
			next = &milestone
		}
		var previous *dbgen.Milestone
		current, err := q.GetIssueMilestone(ctx, locked.ID)
		switch {
		case err == nil:
			previous = &current
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("read issue milestone: %w", err)
		}
		if (previous == nil && next == nil) || (previous != nil && next != nil && previous.ID == next.ID) {
			return nil
		}
		if next == nil {
			err = q.ClearIssueMilestone(ctx, locked.ID)
		} else {
			err = q.SetIssueMilestone(ctx, dbgen.SetIssueMilestoneParams{IssueID: locked.ID, MilestoneID: next.ID})
		}
		if err != nil {
			return fmt.Errorf("set issue milestone: %w", err)
		}
		describe := func(milestone *dbgen.Milestone) (any, string) {
			if milestone == nil {
				return nil, ""
			}
			return milestone.ID.String(), milestone.Name
		}
		previousID, previousName := describe(previous)
		nextID, nextName := describe(next)
		changed = true
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueMilestoneChanged,
			issueContent(locked, access.Project.Key, map[string]any{
				"field": "milestone", "previous": previousID, "new": nextID,
				"previousName": previousName, "newName": nextName,
			}))
	})
	return changed, err
}
