package projects

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
The projector: a project's rows worked out from its stream alone.

On this server the rows are written by the code that appends each event, and
the projector is not used to write anything. It exists so that the claim the
package makes — the stream is the truth, the tables its cache — can be checked,
and so that a server holding only a copy of a project's stream can one day
rebuild the project from it: the same function, applied to the same events in
the same order, arrives at the same rows wherever it runs.

Rows are the columns the stream decides. When a row was written — created_at,
joined_at and the like — is not among them: that is when this database wrote
it, which a copy rebuilt elsewhere would answer differently and correctly.
People are named by address, as the stream names them, never by account.
*/

// ErrUnknownEvent is an event the projector has no rule for. A stream holding
// one stops being applied rather than skipping it, because a projection that
// skipped what it did not understand would silently disagree with its source.
var ErrUnknownEvent = errors.New("no projection for this kind of event")

// Rows is one project as its tables hold it.
type Rows struct {
	Project    ProjectRow                 `json:"project"`
	Members    map[string]MemberRow       `json:"members,omitempty"`
	States     map[uuid.UUID]StateRow     `json:"states,omitempty"`
	Labels     map[uuid.UUID]LabelRow     `json:"labels,omitempty"`
	Priorities map[uuid.UUID]PriorityRow  `json:"priorities,omitempty"`
	Cycles     map[uuid.UUID]CycleRow     `json:"cycles,omitempty"`
	Milestones map[uuid.UUID]MilestoneRow `json:"milestones,omitempty"`
	Issues     map[uuid.UUID]IssueRow     `json:"issues,omitempty"`
	// Relations by "from kind to", the edge as stored.
	Relations map[string]RelationRow `json:"relations,omitempty"`
}

// ProjectRow is the project's own row.
type ProjectRow struct {
	ID              uuid.UUID `json:"id"`
	Key             string    `json:"key"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Visibility      string    `json:"visibility"`
	TimeZone        string    `json:"timeZone"`
	CyclePolicy     string    `json:"cyclePolicy"`
	NextIssueNumber int64     `json:"nextIssueNumber"`
	CreatedBy       string    `json:"createdBy,omitempty"`
	// Federated is whether the project may be shared with other servers.
	Federated bool `json:"federated,omitempty"`
}

// MemberRow is somebody's place in the project, keyed by their address.
type MemberRow struct {
	Role    string `json:"role"`
	AddedBy string `json:"addedBy,omitempty"`
}

// StateRow is one column of the board.
type StateRow struct {
	Name         string `json:"name"`
	Position     int32  `json:"position"`
	IsCompletion bool   `json:"isCompletion"`
}

// LabelRow is one label of the project's vocabulary.
type LabelRow struct {
	Name   string `json:"name"`
	Colour string `json:"colour"`
}

// PriorityRow is one level of the project's scale.
type PriorityRow struct {
	Name string `json:"name"`
	Rank int32  `json:"rank"`
}

// CycleRow is a cycle: days chosen, instants worked out in the project's zone.
type CycleRow struct {
	Name         string     `json:"name"`
	StartsOn     string     `json:"startsOn"`
	EndsOn       string     `json:"endsOn"`
	StartsAt     time.Time  `json:"startsAt"`
	EndsAt       time.Time  `json:"endsAt"`
	ClosedAt     *time.Time `json:"closedAt,omitempty"`
	ClosedPolicy string     `json:"closedPolicy,omitempty"`
	CreatedBy    string     `json:"createdBy,omitempty"`
}

// MilestoneRow is a milestone, its due instant worked out like a cycle's.
type MilestoneRow struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	TargetDate  string     `json:"targetDate,omitempty"`
	DueAt       *time.Time `json:"dueAt,omitempty"`
	CreatedBy   string     `json:"createdBy,omitempty"`
}

// IssueRow is an issue with everything that hangs from it.
type IssueRow struct {
	Number      int64              `json:"number"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	State       uuid.UUID          `json:"state"`
	Author      string             `json:"author,omitempty"`
	Priority    *uuid.UUID         `json:"priority,omitempty"`
	Cycle       *uuid.UUID         `json:"cycle,omitempty"`
	Milestone   *uuid.UUID         `json:"milestone,omitempty"`
	Deleted     bool               `json:"deleted,omitempty"`
	DeletedBy   string             `json:"deletedBy,omitempty"`
	Labels      map[uuid.UUID]bool `json:"labels,omitempty"`
	// Assignees by address, each with who assigned them.
	Assignees map[string]string `json:"assignees,omitempty"`
}

