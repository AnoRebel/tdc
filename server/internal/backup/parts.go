/*
Package backup takes a server, or part of it, into an encrypted archive, and
puts it back.

The archive is this package's own format rather than a pg_dump: the server
image carries no pg_dump, a dump is tied to the Postgres major version, it
cannot be cut by channel, and a data-only dump only restores across foreign
keys for a superuser. What this writes is a tar stream — a manifest, one
newline-delimited JSON file per table, the attachments' bytes — compressed and
then encrypted with age, so the standard age tool opens it without TDC. See
docs/hosting.md for the format as an operator reads it.

Everything about tables is read from Postgres's catalogue at run time —
columns, keys, references — so a migration adding a column needs nothing here.
What does need saying here is which part a new table belongs to, and a test
fails until somebody says it.
*/
package backup

import (
	"fmt"
	"slices"
	"sort"

	"github.com/google/uuid"
)

// Part is a named set of tables an administrator can choose to back up.
type Part string

const (
	PartSettings     Part = "settings"
	PartAccounts     Part = "accounts"
	PartChannels     Part = "channels"
	PartFiles        Part = "files"
	PartProjects     Part = "projects"
	PartDirectory    Part = "directory"
	PartIntegrations Part = "integrations"
	PartAudit        Part = "audit"
)

// Parts lists every part in the order they are written and restored: each
// after everything it depends on.
var Parts = []Part{
	PartSettings, PartAccounts, PartChannels, PartFiles,
	PartProjects, PartDirectory, PartIntegrations, PartAudit,
}

// Known reports whether a part exists.
func Known(part Part) bool { return slices.Contains(Parts, part) }

/*
dependencies are the parts a part cannot be restored without.

A channel's messages name their senders; a project is a room; a directory
engagement names a project and a room. Choosing a part chooses these, so an
archive is always one that restores into an empty database.
*/
var dependencies = map[Part][]Part{
	PartChannels:     {PartAccounts},
	PartFiles:        {PartChannels},
	PartProjects:     {PartAccounts, PartChannels},
	PartDirectory:    {PartAccounts, PartChannels, PartProjects},
	PartIntegrations: {PartAccounts, PartChannels},
	PartAudit:        {PartAccounts},
}

/*
needsEveryChannel marks parts that reference rooms they do not own.

A project and each of its issues is a room, an integration is attached to
channels, and an engagement may have a conversation. With only some channels in
an archive those references would point at nothing, so these parts refuse a
channel selection rather than write an archive that cannot be restored.
*/
var needsEveryChannel = map[Part]bool{
	PartProjects:     true,
	PartDirectory:    true,
	PartIntegrations: true,
}

// Dependencies returns the parts a part needs, for a client to show.
func Dependencies(part Part) []Part { return slices.Clone(dependencies[part]) }

// NeedsEveryChannel reports whether a part refuses a channel selection.
func NeedsEveryChannel(part Part) bool { return needsEveryChannel[part] }

/*
tableSpec is one table in a part, and how a channel selection narrows it.

roomColumn names the column holding the room, for a table that has one.
Otherwise parentColumn and parentTable say the row belongs to a room through
another table that has one — a view of a message, the use of an invitation.
A table with neither is not narrowed.
*/
type tableSpec struct {
	name         string
	roomColumn   string
	parentColumn string
	parentTable  string
}

func roomScoped(name string) tableSpec { return tableSpec{name: name, roomColumn: "room_id"} }

func childOf(name, column, parent string) tableSpec {
	return tableSpec{name: name, parentColumn: column, parentTable: parent}
}

// partTables says which tables make up each part. The files part has none:
// it is the bytes the channels part's attachment rows name.
var partTables = map[Part][]tableSpec{
	PartSettings: {
		{name: "server_config"},
		{name: "server_statuses"},
	},
	PartAccounts: {
		{name: "users"},
		{name: "credentials"},
		{name: "roles"},
		{name: "role_permissions"},
		{name: "user_roles"},
		{name: "user_privacy"},
		{name: "translation_preferences"},
		{name: "automatic_replies"},
		{name: "calendar_feeds"},
		{name: "username_history"},
		{name: "user_blocks"},
		{name: "oauth_authorizations"},
		{name: "push_subscriptions"},
		{name: "notification_digests"},
	},
	PartChannels: {
		{name: "rooms", roomColumn: "id"},
		roomScoped("room_members"),
		roomScoped("room_sequences"),
		roomScoped("room_peers"),
		roomScoped("remote_members"),
		roomScoped("events"),
		roomScoped("reactions"),
		roomScoped("pinned_messages"),
		roomScoped("pinned_rooms"),
		roomScoped("message_lifetimes"),
		roomScoped("message_search"),
		roomScoped("message_interactions"),
		roomScoped("action_presses"),
		childOf("message_views", "event_id", "events"),
		childOf("thread_reads", "root_event_id", "events"),
		roomScoped("attachments"),
		roomScoped("notifications"),
		roomScoped("notification_preferences"),
		roomScoped("channel_bans"),
		roomScoped("posting_restrictions"),
		roomScoped("invitations"),
		childOf("invitation_uses", "invitation_id", "invitations"),
		roomScoped("scheduled_messages"),
		roomScoped("reports"),
		roomScoped("calls"),
		childOf("call_participants", "call_id", "calls"),
		roomScoped("call_links"),
		roomScoped("translation_room_rules"),
		// An event is a room of its own, so only some channels carries none.
		tableSpec{name: "calendar_subscriptions", roomColumn: "channel_id"},
		tableSpec{name: "calendar_events", roomColumn: "id"},
		childOf("calendar_event_people", "event_id", "calendar_events"),
		childOf("calendar_sessions", "event_id", "calendar_events"),
		childOf("calendar_rsvps", "event_id", "calendar_events"),
		childOf("calendar_invites", "event_id", "calendar_events"),
		childOf("calendar_reminders", "event_id", "calendar_events"),
	},
	PartProjects: {
		{name: "projects"},
		{name: "project_members"},
		{name: "project_views"},
		{name: "project_labels"},
		{name: "project_priorities"},
		{name: "issue_states"},
		{name: "issues"},
		{name: "issue_search"},
		{name: "issue_assignees"},
		{name: "issue_labels"},
		{name: "issue_relations"},
		{name: "cycles"},
		{name: "issue_cycle"},
		{name: "milestones"},
		{name: "issue_milestone"},
	},
	PartDirectory: {
		{name: "skills"},
		{name: "skill_aliases"},
		{name: "directory_profiles"},
		{name: "profile_skills"},
		{name: "profile_verifications"},
		{name: "portfolio_items"},
		{name: "company_profiles"},
		{name: "company_members"},
		{name: "enquiries"},
		{name: "engagements"},
		{name: "engagement_transitions"},
		{name: "proposal_versions"},
		{name: "proposal_responses"},
		{name: "engagement_agreements"},
		{name: "agreement_revisions"},
		{name: "agreement_revision_outcomes"},
	},
	PartIntegrations: {
		{name: "applications"},
		{name: "application_channels"},
		{name: "api_tokens"},
		{name: "incoming_webhooks"},
		{name: "webhook_subscriptions"},
		{name: "webhook_deliveries"},
		{name: "webhook_attempts"},
		{name: "integration_context"},
	},
	PartAudit: {
		{name: "audit_entries"},
	},
}

