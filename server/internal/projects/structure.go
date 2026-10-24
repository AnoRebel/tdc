package projects

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// Errors a caller is expected to handle.
var (
	ErrCannotSeeIssue  = errors.New("that person cannot see this issue, so it cannot be theirs")
	ErrLabelNotFound   = errors.New("that label is not one of this project's")
	ErrLabelName       = errors.New("a label needs a name of at most thirty characters")
	ErrLabelNameTaken  = errors.New("this project already has a label by that name")
	ErrLabelColour     = errors.New("a label's colour is written #rrggbb")
	ErrPriorityMissing = errors.New("that priority is not one of this project's")
	ErrPriorityName    = errors.New("a priority needs a name of at most twenty characters")
	ErrPriorityTaken   = errors.New("this project already has a priority by that name")
	ErrRelationKind    = errors.New("a relationship is parent, child, blocks or blocked_by")
	ErrOtherProject    = errors.New("issues can only be related within one project")
	ErrSelfRelation    = errors.New("an issue cannot be related to itself")
	ErrHasParent       = errors.New("that issue already has a parent; an issue has at most one")
	ErrNotRelated      = errors.New("those issues are not related that way")
)

/*
CycleError refuses a relationship that would make a loop.

It says which issues the loop would run through, because "that would create a
cycle" with nothing more leaves somebody hunting through a hierarchy for the
edge they did not know was there.
*/
type CycleError struct {
	Kind string
	Path []string
}

func (e CycleError) Error() string {
	noun := "block each other"
	if e.Kind == "parent" {
		noun = "be each other's ancestors"
	}
	return fmt.Sprintf("that would make %s %s in a loop: %s",
		strings.Join(e.Path[:len(e.Path)-1], ", "), noun, strings.Join(e.Path, " → "))
}

var labelColour = regexp.MustCompile(`^#[0-9a-f]{6}$`)

const (
	maxLabelName    = 30
	maxPriorityName = 20
)

// defaultPriorities are the scale a project starts with, most urgent first.
var defaultPriorities = []string{"Urgent", "High", "Medium", "Low"}

// seedPriorities gives a new project its starting scale, inside the
// transaction that creates it, each level recorded as the creator's.
func (s *Service) seedPriorities(ctx context.Context, q *dbgen.Queries, stream dbgen.Room, creator dbgen.User) error {
	for rank, name := range defaultPriorities {
		added, err := q.CreateProjectPriority(ctx, dbgen.CreateProjectPriorityParams{
			ID: uuid.New(), ProjectID: stream.ID, Name: name,
			NameFolded: strings.ToLower(name), Rank: int32(rank + 1),
		})
		if err != nil {
			return fmt.Errorf("create priority: %w", err)
		}
		if _, err := s.appendTo(ctx, q, stream, creator, events.TypeProjectPriorityAdded, priorityContent(added)); err != nil {
			return err
		}
	}
	return nil
}

func priorityContent(priority dbgen.ProjectPriority) map[string]any {
	return map[string]any{"priority": priority.ID.String(), "name": priority.Name, "rank": priority.Rank}
}

func labelContent(label dbgen.ProjectLabel) map[string]any {
	return map[string]any{"label": label.ID.String(), "name": label.Name, "colour": label.Colour}
}

/*
Assign makes somebody responsible for an issue.

They must be able to see it: an issue in a private project cannot belong to
somebody outside it, who would be told about it and then find nothing there.
They join its discussion and follow it, in the same transaction, because being
on an issue and hearing about it are one thing.

Returns the event recording it, for the caller to tell them; nil when they
were already assigned and nothing changed.
*/
func (s *Service) Assign(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, username string) (*dbgen.Event, dbgen.User, error) {
	if decision := access.AllowsOn(authz.PermIssueAssign, issue); !decision.Allowed {
		return nil, dbgen.User{}, RefusalError{decision}
	}
	person, err := s.personNamed(ctx, username)
	if err != nil {
		return nil, dbgen.User{}, err
	}
	visible, err := s.Visible(ctx, access.Project.ID, person.ID)
	if err != nil {
		return nil, dbgen.User{}, err
	}
	if !visible {
		return nil, dbgen.User{}, ErrCannotSeeIssue
	}

	var recorded *dbgen.Event
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		rows, err := q.AssignIssue(ctx, dbgen.AssignIssueParams{
			IssueID: issue.ID, UserID: person.ID, AssignedBy: &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("assign: %w", err)
		}
		if rows == 0 {
			return nil
		}

		if err := s.joinDiscussion(ctx, q, issue.ID, person); err != nil {
			return err
		}

		event, err := s.appendProjectEventReturning(ctx, q, access.Project.ID, actor.ID, events.TypeIssueAssigned,
			issueContent(issue, access.Project.Key, map[string]any{
				"assignee": s.rooms.UserAddress(person.Username),
			}))
		if err != nil {
			return err
		}
		recorded = &event
		return nil
	})
	return recorded, person, err
}

