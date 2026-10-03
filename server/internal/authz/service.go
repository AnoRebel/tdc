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

// Errors a caller is expected to handle.
var (
	// ErrUnknownPermission reports a permission name not in the catalogue. A
	// typo fails loudly rather than being stored and silently ignored.
	ErrUnknownPermission = errors.New("unknown permission")

	// ErrScopeNotAllowed reports a permission granted at a scope it cannot
	// take effect at, such as server administration attached to a channel.
	ErrScopeNotAllowed = errors.New("permission cannot be granted at this scope")

	/*
		ErrWouldLockYourselfOut reports a change that would remove the actor's
		own ability to administer the server.

		Distinct from ErrLastAdministrator, which protects the server. This
		protects the person making the change: the server stays administered,
		but they can no longer reach it, and getting back in means asking
		somebody else. Confirmable rather than refused outright, because
		stepping down is a legitimate thing to do.
	*/
	ErrWouldLockYourselfOut = errors.New("this would remove your own ability to administer this server")

	// ErrLastAdministrator reports a change that would leave the server with
	// nobody able to administer it.
	ErrLastAdministrator = errors.New("this would leave the server with no administrator")

	// ErrRoleNotFound reports a role that does not exist.
	ErrRoleNotFound = errors.New("role not found")

	// ErrRoleProtected reports an attempt to delete a built-in role.
	ErrRoleProtected = errors.New("built-in roles cannot be deleted")
)

// Store is the database surface the authorization service needs.
type Store interface {
	dbgen.Querier
}

// Service manages roles and resolves permissions.
type Service struct {
	store Store
}

// NewService builds an authorization service over the given store.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// PermissionsFor resolves everything a user is allowed to do.
//
// One query returns every grant across every role the person holds, because a
// request typically tests several permissions and per-check queries would each
// be a round trip.
func (s *Service) PermissionsFor(ctx context.Context, userID uuid.UUID) (Permissions, error) {
	rows, err := s.store.ListGrantsForUser(ctx, dbgen.ListGrantsForUserParams{
		BaselineRole: DefaultRoleName,
		UserID:       userID,
	})
	if err != nil {
		return Permissions{}, fmt.Errorf("list grants: %w", err)
	}

	grants := make([]Grant, 0, len(rows))
	for _, row := range rows {
		grants = append(grants, Grant{
			Permission: Permission(row.Permission),
			ChannelID:  row.ScopeID,
			Granted:    row.Granted,
			Baseline:   row.Baseline,
		})
	}

	return Resolve(grants), nil
}

/*
SetRolePermission grants or denies a permission on a role.

The permission must exist and must be grantable at the scope given, so an
unusable grant is refused at the point it is written rather than stored and
ignored at evaluation.

Withdrawing an administrative permission is checked twice: that somebody is
left able to administer the server, and that the person making the change is
not removing it from a role they hold themselves. actor is who is making the
change; confirmed lets them proceed anyway, having been told.
*/
func (s *Service) SetRolePermission(
	ctx context.Context,
	roleID uuid.UUID,
	permission Permission,
	channelID *uuid.UUID,
	granted bool,
	actor uuid.UUID,
	confirmed bool,
) error {
	definition, known := Lookup(permission)
	if !known {
		return fmt.Errorf("%w: %q", ErrUnknownPermission, permission)
	}

	scope := ScopeServer
	if channelID != nil {
		scope = ScopeChannel
	}
	if !GrantableAt(permission, scope) {
		return fmt.Errorf("%w: %q may be granted only at %v scope",
			ErrScopeNotAllowed, permission, definition.Scopes)
	}

	// Withdrawing an administrative permission could strand the server, so
	// check before writing rather than discovering it afterwards.
	if !granted && channelID == nil && definition.Administrative {
		if err := s.ensureAdministratorRemains(ctx, permission, roleID); err != nil {
			return err
		}

		// And could strand the person making the change, which the check above
		// does not catch: the server stays administered by everybody else.
		if !confirmed {
			held, err := s.userHoldsRole(ctx, actor, roleID)
			if err != nil {
				return err
			}
			if held {
				return ErrWouldLockYourselfOut
			}
		}
	}

	if err := s.store.SetRolePermission(ctx, dbgen.SetRolePermissionParams{
		RoleID:     roleID,
		Permission: string(permission),
		ScopeID:    channelID,
		Granted:    granted,
	}); err != nil {
		return fmt.Errorf("set role permission: %w", err)
	}

	return nil
}