/*
excluded are the tables in no part, and why.

Each either lapses on its own within minutes or days, or describes the running
server rather than the community it holds. A replace restore still empties the
transient ones — a session for an account the restore replaced must not
survive it — and never touches the operational ones.
*/
var excluded = map[string]string{
	"sessions":            "signed-in sessions; everybody signs in again after a restore",
	"webauthn_ceremonies": "passkey ceremonies in progress, which expire in minutes",
	"pending_sign_ins":    "sign-ins waiting for a second factor, which expire in minutes",
	"recovery_tokens":     "recovery links, which expire and must not outlive a restore",
	"email_confirmations": "confirmation links, which expire",
	"oauth_outcomes":      "sign-ins through a provider in progress",
	"typing_indicators":   "who is typing, which lapses in seconds",
	"user_presence":       "who is online, which lapses in seconds",
	"event_idempotency":   "retry keys for messages being sent, which lapse",
	"call_invites":        "who was let into a call in progress",
	"call_lobby":          "who is waiting to be let into a call in progress",
	"automatic_reply_log": "who was answered automatically today, which keeps a reply to one a day " +
		"and lapses in a day",
	"message_translations": "translations already made, which are made again when asked for",
	"translation_connections": "keys for outside translation services, sealed with this server's own " +
		"key; carrying them to another server is not this archive's to do, so people connect again",
}

// operational tables are never truncated, read or written by a restore.
var operational = map[string]string{
	"schema_migrations": "the schema's own version",
	"schema_versions":   "which release last ran against this database",
	"backups":           "the archives themselves",
	"operations":        "the record of backups, restores and restarts",
	"server_nodes":      "which processes are running",
	"maintenance":       "whether the server is refusing requests",
}

// Selection is what to back up: parts, and optionally only some channels.
type Selection struct {
	Parts []Part
	// Rooms narrows the channels part. Empty means every room.
	Rooms []uuid.UUID
}

/*
Resolve adds every part the chosen ones depend on, orders them, and refuses a
channel selection a chosen part cannot live with.
*/
func (s Selection) Resolve() (Selection, error) {
	if len(s.Parts) == 0 {
		return Selection{}, fmt.Errorf("%w: choose at least one part", ErrInvalid)
	}

	chosen := map[Part]bool{}
	var add func(Part) error
	add = func(part Part) error {
		if !Known(part) {
			return fmt.Errorf("%w: %q is not a part", ErrInvalid, part)
		}
		if chosen[part] {
			return nil
		}
		chosen[part] = true
		for _, needed := range dependencies[part] {
			if err := add(needed); err != nil {
				return err
			}
		}
		return nil
	}
	for _, part := range s.Parts {
		if err := add(part); err != nil {
			return Selection{}, err
		}
	}

	resolved := Selection{}
	for _, part := range Parts {
		if chosen[part] {
			resolved.Parts = append(resolved.Parts, part)
		}
	}

	if len(s.Rooms) > 0 {
		for _, part := range resolved.Parts {
			if needsEveryChannel[part] {
				return Selection{}, fmt.Errorf(
					"%w: %s needs every channel, so it cannot be backed up with only some", ErrInvalid, part)
			}
		}
		rooms := slices.Clone(s.Rooms)
		sort.Slice(rooms, func(i, j int) bool { return rooms[i].String() < rooms[j].String() })
		resolved.Rooms = slices.Compact(rooms)
	}

	return resolved, nil
}

// Has reports whether a part is selected.
func (s Selection) Has(part Part) bool { return slices.Contains(s.Parts, part) }

// Complete reports whether this is everything: every part and every room.
func (s Selection) Complete() bool {
	return len(s.Rooms) == 0 && len(s.Parts) == len(Parts)
}

// partOf returns the part a table belongs to, if any.
func partOf(table string) (Part, tableSpec, bool) {
	for part, specs := range partTables {
		for _, spec := range specs {
			if spec.name == table {
				return part, spec, true
			}
		}
	}
	return "", tableSpec{}, false
}
