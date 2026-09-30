package projects

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// NewIssue describes an issue to file.
type NewIssue struct {
	Title       string
	Description string
	// StateID is where it starts. Nil means the project's first state.
	StateID *uuid.UUID

	/*
		Origin is the message this issue was made from, if it was.

		A reference and never a copy. The issue records which message in which
		room, and what that message says is read from the room when somebody
		looks — through the room's own access rules, so the reference cannot
		become a way around them, and a message later deleted or expired is
		deleted or expired here too.
	*/
	Origin *Origin
}

// Origin names a message an issue was made from.
type Origin struct {
	RoomID  uuid.UUID
	EventID string
}

// Identifier is the name people use for an issue: WEB-12.
func Identifier(project dbgen.Project, issue dbgen.Issue) string {
	return identifier(project.Key, issue.Number)
}

/*
ParseIdentifier splits WEB-12 into its key and number.

The key cannot contain a hyphen, so the last one is the separator and anything
malformed is simply not an identifier — reported the same way as one that names
no issue, since to the person asking the two are the same.
*/
func ParseIdentifier(ref string) (string, int64, bool) {
	index := strings.LastIndex(ref, "-")
	if index <= 0 || index == len(ref)-1 {
		return "", 0, false
	}
	number, err := strconv.ParseInt(ref[index+1:], 10, 64)
	if err != nil || number <= 0 {
		return "", 0, false
	}
	key, err := NormaliseKey(ref[:index])
	if err != nil {
		return "", 0, false
	}
	return key, number, true
}

func validTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxTitle {
		return "", ErrTitle
	}
	return trimmed, nil
}

func validDescription(description string) (string, error) {
	trimmed := strings.TrimSpace(description)
	if utf8.RuneCountInString(trimmed) > maxDescription {
		return "", ErrDescription
	}
	return trimmed, nil
}

/*
CreateIssue files an issue.

Three things happen, and they happen together or not at all: the issue takes
the next number in its project, its discussion room is made — a room like any
other, which is where its comments will live — and issue.created is appended
to the project's stream. An issue whose discussion did not exist, or whose
creation the history did not record, is not a state anybody should be able to
observe.
*/
func (s *Service) CreateIssue(ctx context.Context, actor Actor, access Access, input NewIssue) (dbgen.Issue, error) {
	if decision := access.Allows(authz.PermIssueCreate); !decision.Allowed {
		return dbgen.Issue{}, RefusalError{decision}
	}
	title, err := validTitle(input.Title)
	if err != nil {
		return dbgen.Issue{}, err
	}
	description, err := validDescription(input.Description)
	if err != nil {
		return dbgen.Issue{}, err
	}

	var origin map[string]any
	if input.Origin != nil {
		if origin, err = s.checkOrigin(ctx, actor, *input.Origin); err != nil {
			return dbgen.Issue{}, err
		}
	}

	project := access.Project
	var issue dbgen.Issue
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		var state dbgen.IssueState
		if input.StateID != nil {
			if state, err = stateOf(ctx, q, project.ID, *input.StateID); err != nil {
				return err
			}
		} else {
			states, err := q.ListIssueStates(ctx, project.ID)
			if err != nil {
				return fmt.Errorf("list states: %w", err)
			}
			if len(states) == 0 {
				return ErrLastState
			}
			state = states[0]
		}

		author, err := q.GetUserByID(ctx, actor.ID)
		if err != nil {
			return fmt.Errorf("read author: %w", err)
		}

		number, err := q.AllocateIssueNumber(ctx, project.ID)
		if err != nil {
			return fmt.Errorf("allocate number: %w", err)
		}
		name := identifier(project.Key, number)

		/*
			The discussion, made first because the issue refers to it.

			Private as a room, with its visibility decided by the project: the
			rooms package asks this one who may see a discussion, so it is
			exactly as visible as the issue and never more.
		*/
		discussion, err := q.CreateRoom(ctx, dbgen.CreateRoomParams{
			ID:         uuid.New(),
			Name:       name,
			Topic:      title,
			Kind:       "issue-discussion",
			Visibility: "private",
			HomeServer: s.rooms.ServerName(),
			CreatedBy:  &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("create discussion: %w", err)
		}
		// The author takes part from the start, as a room's creator does, and
		// hears what is said about what they filed.
		if err := q.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
			RoomID: discussion.ID, UserID: actor.ID,
		}); err != nil {
			return fmt.Errorf("add author to discussion: %w", err)
		}
		if err := q.WatchDiscussion(ctx, dbgen.WatchDiscussionParams{
			UserID: actor.ID, RoomID: &discussion.ID,
		}); err != nil {
			return fmt.Errorf("watch discussion: %w", err)
		}
		if _, err := s.appendTo(ctx, q, discussion, author, events.TypeRoomCreated, map[string]any{
			"name": discussion.Name, "topic": discussion.Topic,
			"visibility": discussion.Visibility, "kind": discussion.Kind,
		}); err != nil {
			return err
		}

		issue, err = q.CreateIssue(ctx, dbgen.CreateIssueParams{
			ID:          discussion.ID,
			ProjectID:   project.ID,
			Number:      number,
			Title:       title,
			Description: description,
			StateID:     state.ID,
			AuthorID:    &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}

		content := issueContent(issue, project.Key, map[string]any{
			"title":       title,
			"description": description,
			"state":       state.ID.String(),
			"stateName":   state.Name,
		})
		for name, value := range origin {
			content[name] = value
		}

		stream, err := q.GetRoomByID(ctx, project.ID)
		if err != nil {
			return fmt.Errorf("read project stream: %w", err)
		}
		_, err = s.appendTo(ctx, q, stream, author, events.TypeIssueCreated, content)
		return err
	})
	return issue, err
}