// AssignRole gives a role to a user.
func (s *Service) AssignRole(ctx context.Context, userID, roleID uuid.UUID, grantedBy *uuid.UUID) error {
	if err := s.store.AssignRole(ctx, dbgen.AssignRoleParams{
		UserID:    userID,
		RoleID:    roleID,
		GrantedBy: grantedBy,
	}); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	return nil
}

/*
RemoveRole takes a role away from a user.

Refused in two cases, which protect different things.

The first is leaving the server with no administrator at all: a server nobody
can administer cannot be recovered through the application, only through the
database.

The second is somebody removing their own administration. That leaves the
server perfectly well administered by other people and the actor locked out of
it — recoverable, but only by asking one of them, and not at all if there is
nobody to ask in practice. Somebody who genuinely means to step down passes
confirmed, which is a deliberate act rather than a click that did more than it
appeared to.

actor is who is making the change, which is not always the person losing the
role.
*/
func (s *Service) RemoveRole(
	ctx context.Context,
	userID, roleID uuid.UUID,
	actor uuid.UUID,
	confirmed bool,
) error {
	confers, err := s.roleConfersAdministration(ctx, roleID)
	if err != nil {
		return err
	}

	if confers {
		remaining, err := s.administratorCount(ctx)
		if err != nil {
			return err
		}
		if remaining <= 1 {
			return ErrLastAdministrator
		}

		if userID == actor && !confirmed {
			return ErrWouldLockYourselfOut
		}
	}

	if err := s.store.RemoveRoleFromUser(ctx, dbgen.RemoveRoleFromUserParams{
		UserID: userID,
		RoleID: roleID,
	}); err != nil {
		return fmt.Errorf("remove role: %w", err)
	}
	return nil
}

// DeleteRole removes a role. Users holding it are left in place, having lost
// only what the role granted.
func (s *Service) DeleteRole(ctx context.Context, roleID uuid.UUID) error {
	role, err := s.store.GetRoleByID(ctx, roleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRoleNotFound
	}
	if err != nil {
		return fmt.Errorf("read role: %w", err)
	}
	if role.BuiltIn {
		return ErrRoleProtected
	}

	confers, err := s.roleConfersAdministration(ctx, roleID)
	if err != nil {
		return err
	}
	if confers {
		remaining, err := s.administratorCount(ctx)
		if err != nil {
			return err
		}
		// Deleting the role strips administration from everyone holding it.
		if remaining <= 1 {
			return ErrLastAdministrator
		}
	}

	if err := s.store.DeleteRole(ctx, roleID); err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	return nil
}

// ensureAdministratorRemains refuses a withdrawal that would leave nobody
// holding an administrative permission.
func (s *Service) ensureAdministratorRemains(ctx context.Context, permission Permission, roleID uuid.UUID) error {
	if permission != PermServerAdminister {
		// Only the top-level administration permission is load-bearing for
		// recovery; the rest can be regranted by an administrator.
		return nil
	}

	confers, err := s.roleConfersAdministration(ctx, roleID)
	if err != nil {
		return err
	}
	if !confers {
		return nil
	}

	remaining, err := s.administratorCount(ctx)
	if err != nil {
		return err
	}
	if remaining <= 1 {
		return ErrLastAdministrator
	}
	return nil
}

func (s *Service) roleConfersAdministration(ctx context.Context, roleID uuid.UUID) (bool, error) {
	permissions, err := s.store.ListRolePermissions(ctx, roleID)
	if err != nil {
		return false, fmt.Errorf("list role permissions: %w", err)
	}
	for _, permission := range permissions {
		if Permission(permission.Permission) == PermServerAdminister &&
			permission.ScopeID == nil && permission.Granted {
			return true, nil
		}
	}
	return false, nil
}

// userHoldsRole reports whether somebody currently holds a role.
//
// Used to tell a change that costs the actor their own access from one that
// costs somebody else theirs.
func (s *Service) userHoldsRole(ctx context.Context, userID, roleID uuid.UUID) (bool, error) {
	roles, err := s.store.ListRolesForUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("list roles for user: %w", err)
	}
	for _, role := range roles {
		if role.ID == roleID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) administratorCount(ctx context.Context) (int64, error) {
	count, err := s.store.CountUsersWithPermission(ctx, string(PermServerAdminister))
	if err != nil {
		return 0, fmt.Errorf("count administrators: %w", err)
	}
	return count, nil
}

