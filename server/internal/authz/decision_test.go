package authz_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
)

func channel() *uuid.UUID {
	id := uuid.New()
	return &id
}

func TestGrantAtServerScopeApplies(t *testing.T) {
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermChannelPost, Granted: true},
	})

	if decision := permissions.AllowsServer(authz.PermChannelPost); !decision.Allowed {
		t.Errorf("AllowsServer = %v, want allowed: %s", decision.Allowed, decision.Reason)
	}
}

func TestUngrantedPermissionIsRefused(t *testing.T) {
	permissions := authz.Resolve(nil)

	decision := permissions.AllowsServer(authz.PermChannelPost)
	if decision.Allowed {
		t.Fatal("an ungranted permission was allowed")
	}
	// A refusal must name the permission that would have allowed it.
	if decision.Error() == "" {
		t.Error("the refusal gives no reason")
	}
}

// Holding several roles gives the union of what they grant.
func TestMultipleRolesUnion(t *testing.T) {
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermChannelPost, Granted: true},
		{Permission: authz.PermChannelCreate, Granted: true},
	})

	for _, permission := range []authz.Permission{authz.PermChannelPost, authz.PermChannelCreate} {
		if !permissions.AllowsServer(permission).Allowed {
			t.Errorf("%s was not granted by the union of roles", permission)
		}
	}
}

// A denial in one role must beat a grant in another, so removing access does
// not require finding every role that conferred it.
func TestDenialBeatsGrantInSameScope(t *testing.T) {
	both := [][]authz.Grant{
		{
			{Permission: authz.PermChannelPost, Granted: true},
			{Permission: authz.PermChannelPost, Granted: false},
		},
		// The reverse order must give the same answer.
		{
			{Permission: authz.PermChannelPost, Granted: false},
			{Permission: authz.PermChannelPost, Granted: true},
		},
	}

	for i, grants := range both {
		if authz.Resolve(grants).AllowsServer(authz.PermChannelPost).Allowed {
			t.Errorf("case %d: a denial did not beat a grant", i)
		}
	}
}

// The specification's central rule: a denial at channel scope overrides a
// grant at server scope, and only for that channel.
func TestChannelDenialOverridesServerGrant(t *testing.T) {
	restricted := channel()
	other := uuid.New()

	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermChannelPost, Granted: true},
		{Permission: authz.PermChannelPost, ChannelID: restricted, Granted: false},
	})

	if permissions.AllowsChannel(authz.PermChannelPost, *restricted).Allowed {
		t.Error("posting was allowed in the channel that denies it")
	}
	if !permissions.AllowsChannel(authz.PermChannelPost, other).Allowed {
		t.Error("the denial leaked into an unrelated channel")
	}
	if !permissions.AllowsServer(authz.PermChannelPost).Allowed {
		t.Error("the channel denial removed the server-scope grant")
	}
}

// A channel-scope grant must confer authority in that channel without
// conferring it anywhere else.
func TestChannelGrantDoesNotLeak(t *testing.T) {
	moderated := channel()
	elsewhere := uuid.New()

	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermMessageDeleteAny, ChannelID: moderated, Granted: true},
	})

	if !permissions.AllowsChannel(authz.PermMessageDeleteAny, *moderated).Allowed {
		t.Error("the channel moderator cannot moderate their own channel")
	}
	if permissions.AllowsChannel(authz.PermMessageDeleteAny, elsewhere).Allowed {
		t.Error("a channel-scope grant conferred authority in another channel")
	}
	if permissions.AllowsServer(authz.PermMessageDeleteAny).Allowed {
		t.Error("a channel-scope grant conferred server-wide authority")
	}
}

// An administrator holds every permission without each being granted
// individually.
func TestAdministratorHoldsEveryPermission(t *testing.T) {
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermServerAdminister, Granted: true},
	})

	for _, definition := range authz.Catalogue() {
		if !permissions.AllowsServer(definition.Name).Allowed {
			t.Errorf("administrator lacks %s", definition.Name)
		}
	}
}

// An explicit denial must hold even against an administrator, so that
// "administrator except for X" is expressible.
func TestExplicitDenialConstrainsAdministrator(t *testing.T) {
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermServerAdminister, Granted: true},
		{Permission: authz.PermServerViewAudit, Granted: false},
	})

	if permissions.AllowsServer(authz.PermServerViewAudit).Allowed {
		t.Error("an explicit denial did not constrain an administrator")
	}
	if !permissions.AllowsServer(authz.PermServerConfigure).Allowed {
		t.Error("the denial removed unrelated administrative authority")
	}
}

func TestCatalogueIsComplete(t *testing.T) {
	catalogue := authz.Catalogue()
	if len(catalogue) == 0 {
		t.Fatal("the catalogue is empty")
	}

	for _, definition := range catalogue {
		if definition.Description == "" {
			t.Errorf("%s has no description", definition.Name)
		}
		if len(definition.Scopes) == 0 {
			t.Errorf("%s may be granted at no scope, so it can never take effect", definition.Name)
		}
		if _, known := authz.Lookup(definition.Name); !known {
			t.Errorf("%s is listed but not resolvable", definition.Name)
		}
	}
}