/*
joinDiscussion makes somebody a participant in an issue's discussion, inside
the caller's transaction: a member of the room, recorded as having joined it,
and following it.
*/
func (s *Service) joinDiscussion(ctx context.Context, q *dbgen.Queries, roomID uuid.UUID, person dbgen.User) error {
	member, err := q.IsRoomMember(ctx, dbgen.IsRoomMemberParams{RoomID: roomID, UserID: person.ID})
	if err != nil {
		return fmt.Errorf("check discussion membership: %w", err)
	}
	if !member {
		if err := q.AddRoomMember(ctx, dbgen.AddRoomMemberParams{RoomID: roomID, UserID: person.ID}); err != nil {
			return fmt.Errorf("join discussion: %w", err)
		}
		room, err := q.GetRoomByID(ctx, roomID)
		if err != nil {
			return fmt.Errorf("read discussion: %w", err)
		}
		if _, err := s.appendTo(ctx, q, room, person, events.TypeMemberJoined,
			map[string]any{"member": s.rooms.UserAddress(person.Username)}); err != nil {
			return err
		}
	}
	if err := q.WatchDiscussion(ctx, dbgen.WatchDiscussionParams{UserID: person.ID, RoomID: &roomID}); err != nil {
		return fmt.Errorf("watch discussion: %w", err)
	}
	return nil
}

// Unassign takes somebody off an issue, recorded with who did it and when.
func (s *Service) Unassign(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, username string) error {
	if decision := access.AllowsOn(authz.PermIssueAssign, issue); !decision.Allowed {
		return RefusalError{decision}
	}
	person, err := s.personNamed(ctx, username)
	if err != nil {
		return err
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		rows, err := q.UnassignIssue(ctx, dbgen.UnassignIssueParams{IssueID: issue.ID, UserID: person.ID})
		if err != nil {
			return fmt.Errorf("unassign: %w", err)
		}
		if rows == 0 {
			return nil
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueUnassigned,
			issueContent(issue, access.Project.Key, map[string]any{
				"assignee": s.rooms.UserAddress(person.Username),
			}))
	})
}

func (s *Service) personNamed(ctx context.Context, username string) (dbgen.User, error) {
	person, err := s.queries.GetUserByUsername(ctx, identity.NormaliseUsername(username))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, ErrNoSuchPerson
	}
	if err != nil {
		return dbgen.User{}, fmt.Errorf("read person: %w", err)
	}
	return person, nil
}