// IssueByIdentifier finds an issue by the name people use for it, if the
// actor may see it.
func (s *Service) IssueByIdentifier(ctx context.Context, actor Actor, ref string) (Access, dbgen.Issue, error) {
	key, number, ok := ParseIdentifier(strings.TrimSpace(ref))
	if !ok {
		return Access{}, dbgen.Issue{}, ErrIssueNotFound
	}
	access, err := s.ProjectByKey(ctx, actor, key)
	if errors.Is(err, ErrProjectNotFound) {
		// The same answer as a missing issue: which part of WEB-12 was
		// wrong is itself something a private project should not disclose.
		return Access{}, dbgen.Issue{}, ErrIssueNotFound
	}
	if err != nil {
		return Access{}, dbgen.Issue{}, err
	}

	issue, err := s.queries.GetIssueByNumber(ctx, dbgen.GetIssueByNumberParams{
		ProjectID: access.Project.ID, Number: number,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, dbgen.Issue{}, ErrIssueNotFound
	}
	if err != nil {
		return Access{}, dbgen.Issue{}, fmt.Errorf("read issue: %w", err)
	}
	if issue.DeletedAt != nil {
		return Access{}, dbgen.Issue{}, ErrIssueNotFound
	}
	return access, issue, nil
}

// IssueByID is IssueByIdentifier for a caller holding the identifier.
func (s *Service) IssueByID(ctx context.Context, actor Actor, id uuid.UUID) (Access, dbgen.Issue, error) {
	issue, err := s.queries.GetIssueByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, dbgen.Issue{}, ErrIssueNotFound
	}
	if err != nil {
		return Access{}, dbgen.Issue{}, fmt.Errorf("read issue: %w", err)
	}
	access, err := s.ProjectByID(ctx, actor, issue.ProjectID)
	if errors.Is(err, ErrProjectNotFound) {
		return Access{}, dbgen.Issue{}, ErrIssueNotFound
	}
	if err != nil {
		return Access{}, dbgen.Issue{}, err
	}
	if issue.DeletedAt != nil {
		return Access{}, dbgen.Issue{}, ErrIssueNotFound
	}
	return access, issue, nil
}

/*
Transition moves an issue to another state.

expected, when given, is the state the mover saw it in. A board always sends
it: two people dragging the same card at once both reach the database, the
first one moves it, and the second is told what it became — a ConflictError carrying
the issue as it now is — rather than either move silently winning. Without it,
the move applies to whatever state the issue is in, which is what an
integration setting a state outright wants.

The row is locked for the length of the transaction, so the check against
expected and the move itself see the same value, and the event appended is
the one the move justifies.
*/
func (s *Service) Transition(
	ctx context.Context,
	actor Actor,
	access Access,
	issue dbgen.Issue,
	to uuid.UUID,
	expected *uuid.UUID,
) (dbgen.Issue, error) {
	if decision := access.AllowsOn(authz.PermIssueTransition, issue); !decision.Allowed {
		return dbgen.Issue{}, RefusalError{decision}
	}

	moved := issue
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		locked, err := q.LockIssue(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("lock issue: %w", err)
		}
		if locked.DeletedAt != nil {
			return ErrIssueNotFound
		}

		destination, err := stateOf(ctx, q, access.Project.ID, to)
		if err != nil {
			return err
		}
		if expected != nil && locked.StateID != *expected {
			return ConflictError{Issue: locked}
		}
		if locked.StateID == destination.ID {
			moved = locked
			return nil
		}

		origin, err := stateOf(ctx, q, access.Project.ID, locked.StateID)
		if err != nil {
			return err
		}
		actorRow, err := q.GetUserByID(ctx, actor.ID)
		if err != nil {
			return fmt.Errorf("read actor: %w", err)
		}
		stream, err := q.GetRoomByID(ctx, access.Project.ID)
		if err != nil {
			return fmt.Errorf("read project stream: %w", err)
		}

		moved, err = s.moveLocked(ctx, q, stream, actorRow, access.Project, locked, origin, destination)
		return err
	})
	return moved, err
}