// A permission that is not in the catalogue must not be grantable, so a typo
// in a role fails loudly rather than silently withholding access.
func TestUnknownPermissionIsNotGrantable(t *testing.T) {
	if authz.GrantableAt("channel.psot", authz.ScopeServer) {
		t.Error("a misspelled permission was accepted")
	}
	if _, known := authz.Lookup("nonsense.permission"); known {
		t.Error("an unknown permission resolved")
	}
}

// Server-only permissions must not be attachable to a channel, or a channel
// role could confer server administration.
func TestServerOnlyPermissionsRejectChannelScope(t *testing.T) {
	for _, permission := range []authz.Permission{
		authz.PermServerAdminister,
		authz.PermServerConfigure,
		authz.PermRoleManage,
	} {
		if authz.GrantableAt(permission, authz.ScopeChannel) {
			t.Errorf("%s may be granted at channel scope", permission)
		}
		if !authz.GrantableAt(permission, authz.ScopeServer) {
			t.Errorf("%s may not be granted at server scope", permission)
		}
	}
}

func TestAdministrativePermissionsAreMarked(t *testing.T) {
	if !authz.IsAdministrative(authz.PermServerAdminister) {
		t.Error("server administration is not marked administrative")
	}
	if authz.IsAdministrative(authz.PermChannelPost) {
		t.Error("posting a message is marked administrative")
	}
}

/*
An announcement channel: everybody holds Member, Member may not post in this
channel, and the role that announces may.

Between two ordinary roles the denial would win, and nobody holding Member
could ever be let back into the channel. The role every account holds is a
baseline instead, which another role's entry at the same scope replaces.
*/
func TestAnotherRoleOverridesTheBaselineInTheSameScope(t *testing.T) {
	announcements := channel()

	member := authz.Grant{Permission: authz.PermChannelPost, ChannelID: announcements, Granted: false, Baseline: true}
	announcer := authz.Grant{Permission: authz.PermChannelPost, ChannelID: announcements, Granted: true}

	if authz.Resolve([]authz.Grant{member}).AllowsChannel(authz.PermChannelPost, *announcements).Allowed {
		t.Error("a member who only holds Member posted where Member may not")
	}
	// In either order: the baseline is not decided by which row arrives first.
	for _, grants := range [][]authz.Grant{{member, announcer}, {announcer, member}} {
		if !authz.Resolve(grants).AllowsChannel(authz.PermChannelPost, *announcements).Allowed {
			t.Error("the announcing role could not post over the Member baseline")
		}
	}
}

// Only at the same scope: a grant everywhere does not reopen a channel the
// baseline closed, or one channel's restriction would vanish the moment
// somebody held any role granting the permission at server scope.
func TestTheBaselineHoldsAgainstABroaderGrant(t *testing.T) {
	announcements := channel()
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermChannelPost, Granted: true, Baseline: true},
		{Permission: authz.PermChannelPost, ChannelID: announcements, Granted: false, Baseline: true},
		{Permission: authz.PermChannelPost, Granted: true},
	})

	if permissions.AllowsChannel(authz.PermChannelPost, *announcements).Allowed {
		t.Error("a server-scope grant reopened a channel the baseline closed")
	}
}

// A denial on a role other than the baseline still beats a grant.
func TestOrdinaryRolesStillDenyFirst(t *testing.T) {
	announcements := channel()
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermChannelPost, ChannelID: announcements, Granted: true, Baseline: true},
		{Permission: authz.PermChannelPost, ChannelID: announcements, Granted: true},
		{Permission: authz.PermChannelPost, ChannelID: announcements, Granted: false},
	})

	if permissions.AllowsChannel(authz.PermChannelPost, *announcements).Allowed {
		t.Error("a denial on an ordinary role lost to a grant")
	}
}

/*
An administrator who also holds Member is not held to it.

Otherwise, of two administrators, the one promoted from the membership would be
silenced by an announcement channel and the one who set the server up — who
never held Member — would not.
*/
func TestTheBaselineDoesNotBindAnAdministrator(t *testing.T) {
	announcements := channel()
	permissions := authz.Resolve([]authz.Grant{
		{Permission: authz.PermChannelPost, ChannelID: announcements, Granted: false, Baseline: true},
		{Permission: authz.PermChannelCreate, Granted: false, Baseline: true},
		{Permission: authz.PermServerAdminister, Granted: true},
	})

	if !permissions.AllowsChannel(authz.PermChannelPost, *announcements).Allowed {
		t.Error("an administrator holding Member was silenced in an announcement channel")
	}
	if !permissions.AllowsServer(authz.PermChannelCreate).Allowed {
		t.Error("withdrawing channel.create from Member took it from an administrator")
	}
}