// Labels returns a project's labels, by name.
func (s *Service) Labels(ctx context.Context, projectID uuid.UUID) ([]dbgen.ProjectLabel, error) {
	labels, err := s.queries.ListProjectLabels(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	return labels, nil
}

func labelFields(name, colour string) (string, string, string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxLabelName {
		return "", "", "", ErrLabelName
	}
	colour = strings.ToLower(strings.TrimSpace(colour))
	if colour == "" {
		colour = "#6b7280"
	}
	if !labelColour.MatchString(colour) {
		return "", "", "", ErrLabelColour
	}
	return trimmed, strings.ToLower(trimmed), colour, nil
}

// AddLabel adds a label to a project's vocabulary. Labelling issues is
// issue.label; deciding which labels exist is the same permission.
func (s *Service) AddLabel(ctx context.Context, actor Actor, access Access, name, colour string) (dbgen.ProjectLabel, error) {
	if decision := access.Allows(authz.PermIssueLabel); !decision.Allowed {
		return dbgen.ProjectLabel{}, RefusalError{decision}
	}
	trimmed, folded, colour, err := labelFields(name, colour)
	if err != nil {
		return dbgen.ProjectLabel{}, err
	}
	var label dbgen.ProjectLabel
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		var err error
		label, err = q.CreateProjectLabel(ctx, dbgen.CreateProjectLabelParams{
			ID: uuid.New(), ProjectID: access.Project.ID, Name: trimmed, NameFolded: folded, Colour: colour,
		})
		if isUniqueViolation(err) {
			return ErrLabelNameTaken
		}
		if err != nil {
			return fmt.Errorf("create label: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectLabelAdded, labelContent(label))
	})
	return label, err
}

// labelOf returns a label only if it is the project's own.
func labelOf(ctx context.Context, q *dbgen.Queries, projectID, labelID uuid.UUID) (dbgen.ProjectLabel, error) {
	label, err := q.GetProjectLabel(ctx, labelID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && label.ProjectID != projectID) {
		return dbgen.ProjectLabel{}, ErrLabelNotFound
	}
	if err != nil {
		return dbgen.ProjectLabel{}, fmt.Errorf("read label: %w", err)
	}
	return label, nil
}

// UpdateLabel renames or recolours a label.
func (s *Service) UpdateLabel(
	ctx context.Context,
	actor Actor,
	access Access,
	labelID uuid.UUID,
	name, colour *string,
) (dbgen.ProjectLabel, error) {
	if decision := access.Allows(authz.PermIssueLabel); !decision.Allowed {
		return dbgen.ProjectLabel{}, RefusalError{decision}
	}
	var updated dbgen.ProjectLabel
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		label, err := labelOf(ctx, q, access.Project.ID, labelID)
		if err != nil {
			return err
		}
		newName, newColour := label.Name, label.Colour
		if name != nil {
			newName = *name
		}
		if colour != nil {
			newColour = *colour
		}
		trimmed, folded, cleanColour, err := labelFields(newName, newColour)
		if err != nil {
			return err
		}
		updated, err = q.UpdateProjectLabel(ctx, dbgen.UpdateProjectLabelParams{
			ID: label.ID, Name: trimmed, NameFolded: folded, Colour: cleanColour,
		})
		if isUniqueViolation(err) {
			return ErrLabelNameTaken
		}
		if err != nil {
			return fmt.Errorf("update label: %w", err)
		}
		if updated.Name == label.Name && updated.Colour == label.Colour {
			return nil
		}
		content := labelContent(updated)
		content["previousName"], content["previousColour"] = label.Name, label.Colour
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectLabelChanged, content)
	})
	return updated, err
}

/*
RemoveLabel removes a label from a project, and so from every issue that has
it — each removal recorded in that issue's history before the label goes, so
no issue's labels change without a record of it.
*/
func (s *Service) RemoveLabel(ctx context.Context, actor Actor, access Access, labelID uuid.UUID) error {
	if decision := access.Allows(authz.PermIssueLabel); !decision.Allowed {
		return RefusalError{decision}
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		label, err := labelOf(ctx, q, access.Project.ID, labelID)
		if err != nil {
			return err
		}
		labelled, err := q.ListIssueIDsWithLabel(ctx, label.ID)
		if err != nil {
			return fmt.Errorf("list labelled issues: %w", err)
		}
		for _, issueID := range labelled {
			issue, err := q.GetIssueByID(ctx, issueID)
			if err != nil {
				return fmt.Errorf("read issue: %w", err)
			}
			if _, err := q.UnlabelIssue(ctx, dbgen.UnlabelIssueParams{IssueID: issueID, LabelID: label.ID}); err != nil {
				return fmt.Errorf("unlabel: %w", err)
			}
			if err := s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueUnlabeled,
				issueContent(issue, access.Project.Key, map[string]any{
					"label": label.ID.String(), "labelName": label.Name,
				})); err != nil {
				return err
			}
		}
		if err := q.DeleteProjectLabel(ctx, label.ID); err != nil {
			return fmt.Errorf("delete label: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectLabelRemoved,
			map[string]any{"label": label.ID.String(), "name": label.Name})
	})
}

