package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// ErrProgressPeriod is a period that runs backwards or is too long to draw.
var ErrProgressPeriod = errors.New("a progress period runs forwards and covers at most a year")

// maxProgressDays bounds a trend: one point a day, for at most a year.
const maxProgressDays = 366

/*
ProgressScope is what progress is of: the whole project, one cycle, or one
milestone. At most one of the two is set.
*/
type ProgressScope struct {
	Cycle     *uuid.UUID
	Milestone *uuid.UUID
}

// ProgressPoint is how things stood at the end of one day in the project's
// calendar — or, for today, as they stand now.
type ProgressPoint struct {
	Day       string `json:"day"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
}

/*
Progress is how much of something is done, now and over a period.

Total and Completed are counted from the issues as they are — the same rows
every list and board reads. Trend is worked out from the project's history
alone: every event replayed in order, the count taken at the end of each day.
Neither is kept anywhere. Two independent derivations of the same number is
what makes one a check on the other: today's point of the trend is what the
history says now, and it must equal what the issues say now.
*/
type Progress struct {
	Total     int             `json:"total"`
	Completed int             `json:"completed"`
	From      string          `json:"from"`
	To        string          `json:"to"`
	Trend     []ProgressPoint `json:"trend"`
}

/*
Progress reports on a scope over the days from..to, inclusive, in the
project's calendar. A to beyond today stops at today: the future has no
history to replay.
*/
func (s *Service) Progress(ctx context.Context, access Access, scope ProgressScope, from, to Day, now time.Time) (Progress, error) {
	zone := Zone(access.Project)
	today := dayAt(now, zone)
	if today.before(to) {
		to = today
	}
	if to.before(from) || from.StartIn(time.UTC).AddDate(0, 0, maxProgressDays).Before(to.StartIn(time.UTC)) {
		return Progress{}, ErrProgressPeriod
	}

	total, completed, err := s.countNow(ctx, access, scope)
	if err != nil {
		return Progress{}, err
	}

	stream, err := s.Stream(ctx, access.Project.ID)
	if err != nil {
		return Progress{}, err
	}
	states, err := s.queries.ListIssueStates(ctx, access.Project.ID)
	if err != nil {
		return Progress{}, fmt.Errorf("list states: %w", err)
	}
	done := map[uuid.UUID]bool{}
	for _, state := range states {
		done[state.ID] = state.IsCompletion
	}

	var days []Day
	var ends []int64
	for day := from; !to.before(day); day = day.next() {
		days = append(days, day)
		end := day.EndIn(zone)
		if day == today {
			// Today is not over: its point is everything recorded so far.
			end = now.Add(time.Hour)
		}
		ends = append(ends, end.UnixMilli())
	}
	counts, err := ReplayProgress(stream, done, scope, ends)
	if err != nil {
		return Progress{}, err
	}
	trend := make([]ProgressPoint, 0, len(days))
	for i, day := range days {
		trend = append(trend, ProgressPoint{Day: day.String(), Total: counts[i].Total, Completed: counts[i].Completed})
	}
	return Progress{
		Total: total, Completed: completed,
		From: from.String(), To: to.String(), Trend: trend,
	}, nil
}

func (s *Service) countNow(ctx context.Context, access Access, scope ProgressScope) (int, int, error) {
	switch {
	case scope.Cycle != nil:
		counts, err := s.queries.CountCycleIssues(ctx, access.Project.ID)
		if err != nil {
			return 0, 0, fmt.Errorf("count cycle: %w", err)
		}
		for _, count := range counts {
			if count.CycleID == *scope.Cycle {
				return int(count.Total), int(count.Completed), nil
			}
		}
		return 0, 0, nil
	case scope.Milestone != nil:
		counts, err := s.queries.CountMilestoneIssues(ctx, access.Project.ID)
		if err != nil {
			return 0, 0, fmt.Errorf("count milestone: %w", err)
		}
		for _, count := range counts {
			if count.MilestoneID == *scope.Milestone {
				return int(count.Total), int(count.Completed), nil
			}
		}
		return 0, 0, nil
	}
	count, err := s.queries.CountProjectProgress(ctx, access.Project.ID)
	if err != nil {
		return 0, 0, fmt.Errorf("count project: %w", err)
	}
	return int(count.Total), int(count.Completed), nil
}

// dayAt is the day an instant falls on in a zone.
func dayAt(instant time.Time, zone *time.Location) Day {
	local := instant.In(zone)
	return Day{local.Year(), local.Month(), local.Day()}
}

func (d Day) next() Day {
	following := time.Date(d.Year, d.Month, d.Date+1, 0, 0, 0, 0, time.UTC)
	return Day{following.Year(), following.Month(), following.Day()}
}

// Counted is a total and how much of it is done.
type Counted struct {
	Total     int
	Completed int
}

/*
ReplayProgress counts a scope from a project's history alone, once per
boundary: everything recorded before each boundary, applied in order.

Which states mean done is the project's present answer, given as done — the
same answer the issues' own count uses, so the two can be compared exactly.
*/
func ReplayProgress(stream []dbgen.Event, done map[uuid.UUID]bool, scope ProgressScope, boundaries []int64) ([]Counted, error) {
	type replayed struct {
		exists    bool
		state     uuid.UUID
		cycle     string
		milestone string
	}
	issues := map[uuid.UUID]*replayed{}
	counts := make([]Counted, len(boundaries))

	count := func() Counted {
		var c Counted
		for _, issue := range issues {
			if !issue.exists {
				continue
			}
			if scope.Cycle != nil && issue.cycle != scope.Cycle.String() {
				continue
			}
			if scope.Milestone != nil && issue.milestone != scope.Milestone.String() {
				continue
			}
			c.Total++
			if done[issue.state] {
				c.Completed++
			}
		}
		return c
	}

	next := 0
	for _, event := range stream {
		for next < len(boundaries) && event.CreatedAt >= boundaries[next] {
			counts[next] = count()
			next++
		}
		if next == len(boundaries) {
			break
		}
		/*
			What erasure emptied says nothing any more, and is passed over: the
			issue it made counts from the baseline that followed the erasure.
			A baseline is the project as it stood, so counting starts again
			from it.
		*/
		if event.RedactedAt != nil {
			continue
		}
		if events.Type(event.Type) == events.TypeProjectBaseline {
			baseline := emptyRows()
			if err := decode(event, &baseline); err != nil {
				return nil, err
			}
			issues = map[uuid.UUID]*replayed{}
			for id, row := range baseline.Issues {
				issues[id] = &replayed{exists: !row.Deleted, state: row.State, cycle: idText(row.Cycle), milestone: idText(row.Milestone)}
			}
			continue
		}
		var content struct {
			Issue string `json:"issue"`
			State string `json:"state"`
			New   any    `json:"new"`
		}
		switch events.Type(event.Type) {
		case events.TypeIssueCreated, events.TypeIssueStateChanged, events.TypeIssueDeleted,
			events.TypeIssueCycleChanged, events.TypeIssueMilestoneChanged:
		default:
			continue
		}
		if err := json.Unmarshal(event.Content, &content); err != nil {
			return nil, fmt.Errorf("event %s: %w", event.ID, err)
		}
		id, err := uuid.Parse(content.Issue)
		if err != nil {
			return nil, fmt.Errorf("event %s names no issue: %w", event.ID, err)
		}
		issue := issues[id]
		if issue == nil {
			issue = &replayed{}
			issues[id] = issue
		}
		named := func() string {
			if content.New == nil {
				return ""
			}
			return fmt.Sprint(content.New)
		}
		switch events.Type(event.Type) {
		case events.TypeIssueCreated:
			state, err := uuid.Parse(content.State)
			if err != nil {
				return nil, fmt.Errorf("event %s names no state: %w", event.ID, err)
			}
			issue.exists, issue.state = true, state
		case events.TypeIssueStateChanged:
			state, err := uuid.Parse(named())
			if err != nil {
				return nil, fmt.Errorf("event %s names no state: %w", event.ID, err)
			}
			issue.state = state
		case events.TypeIssueDeleted:
			issue.exists = false
		case events.TypeIssueCycleChanged:
			issue.cycle = named()
		case events.TypeIssueMilestoneChanged:
			issue.milestone = named()
		}
	}
	for ; next < len(boundaries); next++ {
		counts[next] = count()
	}
	return counts, nil
}

// idText is an identifier as the events write it, or nothing for none.
func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
