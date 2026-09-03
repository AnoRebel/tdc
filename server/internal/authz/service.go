package authz

import (
	"context"
	"errors"
	"fmt"

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
	rows, err := s.store.ListGrantsForUser(ctx, userID)
	if err != nil {
		return Permissions{}, fmt.Errorf("list grants: %w", err)
	}

	grants := make([]Grant, 0, len(rows))
	for _, row := range rows {
		grants = append(grants, Grant{
			Permission: Permission(row.Permission),
			ChannelID:  row.ScopeID,
			Granted:    row.Granted,
		})
	}

	return Resolve(grants), nil
}

// SetRolePermission grants or denies a permission on a role.
//
// The permission must exist and must be grantable at the scope given, so an
// unusable grant is refused at the point it is written rather than stored and
// ignored at evaluation.
func (s *Service) SetRolePermission(
	ctx context.Context,
	roleID uuid.UUID,
	permission Permission,
	channelID *uuid.UUID,
	granted bool,
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

// RemoveRole takes a role away from a user.
//
// Refused when it would remove the last account able to administer the server:
// a server with no administrator cannot be recovered through the application.
func (s *Service) RemoveRole(ctx context.Context, userID, roleID uuid.UUID) error {
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
		if err := s.SetRolePermission(ctx, role.ID, permission, nil, true); err != nil {
			return dbgen.Role{}, err
		}
	}

	return role, nil
}
