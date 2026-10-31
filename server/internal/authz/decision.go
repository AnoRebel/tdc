package authz

import (
	"fmt"
	"uuid"
)

// Grant is one permission a role confers or withholds at a scope.
type Grant struct {
	Permission Permission
	// ChannelID is nil for a server-scope grant, otherwise the channel the
	// grant applies to.
	ChannelID *uuid.UUID
	// Granted is false for an explicit denial. A denial is stored rather than
	// implied by absence, because it has to be able to override a grant made
	// at a broader scope.
	Granted bool
	// Baseline is true for what the role every account holds confers. Any
	// other role's entry at the same scope overrides it, whichever way either
	// points.
	Baseline bool
}

// Decision is the outcome of an authorization check, carrying enough detail
// for a refusal to explain itself.
type Decision struct {
	Allowed bool
	// Permission is what was checked.
	Permission Permission
	// Reason says why, in terms suitable for a log or an error message.
	Reason string
	// Basis says where the answer came from, as a word a client can translate
	// rather than a sentence it would have to parse.
	Basis Basis
}

// Basis is where an authorization decision came from.
type Basis string

const (
	// BasisAdministrator is an allowance that comes from server.administer.
	BasisAdministrator Basis = "administrator"
	// BasisServer is an entry at server scope, granting or denying.
	BasisServer Basis = "server"
	// BasisScope is an entry for the one channel or project asked about.
	BasisScope Basis = "scope"
	// BasisNone is nothing at all: no role says anything about it.
	BasisNone Basis = "none"
)

// Error returns a message naming the permission that would have allowed the
// action, which is what the specification requires a refusal to state.
func (d Decision) Error() string {
	if d.Allowed {
		return ""
	}
	return fmt.Sprintf("requires permission %q", d.Permission)
}

// Permissions is an actor's effective authority, already resolved.
//
// It is computed once per request and consulted many times, rather than
// queried per check: a request typically tests several permissions, and each
// would otherwise be a round trip.
type Permissions struct {
	// serverScope holds grants that apply everywhere.
	serverScope map[Permission]bool
	// channelScope holds per-channel overrides, keyed by channel then
	// permission. An entry here wins over serverScope regardless of whether it
	// grants or denies, because the more specific scope governs.
	channelScope map[uuid.UUID]map[Permission]bool
}

/*
Resolve combines a set of grants into effective permissions.

The rules, in order:
  - Grants from every role a person holds are unioned.
  - Within one scope, an explicit denial beats a grant, so removing access
    does not depend on finding and editing every role that conferred it.
  - Except that the role every account holds is a baseline beneath the others:
    any other role's entry at the same scope replaces its entry there. That is
    what makes an announcement channel possible. Withdrawing channel.post from
    Member in one channel silences everybody, because everybody holds Member;
    a role that grants channel.post there is how the few who announce are let
    back in. Were the denial to win, as it would between two ordinary roles,
    nobody holding Member could ever be given the channel back. And somebody
    another role makes an administrator is not held to the baseline at all.
  - A channel-scope entry overrides the server scope for that channel,
    whichever way it points.
*/
func Resolve(grants []Grant) Permissions {
	resolved := Permissions{
		serverScope:  make(map[Permission]bool),
		channelScope: make(map[uuid.UUID]map[Permission]bool),
	}

	// Applying a grant never overwrites a denial, and a denial always
	// overwrites a grant. That makes the result independent of the order the
	// grants arrive in, which matters because they come from several roles.
	apply := func(scope map[Permission]bool, permission Permission, granted bool) {
		if !granted {
			scope[permission] = false
			return
		}
		if existing, present := scope[permission]; present && !existing {
			return
		}
		scope[permission] = true
	}

	scopeOf := func(into Permissions, grant Grant) map[Permission]bool {
		if grant.ChannelID == nil {
			return into.serverScope
		}
		scope, ok := into.channelScope[*grant.ChannelID]
		if !ok {
			scope = make(map[Permission]bool)
			into.channelScope[*grant.ChannelID] = scope
		}
		return scope
	}

	// The baseline first, then every other role resolved among themselves and
	// laid over it entry by entry.
	others := Permissions{
		serverScope:  make(map[Permission]bool),
		channelScope: make(map[uuid.UUID]map[Permission]bool),
	}
	for _, grant := range grants {
		if grant.Baseline {
			apply(scopeOf(resolved, grant), grant.Permission, grant.Granted)
		} else {
			apply(scopeOf(others, grant), grant.Permission, grant.Granted)
		}
	}

	/*
		An administrator is not an ordinary member, and the baseline describes
		ordinary members. Without this, of two administrators the one promoted
		from the membership would be silenced by an announcement channel and the
		one who set the server up would not — and withdrawing channel.create from
		Member, to leave making channels to administrators, would take it from
		half of them.
	*/
	if others.serverScope[PermServerAdminister] {
		return others
	}

	for permission, granted := range others.serverScope {
		resolved.serverScope[permission] = granted
	}
	for channelID, entries := range others.channelScope {
		scope := scopeOf(resolved, Grant{ChannelID: &channelID})
		for permission, granted := range entries {
			scope[permission] = granted
		}
	}

	return resolved
}

// AllowsServer reports whether the actor holds a permission at server scope.
func (p Permissions) AllowsServer(permission Permission) Decision {
	if p.serverScope[PermServerAdminister] && !IsExplicitlyDenied(p, permission, nil) {
		return Decision{Allowed: true, Permission: permission, Reason: "server administrator", Basis: BasisAdministrator}
	}
	granted, present := p.serverScope[permission]
	if granted {
		return Decision{Allowed: true, Permission: permission, Reason: "granted at server scope", Basis: BasisServer}
	}
	if present {
		return Decision{Allowed: false, Permission: permission, Reason: "denied at server scope", Basis: BasisServer}
	}
	return Decision{Allowed: false, Permission: permission, Reason: "not granted", Basis: BasisNone}
}

// AllowsChannel reports whether the actor holds a permission in a channel.
//
// The channel scope is consulted first: an entry there settles the question,
// whether it grants or denies, because the more specific scope governs.
func (p Permissions) AllowsChannel(permission Permission, channelID uuid.UUID) Decision {
	if scope, ok := p.channelScope[channelID]; ok {
		if granted, present := scope[permission]; present {
			if granted {
				return Decision{Allowed: true, Permission: permission, Reason: "granted for this channel", Basis: BasisScope}
			}
			return Decision{Allowed: false, Permission: permission, Reason: "denied for this channel", Basis: BasisScope}
		}
	}
	return p.AllowsServer(permission)
}

// IsExplicitlyDenied reports whether a permission carries an explicit denial
// at the given scope, which even administration cannot override.
func IsExplicitlyDenied(p Permissions, permission Permission, channelID *uuid.UUID) bool {
	if channelID != nil {
		if scope, ok := p.channelScope[*channelID]; ok {
			granted, present := scope[permission]
			return present && !granted
		}
		return false
	}
	granted, present := p.serverScope[permission]
	return present && !granted
}