// RelationRow is one stored edge between two issues.
type RelationRow struct {
	From      uuid.UUID `json:"from"`
	To        uuid.UUID `json:"to"`
	Kind      string    `json:"kind"`
	CreatedBy string    `json:"createdBy,omitempty"`
}

func relationKey(from, to uuid.UUID, kind string) string {
	return from.String() + " " + kind + " " + to.String()
}

// emptyRows is a project nothing has happened to yet.
func emptyRows() Rows {
	return Rows{
		Members:    map[string]MemberRow{},
		States:     map[uuid.UUID]StateRow{},
		Labels:     map[uuid.UUID]LabelRow{},
		Priorities: map[uuid.UUID]PriorityRow{},
		Cycles:     map[uuid.UUID]CycleRow{},
		Milestones: map[uuid.UUID]MilestoneRow{},
		Issues:     map[uuid.UUID]IssueRow{},
		Relations:  map[string]RelationRow{},
	}
}

/*
RowsFromStream works out a project's rows from its stream, in order.

From the latest baseline, if there is one: a baseline is everything the
project held when it was appended, so what came before it is already in it —
and is, for a project older than its complete history, the part of the stream
that left things out.
*/
func RowsFromStream(stream []dbgen.Event) (Rows, error) {
	start := 0
	for i, event := range stream {
		if events.Type(event.Type) == events.TypeProjectBaseline {
			start = i
		}
	}
	rows := emptyRows()
	for _, event := range stream[start:] {
		if err := Apply(&rows, event); err != nil {
			return Rows{}, err
		}
	}
	return rows, nil
}

/*
Apply changes rows by one event, in place.

A function of nothing but its arguments: it reads no database and no clock, so
the same stream applied anywhere gives the same rows. Each rule does what the
code that appended the event did to the tables, including what the database
did by itself — a cycle removed takes its issues out of it, as the foreign
key's cascade does.
*/
func Apply(rows *Rows, event dbgen.Event) error {
	if rows.Issues == nil {
		*rows = emptyRows()
	}
	project, known := projections[events.Type(event.Type)]
	if !known {
		return fmt.Errorf("event %s, %s: %w", event.ID, event.Type, ErrUnknownEvent)
	}
	if err := project(rows, event); err != nil {
		return fmt.Errorf("event %s, %s: %w", event.ID, event.Type, err)
	}
	return nil
}

// person is who an event names as its sender, or nobody when the server acted
// by itself.
func person(event dbgen.Event) string {
	if !strings.Contains(event.SenderAddress, "@") {
		return ""
	}
	return event.SenderAddress
}

func decode(event dbgen.Event, into any) error {
	if err := json.Unmarshal(event.Content, into); err != nil {
		return fmt.Errorf("content: %w", err)
	}
	return nil
}

func parseID(text string) (uuid.UUID, error) {
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil(), fmt.Errorf("names no identifier: %q", text)
	}
	return id, nil
}

