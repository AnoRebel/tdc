package authz_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
)

// Every project permission is grantable exactly where it can take effect:
// none at a channel, creation only server-wide, the rest server-wide or in
// one project.
func TestProjectPermissionsAreGrantableOnlyWhereTheyApply(t *testing.T) {
	projectScoped := []authz.Permission{
		authz.PermProjectAdminister, authz.PermProjectConfigureStates,
		authz.PermIssueCreate, authz.PermIssueTransition, authz.PermIssueAssign,
		authz.PermIssueLabel, authz.PermIssueRelate, authz.PermIssueDelete,
		authz.PermProjectPlan, authz.PermIssuePlan,
	}
	for _, permission := range projectScoped {
		if !authz.GrantableAt(permission, authz.ScopeProject) {
			t.Errorf("%s is not grantable in a project", permission)
		}
		if !authz.GrantableAt(permission, authz.ScopeServer) {
			t.Errorf("%s is not grantable server-wide", permission)
		}
		if authz.GrantableAt(permission, authz.ScopeChannel) {
			t.Errorf("%s is grantable at a channel, where it means nothing", permission)
		}
	}

	if authz.GrantableAt(authz.PermProjectCreate, authz.ScopeProject) ||
		authz.GrantableAt(authz.PermProjectCreate, authz.ScopeChannel) {
		t.Error("project.create is grantable inside a scope; a project that does not exist has none")
	}

	// And nothing that was not written for projects is grantable in one.
	for _, definition := range authz.Catalogue() {
		if authz.GrantableAt(definition.Name, authz.ScopeProject) {
			found := false
			for _, permission := range projectScoped {
				found = found || permission == definition.Name
			}
			if !found {
				t.Errorf("%s is grantable in a project", definition.Name)
			}
		}
	}
}

// The write path refuses what the catalogue says cannot apply, before
// touching the store — so a grant nobody could use is never stored.
func TestAProjectGrantAtTheWrongScopeIsRefused(t *testing.T) {
	service := authz.NewService(nil)
	project := uuid.New()

	for _, permission := range []authz.Permission{
		authz.PermChannelPost, authz.PermServerAdminister, authz.PermProjectCreate,
	} {
		err := service.SetRolePermissionInProject(context.Background(), uuid.New(), permission, project, true)
		if !errors.Is(err, authz.ErrScopeNotAllowed) {
			t.Errorf("granting %s in a project: %v, want ErrScopeNotAllowed", permission, err)
		}
	}

	err := service.SetRolePermission(context.Background(), uuid.New(), authz.PermIssueTransition,
		&project, true, uuid.Nil(), false)
	if !errors.Is(err, authz.ErrScopeNotAllowed) {
		t.Errorf("granting issue.transition at a channel: %v, want ErrScopeNotAllowed", err)
	}
}

// A project's own scope settles the question either way, and otherwise the
// server's does — the same precedence as a channel's.
func TestAllowsProjectPrecedence(t *testing.T) {
	project, elsewhere := uuid.New(), uuid.New()
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermIssueLabel, Granted: true},
		{Permission: authz.PermIssueLabel, ChannelID: &project, Granted: false},
		{Permission: authz.PermIssueAssign, ChannelID: &project, Granted: true},
	})

	if permissions.AllowsProject(authz.PermIssueLabel, project).Allowed {
		t.Error("a denial in the project was overridden by the server-wide grant")
	}
	if !permissions.AllowsProject(authz.PermIssueLabel, elsewhere).Allowed {
		t.Error("the server-wide grant did not apply to another project")
	}
	if !permissions.AllowsProject(authz.PermIssueAssign, project).Allowed {
		t.Error("a grant in the project did not apply there")
	}
	if permissions.AllowsProject(authz.PermIssueAssign, elsewhere).Allowed {
		t.Error("a grant in one project applied to another")
	}
	if !permissions.DeniedInProject(authz.PermIssueLabel, project) {
		t.Error("the explicit denial is not reported as one")
	}
}
