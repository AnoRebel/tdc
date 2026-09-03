package authz_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

func newTestService(t *testing.T) (*authz.Service, *dbgen.Queries) {
	t.Helper()

	url := dbtest.URL(t, "authz")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pool, err := db.Open(ctx, db.Config{URL: url}, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		for _, statement := range []string{"DELETE FROM users", "DELETE FROM roles"} {
			if _, err := pool.Exec(cleanCtx, statement); err != nil {
				t.Errorf("clean: %v", err)
			}
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	queries := dbgen.New(pool)
	return authz.NewService(queries), queries
}

func makeUser(t *testing.T, queries *dbgen.Queries, name string) uuid.UUID {
	t.Helper()
	user, err := queries.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       name,
		UsernameFolded: name,
		DisplayName:    name,
	})
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	return user.ID
}

func makeRole(t *testing.T, queries *dbgen.Queries, name string) uuid.UUID {
	t.Helper()
	role, err := queries.CreateRole(context.Background(), dbgen.CreateRoleParams{
		ID:   uuid.New(),
		Name: name,
	})
	if err != nil {
		t.Fatalf("create role %q: %v", name, err)
	}
	return role.ID
}

func TestPermissionsResolveFromRoles(t *testing.T) {
	service, queries := newTestService(t)
	ctx := context.Background()

	userID := makeUser(t, queries, "member")
	roleID := makeRole(t, queries, "poster")

	if err := service.SetRolePermission(ctx, roleID, authz.PermChannelPost, nil, true); err != nil {
		t.Fatalf("SetRolePermission: %v", err)
	}
	if err := service.AssignRole(ctx, userID, roleID, nil); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	permissions, err := service.PermissionsFor(ctx, userID)
	if err != nil {
		t.Fatalf("PermissionsFor: %v", err)
	}
	if !permissions.AllowsServer(authz.PermChannelPost).Allowed {
		t.Error("the granted permission did not resolve")
	}
	if permissions.AllowsServer(authz.PermChannelCreate).Allowed {
		t.Error("an ungranted permission resolved as allowed")
	}
}

// A permission removed from a role must stop applying without the user having
// to sign in again.
func TestRevokedPermissionStopsApplying(t *testing.T) {
	service, queries := newTestService(t)
	ctx := context.Background()

	userID := makeUser(t, queries, "member")
	roleID := makeRole(t, queries, "poster")

	if err := service.SetRolePermission(ctx, roleID, authz.PermChannelPost, nil, true); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := service.AssignRole(ctx, userID, roleID, nil); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if err := service.SetRolePermission(ctx, roleID, authz.PermChannelPost, nil, false); err != nil {
		t.Fatalf("deny: %v", err)
	}

	permissions, err := service.PermissionsFor(ctx, userID)
	if err != nil {
		t.Fatalf("PermissionsFor: %v", err)
	}
	if permissions.AllowsServer(authz.PermChannelPost).Allowed {
		t.Error("a withdrawn permission still applies")
	}
}

// The scope resolution rule, verified end to end through the database.
func TestChannelDenialOverridesServerGrantThroughStore(t *testing.T) {
	service, queries := newTestService(t)
	ctx := context.Background()

	userID := makeUser(t, queries, "member")
	roleID := makeRole(t, queries, "mixed")
	restricted := uuid.New()

	if err := service.SetRolePermission(ctx, roleID, authz.PermChannelPost, nil, true); err != nil {
		t.Fatalf("server grant: %v", err)
	}
	if err := service.SetRolePermission(ctx, roleID, authz.PermChannelPost, &restricted, false); err != nil {
		t.Fatalf("channel denial: %v", err)
	}
	if err := service.AssignRole(ctx, userID, roleID, nil); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	permissions, err := service.PermissionsFor(ctx, userID)
	if err != nil {
		t.Fatalf("PermissionsFor: %v", err)
	}
	if permissions.AllowsChannel(authz.PermChannelPost, restricted).Allowed {
		t.Error("posting allowed in the channel that denies it")
	}
	if !permissions.AllowsChannel(authz.PermChannelPost, uuid.New()).Allowed {
		t.Error("the denial leaked into an unrelated channel")
	}
}

// Grants from several roles combine.
func TestGrantsFromSeveralRolesCombine(t *testing.T) {
	service, queries := newTestService(t)
	ctx := context.Background()

	userID := makeUser(t, queries, "member")
	first := makeRole(t, queries, "posters")
	second := makeRole(t, queries, "creators")

	if err := service.SetRolePermission(ctx, first, authz.PermChannelPost, nil, true); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if err := service.SetRolePermission(ctx, second, authz.PermChannelCreate, nil, true); err != nil {
		t.Fatalf("second grant: %v", err)
	}
	for _, roleID := range []uuid.UUID{first, second} {
		if err := service.AssignRole(ctx, userID, roleID, nil); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
	}

	permissions, err := service.PermissionsFor(ctx, userID)
	if err != nil {
		t.Fatalf("PermissionsFor: %v", err)
	}
	for _, permission := range []authz.Permission{authz.PermChannelPost, authz.PermChannelCreate} {
		if !permissions.AllowsServer(permission).Allowed {
			t.Errorf("%s did not resolve from the union of roles", permission)
		}
	}
}