// optionalID is a "new" that is either an identifier or null.
func optionalID(text *string) (*uuid.UUID, error) {
	if text == nil || *text == "" {
		return nil, nil
	}
	id, err := parseID(*text)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func zoneNamed(name string) *time.Location {
	zone, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return zone
}

// cycleInstants are when a cycle's days begin and end in a zone, as the
// tables hold them.
func cycleInstants(cycle *CycleRow, zone *time.Location) error {
	starts, err := ParseDay(cycle.StartsOn)
	if err != nil {
		return err
	}
	ends, err := ParseDay(cycle.EndsOn)
	if err != nil {
		return err
	}
	cycle.StartsAt, cycle.EndsAt = starts.StartIn(zone).UTC(), ends.EndIn(zone).UTC()
	return nil
}

func milestoneDue(milestone *MilestoneRow, zone *time.Location) error {
	milestone.DueAt = nil
	if milestone.TargetDate == "" {
		return nil
	}
	day, err := ParseDay(milestone.TargetDate)
	if err != nil {
		return err
	}
	due := day.EndIn(zone).UTC()
	milestone.DueAt = &due
	return nil
}

// issueOf is the issue an issue event is about, which must already exist.
func issueOf(rows *Rows, event dbgen.Event) (uuid.UUID, IssueRow, error) {
	var content struct {
		Issue string `json:"issue"`
	}
	if err := decode(event, &content); err != nil {
		return uuid.Nil(), IssueRow{}, err
	}
	id, err := parseID(content.Issue)
	if err != nil {
		return uuid.Nil(), IssueRow{}, err
	}
	issue, ok := rows.Issues[id]
	if !ok {
		return uuid.Nil(), IssueRow{}, fmt.Errorf("issue %s was never created", id)
	}
	return id, issue, nil
}

// onIssue is a rule for an event about one issue, given the issue to change.
func onIssue(change func(rows *Rows, event dbgen.Event, issue *IssueRow) error) func(*Rows, dbgen.Event) error {
	return func(rows *Rows, event dbgen.Event) error {
		id, issue, err := issueOf(rows, event)
		if err != nil {
			return err
		}
		if err := change(rows, event, &issue); err != nil {
			return err
		}
		rows.Issues[id] = issue
		return nil
	}
}

// fieldChange is the "new" of an event recording one field's change.
type fieldChange struct {
	New *string `json:"new"`
}

// projections is the rule for every kind of event a project's stream holds.
var projections = map[events.Type]func(*Rows, dbgen.Event) error{
	events.TypeProjectCreated: func(rows *Rows, event dbgen.Event) error {
		var content struct {
			Key, Name, Description, Visibility, TimeZone, CyclePolicy string
		}
		if err := decode(event, &content); err != nil {
			return err
		}
		/*
			A project made before creation recorded its calendar was made with
			the column defaults, and a baseline follows it in the stream.
		*/
		if content.TimeZone == "" {
			content.TimeZone = "UTC"
		}
		if content.CyclePolicy == "" {
			content.CyclePolicy = "carry-forward"
		}
		creator := person(event)
		rows.Project = ProjectRow{
			ID: event.RoomID, Key: content.Key, Name: content.Name, Description: content.Description,
			Visibility: content.Visibility, TimeZone: content.TimeZone, CyclePolicy: content.CyclePolicy,
			NextIssueNumber: 1, CreatedBy: creator,
		}
		rows.Members[creator] = MemberRow{Role: "admin", AddedBy: creator}
		return nil
	},

	events.TypeProjectUpdated: func(rows *Rows, event dbgen.Event) error {
		var content map[string]struct {
			New string `json:"new"`
		}
		if err := decode(event, &content); err != nil {
			return err
		}
		for field, change := range content {
			switch field {
			case "name":
				rows.Project.Name = change.New
			case "description":
				rows.Project.Description = change.New
			case "visibility":
				rows.Project.Visibility = change.New
			case "cyclePolicy":
				rows.Project.CyclePolicy = change.New
			case "timeZone":
				// The same days, at the instants they fall on in the new zone.
				rows.Project.TimeZone = change.New
				zone := zoneNamed(change.New)
				for id, cycle := range rows.Cycles {
					if err := cycleInstants(&cycle, zone); err != nil {
						return err
					}
					rows.Cycles[id] = cycle
				}
				for id, milestone := range rows.Milestones {
					if err := milestoneDue(&milestone, zone); err != nil {
						return err
					}
					rows.Milestones[id] = milestone
				}
			default:
				return fmt.Errorf("changes an unknown field %q", field)
			}
		}
		return nil
	},

	events.TypeProjectMemberAdded: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Member, Role, Previous string }
		if err := decode(event, &content); err != nil {
			return err
		}
		member, existed := rows.Members[content.Member]
		if !existed {
			member.AddedBy = person(event)
		}
		// A change of role keeps who first added them, as the row does.
		member.Role = content.Role
		rows.Members[content.Member] = member
		return nil
	},

	events.TypeProjectFederationChanged: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Federated bool }
		if err := decode(event, &content); err != nil {
			return err
		}
		rows.Project.Federated = content.Federated
		return nil
	},

	events.TypeProjectMemberRemoved: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Member string }
		if err := decode(event, &content); err != nil {
			return err
		}
		delete(rows.Members, content.Member)
		return nil
	},

	events.TypeProjectStateAdded: func(rows *Rows, event dbgen.Event) error {
		var content struct {
			State        string
			Name         string
			Position     int32
			IsCompletion bool
		}
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.State)
		if err != nil {
			return err
		}
		rows.States[id] = StateRow{Name: content.Name, Position: content.Position, IsCompletion: content.IsCompletion}
		return nil
	},

	// A renamed state, or the whole order of them.
	events.TypeProjectStateChanged: func(rows *Rows, event dbgen.Event) error {
		var content struct {
			State        string
			Name         string
			IsCompletion bool
			Order        []string
		}
		if err := decode(event, &content); err != nil {
			return err
		}
		if content.Order != nil {
			for position, text := range content.Order {
				id, err := parseID(text)
				if err != nil {
					return err
				}
				state := rows.States[id]
				state.Position = int32(position + 1)
				rows.States[id] = state
			}
			return nil
		}
		id, err := parseID(content.State)
		if err != nil {
			return err
		}
		state := rows.States[id]
		state.Name, state.IsCompletion = content.Name, content.IsCompletion
		rows.States[id] = state
		return nil
	},

	// Its issues were moved out first, each by an event of its own.
	events.TypeProjectStateRemoved: func(rows *Rows, event dbgen.Event) error {
		var content struct{ State string }
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.State)
		if err != nil {
			return err
		}
		delete(rows.States, id)
		return nil
	},

	events.TypeProjectLabelAdded:   setLabel,
	events.TypeProjectLabelChanged: setLabel,
	// Off every issue first, each by an event of its own; the cascade here
	// is what the database's would do for anything those missed.
	events.TypeProjectLabelRemoved: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Label string }
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Label)
		if err != nil {
			return err
		}
		delete(rows.Labels, id)
		for _, issue := range rows.Issues {
			delete(issue.Labels, id)
		}
		return nil
	},

	events.TypeProjectPriorityAdded: func(rows *Rows, event dbgen.Event) error {
		var content struct {
			Priority string
			Name     string
			Rank     int32
		}
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Priority)
		if err != nil {
			return err
		}
		rows.Priorities[id] = PriorityRow{Name: content.Name, Rank: content.Rank}
		return nil
	},

	// A renamed level, or the whole scale's order, most urgent first.
	events.TypeProjectPriorityChanged: func(rows *Rows, event dbgen.Event) error {
		var content struct {
			Priority string
			Name     string
			Order    []string
		}
		if err := decode(event, &content); err != nil {
			return err
		}
		if content.Order != nil {
			for rank, text := range content.Order {
				id, err := parseID(text)
				if err != nil {
					return err
				}
				priority := rows.Priorities[id]
				priority.Rank = int32(rank + 1)
				rows.Priorities[id] = priority
			}
			return nil
		}
		id, err := parseID(content.Priority)
		if err != nil {
			return err
		}
		priority := rows.Priorities[id]
		priority.Name = content.Name
		rows.Priorities[id] = priority
		return nil
	},

	// Cleared from every issue first, each by an event of its own.
	events.TypeProjectPriorityRemoved: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Priority string }
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Priority)
		if err != nil {
			return err
		}
		delete(rows.Priorities, id)
		return nil
	},

	events.TypeProjectCycleAdded:   setCycle,
	events.TypeProjectCycleChanged: setCycle,
	events.TypeProjectCycleRemoved: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Cycle string }
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Cycle)
		if err != nil {
			return err
		}
		delete(rows.Cycles, id)
		// Only deleted issues can still be in it, and the cascade takes them out.
		for issueID, issue := range rows.Issues {
			if issue.Cycle != nil && *issue.Cycle == id {
				issue.Cycle = nil
				rows.Issues[issueID] = issue
			}
		}
		return nil
	},

	// Closed, by the server. What became of its unfinished issues was
	// recorded before this, one event each.
	events.TypeProjectCycleEnded: func(rows *Rows, event dbgen.Event) error {
		var content struct {
			Cycle    string
			Policy   string
			ClosedAt *time.Time
		}
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Cycle)
		if err != nil {
			return err
		}
		cycle, ok := rows.Cycles[id]
		if !ok {
			return fmt.Errorf("cycle %s was never added", id)
		}
		closed := time.UnixMilli(event.CreatedAt).UTC()
		if content.ClosedAt != nil {
			closed = content.ClosedAt.UTC()
		}
		cycle.ClosedAt, cycle.ClosedPolicy = &closed, content.Policy
		rows.Cycles[id] = cycle
		return nil
	},

	events.TypeProjectMilestoneAdded:   setMilestone,
	events.TypeProjectMilestoneChanged: setMilestone,
	events.TypeProjectMilestoneRemoved: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Milestone string }
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Milestone)
		if err != nil {
			return err
		}
		delete(rows.Milestones, id)
		for issueID, issue := range rows.Issues {
			if issue.Milestone != nil && *issue.Milestone == id {
				issue.Milestone = nil
				rows.Issues[issueID] = issue
			}
		}
		return nil
	},

	// Everything, as it stood: what came before is in it.
	events.TypeProjectBaseline: func(rows *Rows, event dbgen.Event) error {
		baseline := emptyRows()
		if err := decode(event, &baseline); err != nil {
			return err
		}
		for id, issue := range baseline.Issues {
			if issue.Labels == nil {
				issue.Labels = map[uuid.UUID]bool{}
			}
			if issue.Assignees == nil {
				issue.Assignees = map[string]string{}
			}
			baseline.Issues[id] = issue
		}
		*rows = baseline
		return nil
	},

	events.TypeIssueCreated: func(rows *Rows, event dbgen.Event) error {
		var content struct{ Issue, Key, Title, Description, State string }
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Issue)
		if err != nil {
			return err
		}
		state, err := parseID(content.State)
		if err != nil {
			return err
		}
		_, number, ok := ParseIdentifier(content.Key)
		if !ok {
			return fmt.Errorf("names no issue number: %q", content.Key)
		}
		rows.Issues[id] = IssueRow{
			Number: number, Title: content.Title, Description: content.Description,
			State: state, Author: person(event),
			Labels: map[uuid.UUID]bool{}, Assignees: map[string]string{},
		}
		if number >= rows.Project.NextIssueNumber {
			rows.Project.NextIssueNumber = number + 1
		}
		return nil
	},

	events.TypeIssueStateChanged: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content fieldChange
		if err := decode(event, &content); err != nil {
			return err
		}
		if content.New == nil {
			return errors.New("moves to no state")
		}
		state, err := parseID(*content.New)
		issue.State = state
		return err
	}),

	events.TypeIssueTitleChanged: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content fieldChange
		if err := decode(event, &content); err != nil {
			return err
		}
		if content.New != nil {
			issue.Title = *content.New
		}
		return nil
	}),

	events.TypeIssueDescriptionChanged: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content fieldChange
		if err := decode(event, &content); err != nil {
			return err
		}
		if content.New != nil {
			issue.Description = *content.New
		}
		return nil
	}),

	events.TypeIssueDeleted: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		issue.Deleted, issue.DeletedBy = true, person(event)
		return nil
	}),

	events.TypeIssueAssigned: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content struct{ Assignee string }
		if err := decode(event, &content); err != nil {
			return err
		}
		issue.Assignees[content.Assignee] = person(event)
		return nil
	}),

	events.TypeIssueUnassigned: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content struct{ Assignee string }
		if err := decode(event, &content); err != nil {
			return err
		}
		delete(issue.Assignees, content.Assignee)
		return nil
	}),

	events.TypeIssueLabeled:   labelIssue(true),
	events.TypeIssueUnlabeled: labelIssue(false),

	events.TypeIssuePriorityChanged: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content fieldChange
		if err := decode(event, &content); err != nil {
			return err
		}
		priority, err := optionalID(content.New)
		issue.Priority = priority
		return err
	}),

	events.TypeIssueRelated:   relate(true),
	events.TypeIssueUnrelated: relate(false),

	events.TypeIssueMilestoneChanged: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content fieldChange
		if err := decode(event, &content); err != nil {
			return err
		}
		milestone, err := optionalID(content.New)
		issue.Milestone = milestone
		return err
	}),

	events.TypeIssueCycleChanged: onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content fieldChange
		if err := decode(event, &content); err != nil {
			return err
		}
		cycle, err := optionalID(content.New)
		issue.Cycle = cycle
		return err
	}),
}