/*
moveLocked moves an issue its caller has already locked, and records it.

The one place a state change is written. Transition calls it for a person
moving an issue, and removing a state calls it for each issue that state held,
so an issue moved because its column was removed has the same history entry as
one somebody dragged.
*/
func (s *Service) moveLocked(
	ctx context.Context,
	q *dbgen.Queries,
	stream dbgen.Room,
	actor dbgen.User,
	project dbgen.Project,
	issue dbgen.Issue,
	from, to dbgen.IssueState,
) (dbgen.Issue, error) {
	moved, err := q.TransitionIssue(ctx, dbgen.TransitionIssueParams{
		ID: issue.ID, FromState: from.ID, ToState: to.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Unreachable while the row is locked; reported as a conflict rather
		// than a failure should that ever change.
		current, readErr := q.GetIssueByID(ctx, issue.ID)
		if readErr != nil {
			return dbgen.Issue{}, fmt.Errorf("read issue: %w", readErr)
		}
		return dbgen.Issue{}, ConflictError{Issue: current}
	}
	if err != nil {
		return dbgen.Issue{}, fmt.Errorf("move issue: %w", err)
	}

	if _, err := s.appendTo(ctx, q, stream, actor, events.TypeIssueStateChanged,
		issueContent(moved, project.Key, map[string]any{
			"field":        "state",
			"previous":     from.ID.String(),
			"new":          to.ID.String(),
			"previousName": from.Name,
			"newName":      to.Name,
			"completed":    to.IsCompletion,
		})); err != nil {
		return dbgen.Issue{}, err
	}
	return moved, nil
}

// IssueChanges describes an edit to an issue's text. Nil fields are left alone.
type IssueChanges struct {
	Title       *string
	Description *string
}

// Edit changes an issue's title or description, each recorded as its own
// change with what it was before.
func (s *Service) Edit(ctx context.Context, actor Actor, access Access, issue dbgen.Issue, changes IssueChanges) (dbgen.Issue, error) {
	if decision := access.MayEdit(issue); !decision.Allowed {
		return dbgen.Issue{}, RefusalError{decision}
	}

	var title, description *string
	if changes.Title != nil {
		value, err := validTitle(*changes.Title)
		if err != nil {
			return dbgen.Issue{}, err
		}
		title = &value
	}
	if changes.Description != nil {
		value, err := validDescription(*changes.Description)
		if err != nil {
			return dbgen.Issue{}, err
		}
		description = &value
	}

	edited := issue
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		locked, err := q.LockIssue(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("lock issue: %w", err)
		}
		edited = locked

		if title != nil && *title != locked.Title {
			if edited, err = q.SetIssueTitle(ctx, dbgen.SetIssueTitleParams{ID: issue.ID, Title: *title}); err != nil {
				return fmt.Errorf("set title: %w", err)
			}
			if err := s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueTitleChanged,
				issueContent(edited, access.Project.Key, map[string]any{
					"field": "title", "previous": locked.Title, "new": *title,
				})); err != nil {
				return err
			}
		}

		if description != nil && *description != locked.Description {
			if edited, err = q.SetIssueDescription(ctx, dbgen.SetIssueDescriptionParams{
				ID: issue.ID, Description: *description,
			}); err != nil {
				return fmt.Errorf("set description: %w", err)
			}
			if err := s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueDescriptionChanged,
				issueContent(edited, access.Project.Key, map[string]any{
					"field": "description", "previous": locked.Description, "new": *description,
				})); err != nil {
				return err
			}
		}
		return nil
	})
	return edited, err
}

/*
Delete hides an issue.

Not removed: its history is events, and events are never removed, so an issue
that vanished would leave its own history describing something nobody can find.
Its number is never reused, so WEB-12 in an old commit message cannot come to
mean a different issue.
*/
func (s *Service) Delete(ctx context.Context, actor Actor, access Access, issue dbgen.Issue) error {
	if decision := access.Allows(authz.PermIssueDelete); !decision.Allowed {
		return RefusalError{decision}
	}
	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		deleted, err := q.MarkIssueDeleted(ctx, dbgen.MarkIssueDeletedParams{ID: issue.ID, DeletedBy: &actor.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlreadyDeleted
		}
		if err != nil {
			return fmt.Errorf("delete issue: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeIssueDeleted,
			issueContent(deleted, access.Project.Key, nil))
	})
}

// IssueFilter narrows a project's issue list. Every field given must match.
type IssueFilter struct {
	StateID    *uuid.UUID
	LabelID    *uuid.UUID
	PriorityID *uuid.UUID
	AssigneeID *uuid.UUID
	// ByPriority sorts most urgent first, by the project's own scale, instead
	// of newest first.
	ByPriority bool
	// Before continues below this issue number; zero is the first page.
	Before int64
	Limit  int
}

// ListIssues returns a page of a project's issues.
func (s *Service) ListIssues(ctx context.Context, access Access, filter IssueFilter) ([]dbgen.Issue, error) {
	limit := filter.Limit
	if limit <= 0 || limit > maxIssuePage {
		limit = defaultIssuePage
	}
	issues, err := s.queries.ListProjectIssues(ctx, dbgen.ListProjectIssuesParams{
		ProjectID:    access.Project.ID,
		StateID:      filter.StateID,
		LabelID:      filter.LabelID,
		PriorityID:   filter.PriorityID,
		AssigneeID:   filter.AssigneeID,
		ByPriority:   filter.ByPriority,
		BeforeNumber: filter.Before,
		PageSize:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return issues, nil
}