func TestUnknownPermissionIsRefused(t *testing.T) {
	service, queries := newTestService(t)
	roleID := makeRole(t, queries, "typo")

	err := service.SetRolePermission(context.Background(), roleID, "channel.psot", nil, true)
	if !errors.Is(err, authz.ErrUnknownPermission) {
		t.Errorf("error = %v, want ErrUnknownPermission", err)
	}
}

// Server-only permissions must be refused at channel scope, or a channel role
// could confer server administration.
func TestServerOnlyPermissionRefusedAtChannelScope(t *testing.T) {
	service, queries := newTestService(t)
	roleID := makeRole(t, queries, "channel-role")
	channelID := uuid.New()

	err := service.SetRolePermission(context.Background(), roleID, authz.PermServerAdminister, &channelID, true)
	if !errors.Is(err, authz.ErrScopeNotAllowed) {
		t.Errorf("error = %v, want ErrScopeNotAllowed", err)
	}
}

// A server must never be left with nobody able to administer it, since that
// state cannot be recovered through the application.
func TestLastAdministratorCannotBeRemoved(t *testing.T) {
	service, queries := newTestService(t)
	ctx := context.Background()

	adminID := makeUser(t, queries, "founder")
	roleID := makeRole(t, queries, "administrator")

	if err := service.SetRolePermission(ctx, roleID, authz.PermServerAdminister, nil, true); err != nil {
		t.Fatalf("grant administration: %v", err)
	}
	if err := service.AssignRole(ctx, adminID, roleID, nil); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if err := service.RemoveRole(ctx, adminID, roleID); !errors.Is(err, authz.ErrLastAdministrator) {
		t.Errorf("RemoveRole error = %v, want ErrLastAdministrator", err)
	}
	if err := service.SetRolePermission(ctx, roleID, authz.PermServerAdminister, nil, false); !errors.Is(err, authz.ErrLastAdministrator) {
		t.Errorf("withdrawing administration error = %v, want ErrLastAdministrator", err)
	}
	if err := service.DeleteRole(ctx, roleID); !errors.Is(err, authz.ErrLastAdministrator) {
		t.Errorf("DeleteRole error = %v, want ErrLastAdministrator", err)
	}
}

// With a second administrator present, removing the first is allowed.
func TestAdministratorCanBeRemovedWhenAnotherRemains(t *testing.T) {
	service, queries := newTestService(t)
	ctx := context.Background()

	first := makeUser(t, queries, "founder")
	second := makeUser(t, queries, "successor")
	roleID := makeRole(t, queries, "administrator")

	if err := service.SetRolePermission(ctx, roleID, authz.PermServerAdminister, nil, true); err != nil {
		t.Fatalf("grant administration: %v", err)
	}
	for _, userID := range []uuid.UUID{first, second} {
		if err := service.AssignRole(ctx, userID, roleID, nil); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
	}

	if err := service.RemoveRole(ctx, first, roleID); err != nil {
		t.Fatalf("RemoveRole with another administrator present: %v", err)
	}

	permissions, err := service.PermissionsFor(ctx, second)
	if err != nil {
		t.Fatalf("PermissionsFor: %v", err)
	}
	if !permissions.AllowsServer(authz.PermServerAdminister).Allowed {
		t.Error("the remaining administrator lost administration")
	}
}

// Deleting a role must not delete the people holding it.
func TestDeletingRolePreservesUsers(t *testing.T) {
	service, queries := newTestService(t)
	ctx := context.Background()

	userID := makeUser(t, queries, "member")
	roleID := makeRole(t, queries, "temporary")

	if err := service.SetRolePermission(ctx, roleID, authz.PermChannelPost, nil, true); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := service.AssignRole(ctx, userID, roleID, nil); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if err := service.DeleteRole(ctx, roleID); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}

	if _, err := queries.GetUserByID(ctx, userID); err != nil {
		t.Fatalf("the user was removed along with the role: %v", err)
	}

	permissions, err := service.PermissionsFor(ctx, userID)
	if err != nil {
		t.Fatalf("PermissionsFor: %v", err)
	}
	if permissions.AllowsServer(authz.PermChannelPost).Allowed {
		t.Error("the deleted role still grants its permission")
	}
}