func setLabel(rows *Rows, event dbgen.Event) error {
	var content struct{ Label, Name, Colour string }
	if err := decode(event, &content); err != nil {
		return err
	}
	id, err := parseID(content.Label)
	if err != nil {
		return err
	}
	rows.Labels[id] = LabelRow{Name: content.Name, Colour: content.Colour}
	return nil
}

func setCycle(rows *Rows, event dbgen.Event) error {
	var content struct{ Cycle, Name, StartsOn, EndsOn string }
	if err := decode(event, &content); err != nil {
		return err
	}
	id, err := parseID(content.Cycle)
	if err != nil {
		return err
	}
	cycle, existed := rows.Cycles[id]
	if !existed {
		cycle.CreatedBy = person(event)
	}
	cycle.Name, cycle.StartsOn, cycle.EndsOn = content.Name, content.StartsOn, content.EndsOn
	if err := cycleInstants(&cycle, zoneNamed(rows.Project.TimeZone)); err != nil {
		return err
	}
	rows.Cycles[id] = cycle
	return nil
}

func setMilestone(rows *Rows, event dbgen.Event) error {
	var content struct {
		Milestone, Name, Description string
		TargetDate                   *string
	}
	if err := decode(event, &content); err != nil {
		return err
	}
	id, err := parseID(content.Milestone)
	if err != nil {
		return err
	}
	milestone, existed := rows.Milestones[id]
	if !existed {
		milestone.CreatedBy = person(event)
	}
	milestone.Name, milestone.Description, milestone.TargetDate = content.Name, content.Description, ""
	if content.TargetDate != nil {
		milestone.TargetDate = *content.TargetDate
	}
	if err := milestoneDue(&milestone, zoneNamed(rows.Project.TimeZone)); err != nil {
		return err
	}
	rows.Milestones[id] = milestone
	return nil
}

