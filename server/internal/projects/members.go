package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// Members lists who is in a project, for somebody who may see it.
func (s *Service) Members(ctx context.Context, access Access) ([]dbgen.ListProjectMembersRow, error) {
	members, err := s.queries.ListProjectMembers(ctx, access.Project.ID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return members, nil
}

/*
SetMember adds somebody to a project, or changes their role in it.

Demoting the last administrator is refused: a project nobody can administer
cannot have its members or its states changed again except by the server's
own administrators.
*/
func (s *Service) SetMember(ctx context.Context, actor Actor, access Access, username, role string) error {
	if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
		return RefusalError{decision}
	}
	if role == "" {
		role = "member"
	}
	if role != "admin" && role != "member" {
		return ErrMemberRole
	}

	person, err := s.queries.GetUserByUsername(ctx, identity.NormaliseUsername(username))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSuchPerson
	}
	if err != nil {
		return fmt.Errorf("read person: %w", err)
	}

	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		previous, err := q.GetProjectMemberRole(ctx, dbgen.GetProjectMemberRoleParams{
			ProjectID: access.Project.ID, UserID: person.ID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read membership: %w", err)
		}
		if previous == role {
			return nil
		}

		if previous == "admin" && role != "admin" {
			admins, err := q.CountProjectAdmins(ctx, access.Project.ID)
			if err != nil {
				return fmt.Errorf("count administrators: %w", err)
			}
			if admins <= 1 {
				return ErrLastAdmin
			}
		}

		if err := q.AddProjectMember(ctx, dbgen.AddProjectMemberParams{
			ProjectID: access.Project.ID, UserID: person.ID, Role: role, AddedBy: &actor.ID,
		}); err != nil {
			return fmt.Errorf("add member: %w", err)
		}

		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectMemberAdded,
			map[string]any{
				"member":   s.rooms.UserAddress(person.Username),
				"role":     role,
				"previous": previous,
			})
	})
}

/*
RemoveMember takes somebody out of a project.

In a private project that is a revocation, and it reaches the issues'
discussions in the same transaction: a membership row left behind in one would
keep delivering its notifications — message previews included — to somebody
who may no longer read it.

Somebody may always remove themselves, except as the last administrator.
*/
func (s *Service) RemoveMember(ctx context.Context, actor Actor, access Access, username string) error {
	person, err := s.queries.GetUserByUsername(ctx, identity.NormaliseUsername(username))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSuchPerson
	}
	if err != nil {
		return fmt.Errorf("read person: %w", err)
	}

	if person.ID != actor.ID {
		if decision := access.Allows(authz.PermProjectAdminister); !decision.Allowed {
			return RefusalError{decision}
		}
	}

	return s.inTransaction(ctx, func(q *dbgen.Queries) error {
		previous, err := q.GetProjectMemberRole(ctx, dbgen.GetProjectMemberRoleParams{
			ProjectID: access.Project.ID, UserID: person.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAMember
		}
		if err != nil {
			return fmt.Errorf("read membership: %w", err)
		}

		if previous == "admin" {
			admins, err := q.CountProjectAdmins(ctx, access.Project.ID)
			if err != nil {
				return fmt.Errorf("count administrators: %w", err)
			}
			if admins <= 1 {
				return ErrLastAdmin
			}
		}

		if err := q.RemoveProjectMember(ctx, dbgen.RemoveProjectMemberParams{
			ProjectID: access.Project.ID, UserID: person.ID,
		}); err != nil {
			return fmt.Errorf("remove member: %w", err)
		}

		if access.Project.Visibility == "private" {
			if err := q.RemoveFromProjectDiscussions(ctx, dbgen.RemoveFromProjectDiscussionsParams{
				ProjectID: access.Project.ID, UserID: person.ID,
			}); err != nil {
				return fmt.Errorf("remove from discussions: %w", err)
			}
		}

		return s.appendProjectEvent(ctx, q, access.Project.ID, actor.ID, events.TypeProjectMemberRemoved,
			map[string]any{"member": s.rooms.UserAddress(person.Username), "previous": previous})
	})
}
