package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// States returns a project's states in board order.
func (s *Service) States(ctx context.Context, projectID uuid.UUID) ([]dbgen.IssueState, error) {
	states, err := s.queries.ListIssueStates(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list states: %w", err)
	}
	return states, nil
}

// stateOf returns a state only if it belongs to the project. A state from
// another project is reported as missing, exactly like one that never
// existed: moving an issue there is no more possible than moving it nowhere.
func stateOf(ctx context.Context, q *dbgen.Queries, projectID, stateID uuid.UUID) (dbgen.IssueState, error) {
	state, err := q.GetIssueState(ctx, stateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.IssueState{}, ErrStateNotFound
	}
	if err != nil {
		return dbgen.IssueState{}, fmt.Errorf("read state: %w", err)
	}
	if state.ProjectID != projectID {
		return dbgen.IssueState{}, ErrStateNotFound
	}
	return state, nil
}

// AddState adds a state at the end of a project's workflow.
func (s *Service) AddState(ctx context.Context, actor Actor, access Access, input NewState) (dbgen.IssueState, error) {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return dbgen.IssueState{}, RefusalError{decision}
	}
	folded, err := stateName(input.Name)
	if err != nil {
		return dbgen.IssueState{}, err
	}

	var added dbgen.IssueState
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		position, err := q.NextIssueStatePosition(ctx, access.Project.ID)
		if err != nil {
			return fmt.Errorf("next position: %w", err)
		}
		added, err = q.CreateIssueState(ctx, dbgen.CreateIssueStateParams{
			ID:           uuid.New(),
			ProjectID:    access.Project.ID,
			Name:         strings.TrimSpace(input.Name),
			NameFolded:   folded,
			Position:     position,
			IsCompletion: input.IsCompletion,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return ErrStateNameTaken
			}
			return fmt.Errorf("create state: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor,
			events.TypeProjectStateAdded, stateContent(added))
	})
	return added, err
}

// StateChanges describes a change to one state. Nil fields are left alone.
type StateChanges struct {
	Name         *string
	IsCompletion *bool
}

// UpdateState renames a state or changes whether it means finished.
func (s *Service) UpdateState(
	ctx context.Context,
	actor Actor,
	access Access,
	stateID uuid.UUID,
	changes StateChanges,
) (dbgen.IssueState, error) {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return dbgen.IssueState{}, RefusalError{decision}
	}

	var updated dbgen.IssueState
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		state, err := stateOf(ctx, q, access.Project.ID, stateID)
		if err != nil {
			return err
		}

		name, completion := state.Name, state.IsCompletion
		folded := state.NameFolded
		if changes.Name != nil {
			if folded, err = stateName(*changes.Name); err != nil {
				return err
			}
			name = strings.TrimSpace(*changes.Name)
		}
		if changes.IsCompletion != nil {
			completion = *changes.IsCompletion
		}

		updated, err = q.UpdateIssueState(ctx, dbgen.UpdateIssueStateParams{
			ID: state.ID, Name: name, NameFolded: folded, IsCompletion: completion,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return ErrStateNameTaken
			}
			return fmt.Errorf("update state: %w", err)
		}

		content := stateContent(updated)
		content["previous"] = stateContent(state)
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor, events.TypeProjectStateChanged, content)
	})
	return updated, err
}