func labelIssue(present bool) func(*Rows, dbgen.Event) error {
	return onIssue(func(_ *Rows, event dbgen.Event, issue *IssueRow) error {
		var content struct{ Label string }
		if err := decode(event, &content); err != nil {
			return err
		}
		id, err := parseID(content.Label)
		if err != nil {
			return err
		}
		if present {
			issue.Labels[id] = true
		} else {
			delete(issue.Labels, id)
		}
		return nil
	})
}

/*
relate applies one side's account of a relationship. Each relationship is
recorded twice, once from each issue, and both name the same stored edge — so
applying the second is applying nothing new.
*/
func relate(present bool) func(*Rows, dbgen.Event) error {
	return onIssue(func(rows *Rows, event dbgen.Event, _ *IssueRow) error {
		var content struct{ Issue, Kind, Other string }
		if err := decode(event, &content); err != nil {
			return err
		}
		subject, err := parseID(content.Issue)
		if err != nil {
			return err
		}
		other, err := parseID(content.Other)
		if err != nil {
			return err
		}
		from, to, stored, _, _, err := edgeOf(content.Kind, subject, other)
		if err != nil {
			return err
		}
		key := relationKey(from, to, stored)
		if !present {
			delete(rows.Relations, key)
			return nil
		}
		if _, exists := rows.Relations[key]; !exists {
			rows.Relations[key] = RelationRow{From: from, To: to, Kind: stored, CreatedBy: person(event)}
		}
		return nil
	})
}