/*
SetLabel adds a label to an issue or takes it off, recorded either way.

Returns whether anything changed: adding a label an issue already has is not a
change, and records nothing.
*/
func (s *Service) SetLabel(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, labelID uuid.UUID, present bool) (bool, error) {
	if decision := access.AllowsOn(authz.PermIssueLabel, issue); !decision.Allowed {
		return false, RefusalError{decision}
	}
	changed := false
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		label, err := labelOf(ctx, q, access.Project.ID, labelID)
		if err != nil {
			return err
		}
		var rows int64
		eventType := events.TypeIssueLabeled
		if present {
			rows, err = q.LabelIssue(ctx, dbgen.LabelIssueParams{IssueID: issue.ID, LabelID: label.ID})
		} else {
			eventType = events.TypeIssueUnlabeled
			rows, err = q.UnlabelIssue(ctx, dbgen.UnlabelIssueParams{IssueID: issue.ID, LabelID: label.ID})
		}
		if err != nil {
			return fmt.Errorf("label: %w", err)
		}
		if rows == 0 {
			return nil
		}
		changed = true
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, eventType,
			issueContent(issue, access.Project.Key, map[string]any{
				"label": label.ID.String(), "labelName": label.Name,
			}))
	})
	return changed, err
}

// Priorities returns a project's scale, most urgent first.
func (s *Service) Priorities(ctx context.Context, projectID uuid.UUID) ([]dbgen.ProjectPriority, error) {
	priorities, err := s.queries.ListProjectPriorities(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list priorities: %w", err)
	}
	return priorities, nil
}

func priorityName(name string) (string, string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxPriorityName {
		return "", "", ErrPriorityName
	}
	return trimmed, strings.ToLower(trimmed), nil
}

func priorityOf(ctx context.Context, q *dbgen.Queries, projectID, priorityID uuid.UUID) (dbgen.ProjectPriority, error) {
	priority, err := q.GetProjectPriority(ctx, priorityID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && priority.ProjectID != projectID) {
		return dbgen.ProjectPriority{}, ErrPriorityMissing
	}
	if err != nil {
		return dbgen.ProjectPriority{}, fmt.Errorf("read priority: %w", err)
	}
	return priority, nil
}

// AddPriority adds a level at the least urgent end of a project's scale. The
// scale is the project's workflow as much as its states are.
func (s *Service) AddPriority(ctx context.Context, actor Actor, access Access, name string) (dbgen.ProjectPriority, error) {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return dbgen.ProjectPriority{}, RefusalError{decision}
	}
	trimmed, folded, err := priorityName(name)
	if err != nil {
		return dbgen.ProjectPriority{}, err
	}
	var added dbgen.ProjectPriority
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		rank, err := q.NextProjectPriorityRank(ctx, access.Project.ID)
		if err != nil {
			return fmt.Errorf("next rank: %w", err)
		}
		added, err = q.CreateProjectPriority(ctx, dbgen.CreateProjectPriorityParams{
			ID: uuid.New(), ProjectID: access.Project.ID, Name: trimmed, NameFolded: folded, Rank: rank,
		})
		if isUniqueViolation(err) {
			return ErrPriorityTaken
		}
		if err != nil {
			return fmt.Errorf("create priority: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectPriorityAdded, priorityContent(added))
	})
	return added, err
}

// RenamePriority renames a level of the scale without moving it.
func (s *Service) RenamePriority(
	ctx context.Context,
	actor Actor,
	access Access,
	priorityID uuid.UUID,
	name string,
) (dbgen.ProjectPriority, error) {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return dbgen.ProjectPriority{}, RefusalError{decision}
	}
	trimmed, folded, err := priorityName(name)
	if err != nil {
		return dbgen.ProjectPriority{}, err
	}
	var renamed dbgen.ProjectPriority
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		current, err := priorityOf(ctx, q, access.Project.ID, priorityID)
		if err != nil {
			return err
		}
		renamed, err = q.UpdateProjectPriority(ctx, dbgen.UpdateProjectPriorityParams{
			ID: priorityID, Name: trimmed, NameFolded: folded,
		})
		if isUniqueViolation(err) {
			return ErrPriorityTaken
		}
		if err != nil {
			return fmt.Errorf("rename priority: %w", err)
		}
		if renamed.Name == current.Name {
			return nil
		}
		content := priorityContent(renamed)
		content["previousName"] = current.Name
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectPriorityChanged, content)
	})
	return renamed, err
}

