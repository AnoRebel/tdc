// Package authz decides what an actor is allowed to do.
//
// Authority is expressed as named permissions rather than coarse account
// types, so that moderation and administration can be delegated without
// granting unrelated power. Every guarded action names the permission that
// governs it, which is what lets a refusal explain itself.
package authz

import "sort"

// Permission is the name of one thing an actor may be allowed to do.
type Permission string

// Scope says where a permission applies.
type Scope string

const (
	// ScopeServer applies across the whole server.
	ScopeServer Scope = "server"
	// ScopeChannel applies to one channel only.
	ScopeChannel Scope = "channel"
)

// The permission catalogue.
//
// Names are namespaced by the area they govern, so that a reader of an audit
// entry or a refusal can tell at a glance what was being attempted.
const (
	// Server administration.
	PermServerAdminister Permission = "server.administer"
	PermServerConfigure  Permission = "server.configure"
	PermServerViewAudit  Permission = "server.audit.read"

	// User and role management.
	PermUserInvite  Permission = "user.invite"
	PermUserSuspend Permission = "user.suspend"
	PermUserViewAll Permission = "user.read.all"
	PermRoleManage  Permission = "role.manage"
	PermRoleAssign  Permission = "role.assign"

	// Channels.
	PermChannelCreate  Permission = "channel.create"
	PermChannelRename  Permission = "channel.rename"
	PermChannelArchive Permission = "channel.archive"
	PermChannelInvite  Permission = "channel.invite"
	PermChannelRemove  Permission = "channel.member.remove"
	PermChannelRead    Permission = "channel.read"
	PermChannelPost    Permission = "channel.post"

	// Messages. Acting on one's own messages needs no permission; these
	// govern acting on someone else's.
	PermMessageEditAny   Permission = "message.edit.any"
	PermMessageDeleteAny Permission = "message.delete.any"
	PermMessagePin       Permission = "message.pin"

	// Attachments.
	PermAttachmentUpload Permission = "attachment.upload"

	// Moderation.
	PermModerationReview   Permission = "moderation.review"
	PermModerationRestrict Permission = "moderation.restrict"

	// Calls.
	PermCallStart Permission = "call.start"
	PermCallJoin  Permission = "call.join"
	PermCallLink  Permission = "call.link"
)

// Definition describes a permission for the catalogue an administrator reads.
type Definition struct {
	Name Permission
	// Description says what the permission allows, in terms an administrator
	// deciding whether to grant it can act on.
	Description string
	// Scopes lists where this permission may be granted. A permission
	// grantable only at server scope cannot be attached to a channel.
	Scopes []Scope
	// Administrative marks permissions that confer power over the server
	// itself, which the administrator floor protects.
	Administrative bool
}

// catalogue is every permission the server understands.
//
// A permission that is not here cannot be granted: an unknown name in a role
// is rejected rather than silently ignored, so a typo fails loudly instead of
// quietly withholding access.
var catalogue = map[Permission]Definition{
	PermServerAdminister: {
		Name:           PermServerAdminister,
		Description:    "Administer the server, including granting and revoking any permission",
		Scopes:         []Scope{ScopeServer},
		Administrative: true,
	},
	PermServerConfigure: {
		Name:           PermServerConfigure,
		Description:    "Change server settings such as limits, registration policy and branding",
		Scopes:         []Scope{ScopeServer},
		Administrative: true,
	},
	PermServerViewAudit: {
		Name:           PermServerViewAudit,
		Description:    "Read the audit log",
		Scopes:         []Scope{ScopeServer},
		Administrative: true,
	},
	PermUserInvite: {
		Name:        PermUserInvite,
		Description: "Invite people to this server",
		Scopes:      []Scope{ScopeServer},
	},
	PermUserSuspend: {
		Name:        PermUserSuspend,
		Description: "Suspend and reinstate accounts",
		Scopes:      []Scope{ScopeServer},
	},
	PermUserViewAll: {
		Name:        PermUserViewAll,
		Description: "List every account on the server, suspended ones included — never their email addresses, and not a directory profile its owner has hidden",
		Scopes:      []Scope{ScopeServer},
	},
	PermRoleManage: {
		Name:           PermRoleManage,
		Description:    "Create, change and delete roles",
		Scopes:         []Scope{ScopeServer},
		Administrative: true,
	},
	PermRoleAssign: {
		Name:           PermRoleAssign,
		Description:    "Assign roles to people and remove them",
		Scopes:         []Scope{ScopeServer},
		Administrative: true,
	},
	PermChannelCreate: {
		Name:        PermChannelCreate,
		Description: "Create channels",
		Scopes:      []Scope{ScopeServer},
	},
	PermChannelRename: {
		Name:        PermChannelRename,
		Description: "Rename a channel, change its topic, and make it private — making one public needs this granted for the whole server",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermChannelArchive: {
		Name:        PermChannelArchive,
		Description: "Archive and restore a channel",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermChannelInvite: {
		Name:        PermChannelInvite,
		Description: "Add people to a channel",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermChannelRemove: {
		Name:        PermChannelRemove,
		Description: "Remove people from a channel",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermChannelRead: {
		Name:        PermChannelRead,
		Description: "Read a channel's messages",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermChannelPost: {
		Name:        PermChannelPost,
		Description: "Post messages in a channel",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermMessageEditAny: {
		Name:        PermMessageEditAny,
		Description: "Edit messages written by other people",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermMessageDeleteAny: {
		Name:        PermMessageDeleteAny,
		Description: "Delete messages written by other people",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermMessagePin: {
		Name:        PermMessagePin,
		Description: "Pin and unpin messages",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermAttachmentUpload: {
		Name:        PermAttachmentUpload,
		Description: "Attach files to messages",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermModerationReview: {
		Name:        PermModerationReview,
		Description: "Review reported content and resolve the moderation queue",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermModerationRestrict: {
		Name:        PermModerationRestrict,
		Description: "Restrict a person's ability to post",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermCallStart: {
		Name:        PermCallStart,
		Description: "Start a voice or video call",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermCallJoin: {
		Name:        PermCallJoin,
		Description: "Join a voice or video call",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
	PermCallLink: {
		Name:        PermCallLink,
		Description: "Make links that let people into a conversation's call without joining the conversation",
		Scopes:      []Scope{ScopeServer, ScopeChannel},
	},
}

// Lookup returns the definition of a permission and whether it is known.
func Lookup(permission Permission) (Definition, bool) {
	definition, ok := catalogue[permission]
	return definition, ok
}

// Catalogue returns every permission, ordered by name, so an administrator can
// see what exists and what each allows.
func Catalogue() []Definition {
	definitions := make([]Definition, 0, len(catalogue))
	for _, definition := range catalogue {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

// GrantableAt reports whether a permission may be granted at the given scope.
//
// This is what stops a channel-scoped role from carrying server administration:
// the grant is refused at the point it is written, rather than being stored and
// silently ignored at evaluation.
func GrantableAt(permission Permission, scope Scope) bool {
	definition, ok := catalogue[permission]
	if !ok {
		return false
	}
	for _, allowed := range definition.Scopes {
		if allowed == scope {
			return true
		}
	}
	return false
}

// IsAdministrative reports whether a permission confers power over the server
// itself. The administrator floor uses this to refuse a change that would
// leave nobody able to administer the server.
func IsAdministrative(permission Permission) bool {
	definition, ok := catalogue[permission]
	return ok && definition.Administrative
}
