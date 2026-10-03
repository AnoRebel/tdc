package projects

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Errors a caller is expected to handle.
var (
	ErrBulkChange  = errors.New("that is not a change that can be made to many issues at once")
	ErrBulkTooMany = errors.New("a change can be made to at most two hundred issues at once")
	ErrBulkEmpty   = errors.New("name at least one issue")
)

// maxBulk bounds one batch, so one request cannot hold the server for long.
const maxBulk = 200

/*
BulkChange is one change to make to many issues.

Kind is "state", "assign", "unassign", "label", "unlabel" or "priority", and
Value is what the change names: a state, a label or a priority level
by identifier, a person by username. An empty Value clears a
priority.
*/
type BulkChange struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

/*
BulkResult is what happened to one issue in a batch.

Every issue named gets one, in the order they were named, and whether it
changed is said plainly: Changed is false both for an issue refused and for
one that already was as asked, and Code tells the two apart.
*/
type BulkResult struct {
	Issue string `json:"issue"`
	// Code is "changed", "unchanged", "refused", "not_found" or "failed".
	Code    string `json:"code"`
	Changed bool   `json:"changed"`
	// Reason says why, when it did not change.
	Reason string `json:"reason,omitempty"`
	// Permission names what would have allowed it, for a refusal.
	Permission string `json:"permission,omitempty"`
}

/*
Bulk makes one change to many issues of one project, each on its own.

Not one transaction for the batch, deliberately. Each issue goes through the
same function a change to it alone would — its own permission check, its own
transaction, its own event in its own history — so an issue the actor may not
touch is refused without stopping the rest, and every issue that changed has
the record a single change would have left. One transaction would make "some
of these succeeded" impossible to say, which is the thing a batch most needs
to be able to say.

The change itself is checked once, first: a state or label that is not this
project's would be refused for every issue, and that is a mistake in the
request rather than forty-nine refusals.
*/
func (s *Service) Bulk(ctx context.Context, actor Actor, access Access, refs []string, change BulkChange) ([]BulkResult, error) {
	if len(refs) == 0 {
		return nil, ErrBulkEmpty
	}
	if len(refs) > maxBulk {
		return nil, ErrBulkTooMany
	}
	apply, err := s.bulkChange(ctx, actor, access, change)
	if err != nil {
		return nil, err
	}

	results := make([]BulkResult, 0, len(refs))
	for _, ref := range refs {
		results = append(results, s.bulkOne(ctx, actor, access, strings.ToUpper(strings.TrimSpace(ref)), apply))
	}
	return results, nil
}

// applyFunc makes the batch's change to one issue, reporting whether it changed.
type applyFunc func(ctx context.Context, access Access, issue issueRef) (bool, error)

// issueRef is an issue as a batch item knows it.
type issueRef struct {
	key   string
	issue dbgen.Issue
}

func (s *Service) bulkOne(ctx context.Context, actor Actor, access Access, ref string, apply applyFunc) BulkResult {
	result := BulkResult{Issue: ref}
	issueAccess, issue, err := s.IssueByIdentifier(ctx, actor, ref)
	if err != nil {
		result.Code, result.Reason = "not_found", ErrIssueNotFound.Error()
		return result
	}
	if issueAccess.Project.ID != access.Project.ID {
		/*
			Reported as missing, like an issue that does not exist: the batch
			is this project's, and one in a project the actor cannot see must
			not be told apart from one that is simply elsewhere.
		*/
		result.Code, result.Reason = "not_found", "not an issue in this project"
		return result
	}

	changed, err := apply(ctx, access, issueRef{key: ref, issue: issue})
	var refusal RefusalError
	switch {
	case errors.As(err, &refusal):
		result.Code, result.Reason = "refused", refusal.Error()
		result.Permission = string(refusal.Decision.Permission)
	case err != nil:
		result.Code, result.Reason = "failed", err.Error()
	case changed:
		result.Code, result.Changed = "changed", true
	default:
		result.Code, result.Reason = "unchanged", "it already was"
	}
	return result
}

// bulkChange checks a change once and returns how to make it to one issue.
func (s *Service) bulkChange(ctx context.Context, actor Actor, access Access, change BulkChange) (applyFunc, error) {
	value := strings.TrimSpace(change.Value)
	parseID := func(missing error) (uuid.UUID, error) {
		id, err := uuid.Parse(value)
		if err != nil {
			return uuid.Nil, missing
		}
		return id, nil
	}

	switch change.Kind {
	case "state":
		id, err := parseID(ErrStateNotFound)
		if err != nil {
			return nil, err
		}
		if _, err := stateOf(ctx, s.queries, access.Project.ID, id); err != nil {
			return nil, err
		}
		return func(ctx context.Context, access Access, ref issueRef) (bool, error) {
			moved, err := s.Transition(ctx, actor, access, ref.issue, id, nil)
			return err == nil && ref.issue.StateID != moved.StateID, err
		}, nil

	case "label", "unlabel":
		id, err := parseID(ErrLabelNotFound)
		if err != nil {
			return nil, err
		}
		if _, err := labelOf(ctx, s.queries, access.Project.ID, id); err != nil {
			return nil, err
		}
		present := change.Kind == "label"
		return func(ctx context.Context, access Access, ref issueRef) (bool, error) {
			return s.SetLabel(ctx, actor, access, ref.issue, id, present)
		}, nil

	case "assign":
		if _, err := s.personNamed(ctx, value); err != nil {
			return nil, err
		}
		return func(ctx context.Context, access Access, ref issueRef) (bool, error) {
			event, _, err := s.Assign(ctx, actor, access, ref.issue, value)
			return event != nil, err
		}, nil

	case "unassign":
		if _, err := s.personNamed(ctx, value); err != nil {
			return nil, err
		}
		return func(ctx context.Context, access Access, ref issueRef) (bool, error) {
			before, err := s.queries.ListIssueAssignees(ctx, ref.issue.ID)
			if err != nil {
				return false, err
			}
			if err := s.Unassign(ctx, actor, access, ref.issue, value); err != nil {
				return false, err
			}
			after, err := s.queries.ListIssueAssignees(ctx, ref.issue.ID)
			return len(after) != len(before), err
		}, nil

	case "priority":
		var priority *uuid.UUID
		if value != "" {
			id, err := parseID(ErrPriorityMissing)
			if err != nil {
				return nil, err
			}
			if _, err := priorityOf(ctx, s.queries, access.Project.ID, id); err != nil {
				return nil, err
			}
			priority = &id
		}
		return func(ctx context.Context, access Access, ref issueRef) (bool, error) {
			updated, err := s.SetPriority(ctx, actor, access, ref.issue, priority)
			return err == nil && !sameID(ref.issue.PriorityID, updated.PriorityID), err
		}, nil

	}
	return nil, ErrBulkChange
}