// ReorderPriorities sets the scale's order, most urgent first. Every level
// must be named exactly once.
func (s *Service) ReorderPriorities(ctx context.Context, actor Actor, access Access, order []uuid.UUID) ([]dbgen.ProjectPriority, error) {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return nil, RefusalError{decision}
	}
	var reordered []dbgen.ProjectPriority
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		current, err := q.ListProjectPriorities(ctx, access.Project.ID)
		if err != nil {
			return fmt.Errorf("list priorities: %w", err)
		}
		if len(order) != len(current) {
			return ErrPriorityMissing
		}
		known := map[uuid.UUID]bool{}
		for _, priority := range current {
			known[priority.ID] = true
		}
		for rank, id := range order {
			if !known[id] {
				return ErrPriorityMissing
			}
			delete(known, id)
			if err := q.SetProjectPriorityRank(ctx, dbgen.SetProjectPriorityRankParams{ID: id, Rank: int32(rank + 1)}); err != nil {
				return fmt.Errorf("set rank: %w", err)
			}
		}
		// Recorded the way reordering states is: the whole order, most
		// urgent first.
		names := make([]string, 0, len(order))
		for _, id := range order {
			names = append(names, id.String())
		}
		if err := s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID,
			events.TypeProjectPriorityChanged, map[string]any{"order": names}); err != nil {
			return err
		}
		reordered, err = q.ListProjectPriorities(ctx, access.Project.ID)
		return err
	})
	return reordered, err
}

/*
RemovePriority removes a level from the scale, clearing it from every issue that
had it first — each clearing an ordinary recorded change of that issue's
priority — so nothing changes without a record.
*/
func (s *Service) RemovePriority(ctx context.Context, actor Actor, access Access, priorityID uuid.UUID) error {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return RefusalError{decision}
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		priority, err := priorityOf(ctx, q, access.Project.ID, priorityID)
		if err != nil {
			return err
		}
		affected, err := q.ListIssueIDsWithPriority(ctx, &priority.ID)
		if err != nil {
			return fmt.Errorf("list issues with priority: %w", err)
		}
		for _, issueID := range affected {
			issue, err := q.LockIssue(ctx, issueID)
			if err != nil {
				return fmt.Errorf("lock issue: %w", err)
			}
			if err := s.setPriorityLocked(ctx, q, actor, access, issue, nil); err != nil {
				return err
			}
		}
		if err := q.DeleteProjectPriority(ctx, priority.ID); err != nil {
			return fmt.Errorf("delete priority: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectPriorityRemoved,
			map[string]any{"priority": priority.ID.String(), "name": priority.Name})
	})
}

// SetPriority sets or clears an issue's priority. Priority is part of how an
// issue is classified, so it is governed by issue.label.
func (s *Service) SetPriority(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, priorityID *uuid.UUID) (dbgen.Issue, error) {
	if decision := access.AllowsOn(authz.PermIssueLabel, issue); !decision.Allowed {
		return dbgen.Issue{}, RefusalError{decision}
	}
	updated := issue
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		locked, err := q.LockIssue(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("lock issue: %w", err)
		}
		if err := s.setPriorityLocked(ctx, q, actor, access, locked, priorityID); err != nil {
			return err
		}
		updated, err = q.GetIssueByID(ctx, issue.ID)
		return err
	})
	return updated, err
}

