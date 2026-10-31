package projects

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
A change to an issue asked for on a replica and made on the project's home.

A replica never writes its own tables, so somebody working in a project another
server is home to has each change carried there by their server, in their name,
and made there exactly as a local change would be — through the same methods,
held to their project role by RemoteAccess. What comes back is an event, which
replicates like any other.

Issues are named by number rather than by identifier, since the key a replica
knows a project by is not the key its home does.
*/

// The kinds of change a replica may ask for.
const (
	ChangeFile      = "file"
	ChangeEdit      = "edit"
	ChangeDelete    = "delete"
	ChangeMove      = "move"
	ChangeAssign    = "assign"
	ChangeUnassign  = "unassign"
	ChangeLabel     = "label"
	ChangeUnlabel   = "unlabel"
	ChangePriority  = "priority"
	ChangeRelate    = "relate"
	ChangeUnrelate  = "unrelate"
	ChangeCycle     = "cycle"
	ChangeMilestone = "milestone"
)

// ErrUnknownChange is a change this server does not make for a replica.
var ErrUnknownChange = errors.New("that is not a change this server makes for another")

// Change is one change to one issue, or a new issue.
type Change struct {
	Kind  string `json:"kind"`
	Issue int64  `json:"issue,omitempty"`
	// Filing and editing.
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	// Moving: where to, and where the person last saw it.
	State    *uuid.UUID `json:"state,omitempty"`
	Expected *uuid.UUID `json:"expected,omitempty"`
	// Assigning: the person, by address.
	Person string `json:"person,omitempty"`
	// Labelling.
	Label *uuid.UUID `json:"label,omitempty"`
	// Prioritising, and planning: an identifier, or none to clear it.
	Priority  *uuid.UUID `json:"priority,omitempty"`
	Cycle     *uuid.UUID `json:"cycle,omitempty"`
	Milestone *uuid.UUID `json:"milestone,omitempty"`
	// Relating: what the issue is to the other, and the other's number.
	Relation string `json:"relation,omitempty"`
	Other    int64  `json:"other,omitempty"`
}

/*
Apply makes a change somebody asked for through their server, and returns the
issue it was made to — the new one, for a filing.

Every change goes through the method a local request uses, so every check a
local person meets is met here too, against access RemoteAccess resolved: a
remote member is refused exactly what their role does not allow.
*/
func (s *Service) Apply(ctx context.Context, actor Actor, access Access, change Change) (dbgen.Issue, error) {
	if change.Kind == ChangeFile {
		input := NewIssue{StateID: change.State}
		if change.Title != nil {
			input.Title = *change.Title
		}
		if change.Description != nil {
			input.Description = *change.Description
		}
		return s.CreateIssue(ctx, actor, access, input)
	}

	issue, err := s.queries.GetIssueByNumber(ctx, dbgen.GetIssueByNumberParams{ProjectID: access.Project.ID, Number: change.Issue})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && issue.DeletedAt != nil) {
		return dbgen.Issue{}, ErrIssueNotFound
	}
	if err != nil {
		return dbgen.Issue{}, fmt.Errorf("read issue: %w", err)
	}
	other := identifier(access.Project.Key, change.Other)

	switch change.Kind {
	case ChangeEdit:
		return s.Edit(ctx, actor, access, issue, IssueChanges{Title: change.Title, Description: change.Description})
	case ChangeDelete:
		return issue, s.Delete(ctx, actor, access, issue)
	case ChangeMove:
		if change.State == nil {
			return dbgen.Issue{}, ErrStateNotFound
		}
		return s.Transition(ctx, actor, access, issue, *change.State, change.Expected)
	case ChangeAssign:
		_, _, err := s.Assign(ctx, actor, access, issue, change.Person)
		return issue, err
	case ChangeUnassign:
		return issue, s.Unassign(ctx, actor, access, issue, change.Person)
	case ChangeLabel, ChangeUnlabel:
		if change.Label == nil {
			return dbgen.Issue{}, ErrLabelNotFound
		}
		_, err := s.SetLabel(ctx, actor, access, issue, *change.Label, change.Kind == ChangeLabel)
		return issue, err
	case ChangePriority:
		return s.SetPriority(ctx, actor, access, issue, change.Priority)
	case ChangeRelate:
		return issue, s.Relate(ctx, actor, access, issue, change.Relation, other)
	case ChangeUnrelate:
		return issue, s.Unrelate(ctx, actor, access, issue, change.Relation, other)
	case ChangeCycle:
		_, err := s.SetIssueCycle(ctx, actor, access, issue, change.Cycle)
		return issue, err
	case ChangeMilestone:
		_, err := s.SetIssueMilestone(ctx, actor, access, issue, change.Milestone)
		return issue, err
	}
	return dbgen.Issue{}, fmt.Errorf("%w: %q", ErrUnknownChange, change.Kind)
}

// HoldsReplica reports whether a server holds an accepted replica of a project
// this server is home to: only such a server may ask for changes to it.
func (s *Service) HoldsReplica(ctx context.Context, projectID uuid.UUID, server string) (bool, error) {
	return s.queries.HoldsReplica(ctx, dbgen.HoldsReplicaParams{ProjectID: projectID, Server: server})
}