/*
CurrentRows reads a project's rows as its tables hold them now, in the shape
the projector works out — so the two can be compared, and so a project's rows
can be recorded in its stream as a baseline.
*/
func (s *Service) CurrentRows(ctx context.Context, projectID uuid.UUID) (Rows, error) {
	return s.rowsOf(ctx, s.queries, projectID)
}

func (s *Service) rowsOf(ctx context.Context, q *dbgen.Queries, projectID uuid.UUID) (Rows, error) {
	address := func(username string) string {
		if username == "" {
			return ""
		}
		return s.rooms.UserAddress(username)
	}
	rows := emptyRows()

	project, err := q.GetProjectForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read project: %w", err)
	}
	rows.Project = ProjectRow{
		ID: project.ID, Key: project.Key, Name: project.Name, Description: project.Description,
		Visibility: project.Visibility, TimeZone: project.TimeZone, CyclePolicy: project.CyclePolicy,
		NextIssueNumber: project.NextIssueNumber, CreatedBy: address(project.CreatedByUsername),
		Federated: project.Federated,
	}

	members, err := q.ListProjectMembersForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read members: %w", err)
	}
	for _, member := range members {
		rows.Members[address(member.Username)] = MemberRow{Role: member.Role, AddedBy: address(member.AddedByUsername)}
	}
	remote, err := q.ListProjectRemoteMembersForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read remote members: %w", err)
	}
	for _, member := range remote {
		rows.Members[member.Address] = MemberRow{Role: member.Role, AddedBy: address(member.AddedByUsername)}
	}

	states, err := q.ListIssueStates(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read states: %w", err)
	}
	for _, state := range states {
		rows.States[state.ID] = StateRow{Name: state.Name, Position: state.Position, IsCompletion: state.IsCompletion}
	}

	labels, err := q.ListProjectLabels(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read labels: %w", err)
	}
	for _, label := range labels {
		rows.Labels[label.ID] = LabelRow{Name: label.Name, Colour: label.Colour}
	}

	priorities, err := q.ListProjectPriorities(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read priorities: %w", err)
	}
	for _, priority := range priorities {
		rows.Priorities[priority.ID] = PriorityRow{Name: priority.Name, Rank: priority.Rank}
	}

	cycles, err := q.ListCyclesForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read cycles: %w", err)
	}
	for _, cycle := range cycles {
		row := CycleRow{
			Name: cycle.Name, StartsOn: dayOf(cycle.StartsOn).String(), EndsOn: dayOf(cycle.EndsOn).String(),
			StartsAt: cycle.StartsAt.UTC(), EndsAt: cycle.EndsAt.UTC(), CreatedBy: address(cycle.CreatedByUsername),
		}
		if cycle.ClosedAt != nil {
			closed := cycle.ClosedAt.UTC()
			row.ClosedAt = &closed
		}
		if cycle.ClosedPolicy != nil {
			row.ClosedPolicy = *cycle.ClosedPolicy
		}
		rows.Cycles[cycle.ID] = row
	}

	milestones, err := q.ListMilestonesForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read milestones: %w", err)
	}
	for _, milestone := range milestones {
		row := MilestoneRow{
			Name: milestone.Name, Description: milestone.Description, CreatedBy: address(milestone.CreatedByUsername),
		}
		if day, ok := DayOf(milestone.TargetDate); ok {
			row.TargetDate = day.String()
		}
		if milestone.DueAt != nil {
			due := milestone.DueAt.UTC()
			row.DueAt = &due
		}
		rows.Milestones[milestone.ID] = row
	}

	issues, err := q.ListIssuesForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read issues: %w", err)
	}
	for _, issue := range issues {
		rows.Issues[issue.ID] = IssueRow{
			Number: issue.Number, Title: issue.Title, Description: issue.Description, State: issue.StateID,
			Author: cmp.Or(address(issue.AuthorUsername), issue.AuthorAddress), Priority: issue.PriorityID,
			Deleted: issue.Deleted, DeletedBy: address(issue.DeletedByUsername),
			Labels: map[uuid.UUID]bool{}, Assignees: map[string]string{},
		}
	}
	assignees, err := q.ListIssueAssigneesForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read assignees: %w", err)
	}
	for _, assignee := range assignees {
		rows.Issues[assignee.IssueID].Assignees[address(assignee.Username)] = address(assignee.AssignedByUsername)
	}
	remoteAssignees, err := q.ListIssueRemoteAssigneesForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read remote assignees: %w", err)
	}
	for _, assignee := range remoteAssignees {
		rows.Issues[assignee.IssueID].Assignees[assignee.Address] = ""
	}
	labelled, err := q.ListIssueLabelsForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read issue labels: %w", err)
	}
	for _, label := range labelled {
		rows.Issues[label.IssueID].Labels[label.LabelID] = true
	}
	inCycles, err := q.ListIssueCyclesForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read issue cycles: %w", err)
	}
	for _, row := range inCycles {
		issue := rows.Issues[row.IssueID]
		issue.Cycle = &row.CycleID
		rows.Issues[row.IssueID] = issue
	}
	inMilestones, err := q.ListIssueMilestonesForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read issue milestones: %w", err)
	}
	for _, row := range inMilestones {
		issue := rows.Issues[row.IssueID]
		issue.Milestone = &row.MilestoneID
		rows.Issues[row.IssueID] = issue
	}

	relations, err := q.ListIssueRelationsForStream(ctx, projectID)
	if err != nil {
		return Rows{}, fmt.Errorf("read relationships: %w", err)
	}
	for _, relation := range relations {
		rows.Relations[relationKey(relation.FromIssue, relation.ToIssue, relation.Kind)] = RelationRow{
			From: relation.FromIssue, To: relation.ToIssue, Kind: relation.Kind,
			CreatedBy: address(relation.CreatedByUsername),
		}
	}
	return rows, nil
}