func (s *Service) setPriorityLocked(
	ctx context.Context,
	q *dbgen.Queries,
	actor Actor,
	access Access,
	issue dbgen.Issue,
	priorityID *uuid.UUID,
) error {
	if sameID(issue.PriorityID, priorityID) {
		return nil
	}
	nameOf := func(id *uuid.UUID) (any, string, error) {
		if id == nil {
			return nil, "", nil
		}
		priority, err := priorityOf(ctx, q, access.Project.ID, *id)
		if err != nil {
			return nil, "", err
		}
		return priority.ID.String(), priority.Name, nil
	}
	previous, previousName, err := nameOf(issue.PriorityID)
	if err != nil {
		return err
	}
	next, nextName, err := nameOf(priorityID)
	if err != nil {
		return err
	}
	updated, err := q.SetIssuePriority(ctx, dbgen.SetIssuePriorityParams{ID: issue.ID, PriorityID: priorityID})
	if err != nil {
		return fmt.Errorf("set priority: %w", err)
	}
	return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssuePriorityChanged,
		issueContent(updated, access.Project.Key, map[string]any{
			"field": "priority", "previous": previous, "new": next,
			"previousName": previousName, "newName": nextName,
		}))
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

/*
edgeOf turns a relationship named from the subject's side into the one stored
edge, and says what the subject and the other issue each are in it.

"This blocks that" and "that is blocked by this" are one fact, stored once as
blocker → blocked; "this is the parent of that" and "that is a child of this"
are stored as parent → child.
*/
func edgeOf(kind string, subject, other uuid.UUID) (from, to uuid.UUID, stored, subjectIs, otherIs string, err error) {
	switch kind {
	case "parent":
		return subject, other, "parent", "parent", "child", nil
	case "child":
		return other, subject, "parent", "child", "parent", nil
	case "blocks":
		return subject, other, "blocks", "blocks", "blocked_by", nil
	case "blocked_by":
		return other, subject, "blocks", "blocked_by", "blocks", nil
	}
	return uuid.Nil, uuid.Nil, "", "", "", ErrRelationKind
}

/*
Relate records a relationship between two issues of one project, refusing one
that would make a loop.

The loop check walks the project's existing edges of that kind in memory: a
project holds hundreds of issues, not millions, and a plain search from the new
edge's head looking for its tail is simple enough to be obviously correct. It
runs inside the transaction, after the project's stream has been locked by the
first append, so two people adding the two halves of a loop at once cannot both
pass it.
*/
func (s *Service) Relate(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, kind, otherRef string) error {
	if decision := access.AllowsOn(authz.PermIssueRelate, issue); !decision.Allowed {
		return RefusalError{decision}
	}
	other, err := s.relatable(ctx, actor, access, issue, otherRef)
	if err != nil {
		return err
	}
	from, to, stored, subjectIs, otherIs, err := edgeOf(kind, issue.ID, other.ID)
	if err != nil {
		return err
	}

	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		/*
			Serialised on the project's stream before reading the graph.

			The stream's sequence row is locked until this transaction ends, so
			a second relationship being added to the same project waits here
			and then reads a graph that already includes this one.
		*/
		if err := q.LockProjectStream(ctx, access.Project.ID); err != nil {
			return fmt.Errorf("lock project stream: %w", err)
		}
		edges, err := q.ListProjectRelations(ctx, dbgen.ListProjectRelationsParams{
			ProjectID: access.Project.ID, Kind: stored,
		})
		if err != nil {
			return fmt.Errorf("read relationships: %w", err)
		}
		if path := pathBetween(edges, to, from); path != nil {
			keys, err := s.keysOf(ctx, q, access.Project, append(path, to))
			if err != nil {
				return err
			}
			return CycleError{Kind: stored, Path: keys}
		}

		rows, err := q.RelateIssues(ctx, dbgen.RelateIssuesParams{
			FromIssue: from, ToIssue: to, Kind: stored, CreatedBy: &actor.ID,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return ErrHasParent
			}
			return fmt.Errorf("relate: %w", err)
		}
		if rows == 0 {
			return nil
		}

		// One event on each issue, each from its own side, so both histories
		// say what happened in their own terms.
		if err := s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueRelated,
			issueContent(issue, access.Project.Key, map[string]any{
				"kind": subjectIs, "other": other.ID.String(), "otherKey": Identifier(access.Project, other),
			})); err != nil {
			return err
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueRelated,
			issueContent(other, access.Project.Key, map[string]any{
				"kind": otherIs, "other": issue.ID.String(), "otherKey": Identifier(access.Project, issue),
			}))
	})
}

