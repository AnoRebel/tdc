package authz

import (
	"context"
	"fmt"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
ScopeProject applies to one project only, the way ScopeChannel applies to one
channel.

A project-scope grant is stored exactly as a channel-scope one is — a role
permission with the project's identifier as its scope — and resolved into the
same per-scope table. The two cannot collide, because a project's identifier is
the identifier of its own event stream and no channel shares it. What differs
is only what may be granted there, which is what GrantableAt enforces.
*/
const ScopeProject Scope = "project"

// Projects and the issues in them.
const (
	// PermProjectCreate is starting a project. Server scope only: a project
	// that does not exist yet has no scope to be granted in.
	PermProjectCreate Permission = "project.create"
	// PermProjectAdminister is renaming a project, changing its visibility,
	// and deciding who is in it.
	PermProjectAdminister Permission = "project.administer"
	// PermProjectConfigureStates is adding, renaming, reordering and removing
	// the states issues move between — the project's workflow.
	PermProjectConfigureStates Permission = "project.configure_states"
	// PermProjectPlan is making, changing and removing a project's cycles and
	// milestones.
	PermProjectPlan Permission = "project.plan"

	PermIssueCreate     Permission = "issue.create"
	PermIssueTransition Permission = "issue.transition"
	PermIssueAssign     Permission = "issue.assign"
	PermIssueLabel      Permission = "issue.label"
	PermIssueRelate     Permission = "issue.relate"
	PermIssueDelete     Permission = "issue.delete"
	// PermIssuePlan is putting an issue into a cycle or a milestone, or
	// taking it out of one.
	PermIssuePlan Permission = "issue.plan"

	/*
		There is deliberately no issue.comment.

		Commenting on an issue is posting in its discussion, which is a room,
		and posting in a room is already governed — by membership and by
		channel.post, the same as anywhere else. A second permission for the
		same act would be a second answer to one question.
	*/
)

func init() {
	for _, definition := range []Definition{
		{
			Name:        PermProjectCreate,
			Description: "Start projects",
			Scopes:      []Scope{ScopeServer},
		},
		{
			Name:        PermProjectAdminister,
			Description: "Rename a project, change who can see it, and decide who is in it",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermProjectConfigureStates,
			Description: "Change the states a project's issues move between",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermIssueCreate,
			Description: "File issues in a project",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermIssueTransition,
			Description: "Move issues between states",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermIssueAssign,
			Description: "Assign issues to people",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermIssueLabel,
			Description: "Label issues and set their priority",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermIssueRelate,
			Description: "Relate issues to one another: parent and child, blocking",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermIssueDelete,
			Description: "Delete issues",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermProjectPlan,
			Description: "Make and change a project's cycles and milestones",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
		{
			Name:        PermIssuePlan,
			Description: "Put issues into cycles and milestones",
			Scopes:      []Scope{ScopeServer, ScopeProject},
		},
	} {
		// Twice would mean two files each believing they own a permission,
		// and the later description silently winning. Refused at startup.
		if _, taken := catalogue[definition.Name]; taken {
			panic("permission defined twice: " + string(definition.Name))
		}
		catalogue[definition.Name] = definition
	}
}

/*
AllowsProject reports whether the actor holds a permission in a project.

The same precedence as AllowsChannel: an entry at the project's scope settles
the question whichever way it points, and otherwise the server scope decides.
*/
func (p Permissions) AllowsProject(permission Permission, projectID uuid.UUID) Decision {
	if scope, ok := p.channelScope[projectID]; ok {
		if granted, present := scope[permission]; present {
			if granted {
				return Decision{Allowed: true, Permission: permission, Reason: "granted for this project", Basis: BasisScope}
			}
			return Decision{Allowed: false, Permission: permission, Reason: "denied for this project", Basis: BasisScope}
		}
	}
	return p.AllowsServer(permission)
}

/*
DeniedInProject reports an explicit denial at a project's scope.

What a project's own membership confers is decided outside this package, and
this is what lets it yield: a denial written deliberately for one project beats
anything implied by being in it, the same way a denial beats administration.
*/
func (p Permissions) DeniedInProject(permission Permission, projectID uuid.UUID) bool {
	return IsExplicitlyDenied(p, permission, &projectID) ||
		IsExplicitlyDenied(p, permission, nil)
}

/*
SetRolePermissionInProject grants or denies a permission on a role, for one
project.

Refused for a permission that cannot take effect at project scope — a channel's
permissions, or server administration — so a grant nobody could ever use is
never stored. None of the project permissions is administrative, so neither
lockout check applies here.
*/
func (s *Service) SetRolePermissionInProject(
	ctx context.Context,
	roleID uuid.UUID,
	permission Permission,
	projectID uuid.UUID,
	granted bool,
) error {
	definition, known := Lookup(permission)
	if !known {
		return fmt.Errorf("%w: %q", ErrUnknownPermission, permission)
	}
	if !GrantableAt(permission, ScopeProject) {
		return fmt.Errorf("%w: %q may be granted only at %v scope",
			ErrScopeNotAllowed, permission, definition.Scopes)
	}

	if err := s.store.SetRolePermission(ctx, dbgen.SetRolePermissionParams{
		RoleID:     roleID,
		Permission: string(permission),
		ScopeID:    &projectID,
		Granted:    granted,
	}); err != nil {
		return fmt.Errorf("set role permission: %w", err)
	}
	return nil
}