// EnsureRole returns a role by name, creating it with the given permissions if
// it does not exist.
//
// Used for the built-in roles a server needs to function, most importantly the
// administrator role created during first-run setup.
// ErrRoleNameTaken reports a name another role already has.
var ErrRoleNameTaken = errors.New("a role already has that name")

// ErrInvalidRoleName reports a name that cannot be used. Narrow on purpose: a
// role's name is what an administrator picks it out by in a list.
var ErrInvalidRoleName = errors.New("that is not a usable role name")

// ListRoles is every role on this server.
func (s *Service) ListRoles(ctx context.Context) ([]dbgen.Role, error) {
	return s.store.ListRoles(ctx)
}

/*
CreateRole makes a role with no permissions at all.

Empty deliberately: a role that arrived carrying something would be a role
somebody granted without deciding to. Permissions are added one at a time, each
its own decision and its own audit entry.
*/
func (s *Service) CreateRole(ctx context.Context, name, description string) (dbgen.Role, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > 64 {
		return dbgen.Role{}, fmt.Errorf("%w: between one and sixty-four characters", ErrInvalidRoleName)
	}

	if _, err := s.store.GetRoleByName(ctx, trimmed); err == nil {
		return dbgen.Role{}, ErrRoleNameTaken
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Role{}, fmt.Errorf("look up role: %w", err)
	}

	role, err := s.store.CreateRole(ctx, dbgen.CreateRoleParams{
		ID:          uuid.New(),
		Name:        trimmed,
		Description: strings.TrimSpace(description),
		// Not built-in: an operator made it, and an operator may remove it.
		BuiltIn: false,
	})
	if err != nil {
		return dbgen.Role{}, fmt.Errorf("create role: %w", err)
	}
	return role, nil
}

/*
DefaultRoleName is the role every account holds.

An account with no role holds no permissions, which on a server where
channel.create is enforced means only the first administrator can make a
channel. For a community of hundreds moving from somewhere else, that is not a
detail — it is one person doing setup by hand for everybody.

So there is a role, and registration puts people in it. What it grants is
deliberately small: reading and posting in the channels somebody belongs to,
making a channel, inviting somebody to one, and starting a project. None is
destructive, all are what an ordinary member of a developer community does
without asking, and an operator who disagrees can take any of them away — it
is an ordinary role, and taking a permission from it is one call. Withdrawing
channel.post for one channel is how an announcement channel is made, and
granting it there on another role is how the people who announce keep it: what
this role confers is a baseline every other role overrides.
*/
const DefaultRoleName = "Member"

/*
EnsureDefaultRole creates the role every account holds, if it does not exist.

Built-in, so it cannot be deleted out from under the accounts that hold it.
Its permissions can be changed freely: an operator who wants a server where only
administrators make channels withdraws channel.create and nothing else breaks.
*/
func (s *Service) EnsureDefaultRole(ctx context.Context) (dbgen.Role, error) {
	return s.EnsureRole(ctx, DefaultRoleName,
		"What every member can do",
		PermChannelRead, PermChannelPost, PermChannelCreate, PermChannelInvite, PermProjectCreate)
}

func (s *Service) EnsureRole(
	ctx context.Context,
	name, description string,
	permissions ...Permission,
) (dbgen.Role, error) {
	existing, err := s.store.GetRoleByName(ctx, name)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Role{}, fmt.Errorf("look up role: %w", err)
	}

	role, err := s.store.CreateRole(ctx, dbgen.CreateRoleParams{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		// Built-in roles cannot be deleted, so a server always retains a
		// coherent permission model.
		BuiltIn: true,
	})
	if err != nil {
		return dbgen.Role{}, fmt.Errorf("create role: %w", err)
	}

	for _, permission := range permissions {
		// Granting, not withdrawing, and during setup where there is no actor
		// — neither lockout check can apply.
		if err := s.SetRolePermission(
			ctx, role.ID, permission, nil, true, uuid.Nil, false,
		); err != nil {
			return dbgen.Role{}, err
		}
	}

	return role, nil
}