// Unrelate removes a relationship, recorded on both issues.
func (s *Service) Unrelate(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, kind, otherRef string) error {
	if decision := access.AllowsOn(authz.PermIssueRelate, issue); !decision.Allowed {
		return RefusalError{decision}
	}
	other, err := s.relatable(ctx, actor, access, issue, otherRef)
	if err != nil {
		return err
	}
	from, to, stored, subjectIs, otherIs, err := edgeOf(kind, issue.ID, other.ID)
	if err != nil {
		return err
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		rows, err := q.UnrelateIssues(ctx, dbgen.UnrelateIssuesParams{FromIssue: from, ToIssue: to, Kind: stored})
		if err != nil {
			return fmt.Errorf("unrelate: %w", err)
		}
		if rows == 0 {
			return ErrNotRelated
		}
		if err := s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueUnrelated,
			issueContent(issue, access.Project.Key, map[string]any{
				"kind": subjectIs, "other": other.ID.String(), "otherKey": Identifier(access.Project, other),
			})); err != nil {
			return err
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueUnrelated,
			issueContent(other, access.Project.Key, map[string]any{
				"kind": otherIs, "other": issue.ID.String(), "otherKey": Identifier(access.Project, issue),
			}))
	})
}

// relatable finds the other issue of a relationship, which must be a different
// issue in the same project.
func (s *Service) relatable(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, otherRef string) (dbgen.Issue, error) {
	otherAccess, other, err := s.IssueByIdentifier(ctx, actor, otherRef)
	if err != nil {
		return dbgen.Issue{}, err
	}
	if otherAccess.Project.ID != access.Project.ID {
		return dbgen.Issue{}, ErrOtherProject
	}
	if other.ID == issue.ID {
		return dbgen.Issue{}, ErrSelfRelation
	}
	return other, nil
}

/*
pathBetween finds a path from start to goal along the edges, or nil.

A depth-first search with a visited set, so a graph that already contains a
cycle — which this refuses to create, but a hand-edited database might hold —
still terminates.
*/
func pathBetween(edges []dbgen.ListProjectRelationsRow, start, goal uuid.UUID) []uuid.UUID {
	next := map[uuid.UUID][]uuid.UUID{}
	for _, edge := range edges {
		next[edge.FromIssue] = append(next[edge.FromIssue], edge.ToIssue)
	}
	visited := map[uuid.UUID]bool{}
	var walk func(at uuid.UUID, path []uuid.UUID) []uuid.UUID
	walk = func(at uuid.UUID, path []uuid.UUID) []uuid.UUID {
		path = append(path, at)
		if at == goal {
			return path
		}
		if visited[at] {
			return nil
		}
		visited[at] = true
		for _, onward := range next[at] {
			if found := walk(onward, path); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(start, nil)
}

// keysOf names issues by their identifiers, for saying which ones a loop
// would run through.
func (s *Service) keysOf(ctx context.Context, q *dbgen.Queries, project dbgen.Project, ids []uuid.UUID) ([]string, error) {
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		issue, err := q.GetIssueByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read issue: %w", err)
		}
		keys = append(keys, Identifier(project, issue))
	}
	return keys, nil
}

// Relations returns an issue's relationships in both directions.
func (s *Service) Relations(ctx context.Context, issue dbgen.Issue) ([]dbgen.ListRelationsOfIssueRow, error) {
	relations, err := s.queries.ListRelationsOfIssue(ctx, issue.ID)
	if err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}
	return relations, nil
}

// MyIssues lists what is assigned to somebody, in every project they may see.
func (s *Service) MyIssues(ctx context.Context, userID uuid.UUID, limit int) ([]dbgen.ListMyIssuesRow, error) {
	if limit <= 0 || limit > maxIssuePage {
		limit = maxIssuePage
	}
	issues, err := s.queries.ListMyIssues(ctx, dbgen.ListMyIssuesParams{UserID: userID, PageSize: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("list my issues: %w", err)
	}
	return issues, nil
}
