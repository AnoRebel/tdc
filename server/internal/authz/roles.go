package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
What an administration screen needs beyond granting and assigning: renaming a
role, putting an entry back to saying nothing, and answering why somebody may
do what they may.
*/

// ErrBuiltInName reports an attempt to rename a role the server finds by name.
var ErrBuiltInName = errors.New("a built-in role keeps its name")

/*
checkWithdrawal is what taking an administrative permission away from a role
must pass: somebody is left able to administer the server, and the person making
the change is not removing it from a role they hold, unless they said they mean
it.
*/
func (s *Service) checkWithdrawal(
	ctx context.Context,
	roleID uuid.UUID,
	permission Permission,
	actor uuid.UUID,
	confirmed bool,
) error {
	if err := s.ensureAdministratorRemains(ctx, permission, roleID); err != nil {
		return err
	}

	// And could strand the person making the change, which the check above
	// does not catch: the server stays administered by everybody else.
	if confirmed {
		return nil
	}
	held, err := s.userHoldsRole(ctx, actor, roleID)
	if err != nil {
		return err
	}
	if held {
		return ErrWouldLockYourselfOut
	}
	return nil
}

/*
ClearRolePermission removes a role's entry for a permission at one scope, so
the role says nothing about it either way.

Distinct from withdrawing, which stores a denial. A denial beats every other
role's grant at its scope; saying nothing leaves the decision to them. Clearing
a server-scope grant of something administrative is a withdrawal and is checked
as one; clearing a denial only ever gives something back, so it is not.
*/
func (s *Service) ClearRolePermission(
	ctx context.Context,
	roleID uuid.UUID,
	permission Permission,
	scopeID *uuid.UUID,
	actor uuid.UUID,
	confirmed bool,
) error {
	definition, known := Lookup(permission)
	if !known {
		return fmt.Errorf("%w: %q", ErrUnknownPermission, permission)
	}

	entries, err := s.store.ListRolePermissions(ctx, roleID)
	if err != nil {
		return fmt.Errorf("list role permissions: %w", err)
	}
	var existing *dbgen.RolePermission
	for i, entry := range entries {
		if entry.Permission == string(permission) && sameScope(entry.ScopeID, scopeID) {
			existing = &entries[i]
			break
		}
	}
	if existing == nil {
		// Already saying nothing. Not an error: the screen asked for a state
		// the role is already in.
		return nil
	}

	if existing.Granted && scopeID == nil && definition.Administrative {
		if err := s.checkWithdrawal(ctx, roleID, permission, actor, confirmed); err != nil {
			return err
		}
	}

	if err := s.store.RemoveRolePermission(ctx, dbgen.RemoveRolePermissionParams{
		RoleID:     roleID,
		Permission: string(permission),
		ScopeID:    scopeID,
	}); err != nil {
		return fmt.Errorf("clear role permission: %w", err)
	}
	return nil
}

func sameScope(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

/*
UpdateRole changes a role's name and description.

A built-in role keeps its name. The server finds Member and Administrator by
name — registration puts people in the one, setup in the other, and the
baseline rule recognises Member by it — so renaming either would quietly make a
second one the next time the server looked.
*/
func (s *Service) UpdateRole(ctx context.Context, roleID uuid.UUID, name, description string) (dbgen.Role, error) {
	role, err := s.store.GetRoleByID(ctx, roleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Role{}, ErrRoleNotFound
	}
	if err != nil {
		return dbgen.Role{}, fmt.Errorf("read role: %w", err)
	}

	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > 64 {
		return dbgen.Role{}, fmt.Errorf("%w: between one and sixty-four characters", ErrInvalidRoleName)
	}

	if trimmed != role.Name {
		if role.BuiltIn {
			return dbgen.Role{}, ErrBuiltInName
		}
		other, err := s.store.GetRoleByName(ctx, trimmed)
		switch {
		case err == nil && other.ID != roleID:
			return dbgen.Role{}, ErrRoleNameTaken
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return dbgen.Role{}, fmt.Errorf("look up role: %w", err)
		}
	}

	updated, err := s.store.UpdateRole(ctx, dbgen.UpdateRoleParams{
		ID:          roleID,
		Name:        trimmed,
		Description: strings.TrimSpace(description),
	})
	if err != nil {
		return dbgen.Role{}, fmt.Errorf("update role: %w", err)
	}
	return updated, nil
}

// RoleSummary is a role with how many hold it and every entry it carries.
type RoleSummary struct {
	dbgen.ListRolesWithMemberCountsRow
	Entries []dbgen.ListAllRolePermissionsRow
}

// RoleSummaries is every role, as the administration screen lists them.
func (s *Service) RoleSummaries(ctx context.Context) ([]RoleSummary, error) {
	roles, err := s.store.ListRolesWithMemberCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	entries, err := s.store.ListAllRolePermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list role permissions: %w", err)
	}

	byRole := make(map[uuid.UUID][]dbgen.ListAllRolePermissionsRow, len(roles))
	for _, entry := range entries {
		byRole[entry.RoleID] = append(byRole[entry.RoleID], entry)
	}

	summaries := make([]RoleSummary, 0, len(roles))
	for _, role := range roles {
		summaries = append(summaries, RoleSummary{
			ListRolesWithMemberCountsRow: role,
			Entries:                      append([]dbgen.ListAllRolePermissionsRow{}, byRole[role.ID]...),
		})
	}
	return summaries, nil
}

// RoleMembers is who holds a role, up to limit of them.
func (s *Service) RoleMembers(ctx context.Context, roleID uuid.UUID, limit int32) ([]dbgen.ListRoleMembersRow, error) {
	if _, err := s.store.GetRoleByID(ctx, roleID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read role: %w", err)
	}
	members, err := s.store.ListRoleMembers(ctx, dbgen.ListRoleMembersParams{RoleID: roleID, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list role members: %w", err)
	}
	return members, nil
}

// RolesFor is every role somebody holds.
func (s *Service) RolesFor(ctx context.Context, userID uuid.UUID) ([]dbgen.Role, error) {
	roles, err := s.store.ListRolesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list roles for user: %w", err)
	}
	return roles, nil
}

// GrantSource is one entry behind somebody's permissions, and the role it is on.
type GrantSource struct {
	RoleID   uuid.UUID
	RoleName string
	Grant
}

/*
Explain resolves somebody's permissions and says where each entry came from.

The same rows PermissionsFor resolves, read once, so the answer to "why may
they" can never disagree with what the server actually decides.
*/
func (s *Service) Explain(ctx context.Context, userID uuid.UUID) (Permissions, []GrantSource, error) {
	rows, err := s.store.ListGrantSourcesForUser(ctx, dbgen.ListGrantSourcesForUserParams{
		BaselineRole: DefaultRoleName,
		UserID:       userID,
	})
	if err != nil {
		return Permissions{}, nil, fmt.Errorf("list grant sources: %w", err)
	}

	grants := make([]Grant, 0, len(rows))
	sources := make([]GrantSource, 0, len(rows))
	for _, row := range rows {
		grant := Grant{
			Permission: Permission(row.Permission),
			ChannelID:  row.ScopeID,
			Granted:    row.Granted,
			Baseline:   row.Baseline,
		}
		grants = append(grants, grant)
		sources = append(sources, GrantSource{RoleID: row.RoleID, RoleName: row.RoleName, Grant: grant})
	}
	return Resolve(grants), sources, nil
}