/*
ReorderStates sets the order of a project's states, left to right on a board.

The list must name every state exactly once. A partial order would leave the
unnamed ones wherever their old positions happened to fall among the new ones,
which is an order nobody chose.
*/
func (s *Service) ReorderStates(ctx context.Context, actor Actor, access Access, order []uuid.UUID) ([]dbgen.IssueState, error) {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return nil, RefusalError{decision}
	}

	var reordered []dbgen.IssueState
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		current, err := q.ListIssueStates(ctx, access.Project.ID)
		if err != nil {
			return fmt.Errorf("list states: %w", err)
		}
		if len(order) != len(current) {
			return ErrStateNotFound
		}
		known := make(map[uuid.UUID]bool, len(current))
		for _, state := range current {
			known[state.ID] = true
		}
		for position, id := range order {
			if !known[id] {
				return ErrStateNotFound
			}
			delete(known, id)
			if err := q.SetIssueStatePosition(ctx, dbgen.SetIssueStatePositionParams{
				ID: id, Position: int32(position + 1),
			}); err != nil {
				return fmt.Errorf("set position: %w", err)
			}
		}

		names := make([]string, 0, len(order))
		for _, id := range order {
			names = append(names, id.String())
		}
		if err := s.appendProjectEvent(ctx, q, access.Project.ID, actor,
			events.TypeProjectStateChanged, map[string]any{"order": names}); err != nil {
			return err
		}

		reordered, err = q.ListIssueStates(ctx, access.Project.ID)
		if err != nil {
			return fmt.Errorf("list states: %w", err)
		}
		return nil
	})
	return reordered, err
}

/*
RemoveState removes a state from a project's workflow.

A state issues are still in is removed only if the caller names where they go,
and then every one of them is moved there first — each move an ordinary
transition, recorded in that issue's own history like any other. Only then is
the state deleted, in the same transaction, so there is no moment at which an
issue names a state that has gone; the database's own foreign key would refuse
it if there were.
*/
func (s *Service) RemoveState(
	ctx context.Context,
	actor Actor,
	access Access,
	stateID uuid.UUID,
	moveTo *uuid.UUID,
) error {
	if decision := access.Allows(authz.PermProjectConfigureStates); !decision.Allowed {
		return RefusalError{decision}
	}

	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		state, err := stateOf(ctx, q, access.Project.ID, stateID)
		if err != nil {
			return err
		}

		all, err := q.ListIssueStates(ctx, access.Project.ID)
		if err != nil {
			return fmt.Errorf("list states: %w", err)
		}
		if len(all) <= 1 {
			return ErrLastState
		}

		inState, err := q.ListIssueIDsInState(ctx, state.ID)
		if err != nil {
			return fmt.Errorf("list issues in state: %w", err)
		}

		content := stateContent(state)
		if len(inState) > 0 {
			if moveTo == nil {
				return ErrStateInUse
			}
			destination, err := stateOf(ctx, q, access.Project.ID, *moveTo)
			if err != nil {
				return err
			}
			if destination.ID == state.ID {
				return ErrStateInUse
			}

			stream, err := q.GetRoomByID(ctx, access.Project.ID)
			if err != nil {
				return fmt.Errorf("read project stream: %w", err)
			}

			/*
				Every issue locked before any is moved.

				A move takes the project stream's position lock, and somebody
				moving one of these issues at the same moment holds its row and
				waits for that same lock. Locking one issue, moving it, then
				locking the next would be waiting for a row while holding what
				its holder waits for — a deadlock Postgres would break by
				killing one of the two. Taking every row first, in number order,
				means this waits for them before it holds anything they need.
			*/
			locked := make([]dbgen.Issue, 0, len(inState))
			for _, issueID := range inState {
				issue, err := q.LockIssue(ctx, issueID)
				if err != nil {
					return fmt.Errorf("lock issue: %w", err)
				}
				locked = append(locked, issue)
			}
			for _, issue := range locked {
				if issue.StateID != state.ID {
					// Moved out by somebody else while this waited for it.
					continue
				}
				if _, err := s.moveLocked(ctx, q, stream, actor, access.Project, issue, state, destination); err != nil {
					return err
				}
			}
			content["movedTo"] = destination.ID.String()
			content["moved"] = len(inState)
		}

		if err := q.DeleteIssueState(ctx, state.ID); err != nil {
			// An issue filed into this state after the list above was read:
			// the foreign key refuses, and the caller is told why.
			if strings.Contains(err.Error(), "SQLSTATE 23503") {
				return ErrStateInUse
			}
			return fmt.Errorf("delete state: %w", err)
		}
		return s.appendProjectEvent(ctx, q, access.Project.ID, actor, events.TypeProjectStateRemoved, content)
	})
}