/*
CompleteStreams gives every project whose history predates its being complete
a baseline: its rows as they stand, appended by the server. Returns how many it
gave one.

Each in its own transaction, holding the project's stream while its rows are
read, so a change being made at the same moment lands either in the baseline
or after it in the stream, never in neither. Marking the project first means
two servers sharing a database cannot both append one.
*/
func (s *Service) CompleteStreams(ctx context.Context) (int, error) {
	incomplete, err := s.queries.ListProjectsWithIncompleteStreams(ctx)
	if err != nil {
		return 0, fmt.Errorf("list incomplete streams: %w", err)
	}
	completed := 0
	for _, projectID := range incomplete {
		err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
			if err := q.LockProjectStream(ctx, projectID); err != nil {
				return fmt.Errorf("lock project stream: %w", err)
			}
			marked, err := q.MarkProjectStreamComplete(ctx, projectID)
			if err != nil {
				return fmt.Errorf("mark stream complete: %w", err)
			}
			if marked == 0 {
				return nil
			}
			rows, err := s.rowsOf(ctx, q, projectID)
			if err != nil {
				return err
			}
			stream, err := q.GetRoomByID(ctx, projectID)
			if err != nil {
				return fmt.Errorf("read project stream: %w", err)
			}
			if _, err := s.appendAsServer(ctx, q, stream, events.TypeProjectBaseline, rows); err != nil {
				return err
			}
			completed++
			return nil
		})
		if err != nil {
			return completed, err
		}
	}
	return completed, nil
}
