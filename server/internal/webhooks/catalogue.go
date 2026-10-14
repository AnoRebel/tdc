package webhooks

import (
	"slices"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
What a subscription can listen to: one channel, one project, or the server.

A subscription to a channel or a project names the room it listens to — a
project's history is its stream, which is a room like any other. A subscription
to the server names none, and hears an event wherever its owner may read the
room it happened in, at the moment it happens and again at the moment it is
sent.
*/
const (
	ScopeChannel = "channel"
	ScopeProject = "project"
	ScopeServer  = "server"
)

/*
EventType is one thing a subscription may ask to be told about.

Groups are how a list of several dozen is offered to a person choosing: what it
is about, before what happened to it. A group is a name and nothing more, so a
feature that adds events of its own — meetups, say — adds a group by naming it.
*/
type EventType struct {
	Name  string `json:"name"`
	Group string `json:"group"`
	// Scopes are where it can be asked for. An event that happens to no room
	// in particular — a new account — only at the server's.
	Scopes []string `json:"scopes"`
	/*
		Permission is what the subscription's owner must hold, server-wide, to
		be told about an event that belongs to no room. Empty for an event in a
		room, which is guarded by whether the owner may read that room.
	*/
	Permission string `json:"permission,omitempty"`
	// Derived marks a type the server names by reading another event: a
	// comment is a message in an issue's discussion.
	Derived bool `json:"derived,omitempty"`
}

// Event groups.
const (
	GroupMessages  = "messages"
	GroupMembers   = "members"
	GroupChannels  = "channels"
	GroupCalls     = "calls"
	GroupDirectory = "directory"
	GroupProjects  = "projects"
	GroupIssues    = "issues"
	GroupPlanning  = "planning"
	GroupPeople    = "people"
)

// Event types the server names itself, by reading another event.
const (
	// TypeIssueCommented is a message in an issue's discussion.
	TypeIssueCommented = "issue.commented"
	// TypeIssueClosed is an issue moved into a finishing state from one that
	// is not.
	TypeIssueClosed = "issue.closed"
	// TypeIssueReopened is an issue moved out of a finishing state.
	TypeIssueReopened = "issue.reopened"
)

// Event types that belong to no room.
const (
	TypeAccountCreated     = "account.created"
	TypeAccountRoleChanged = "account.role_changed"
)

var (
	inRooms    = []string{ScopeChannel, ScopeServer}
	inProjects = []string{ScopeProject, ScopeServer}
	onServer   = []string{ScopeServer}
)

func types(group string, scopes []string, names ...events.Type) []EventType {
	out := make([]EventType, 0, len(names))
	for _, name := range names {
		out = append(out, EventType{Name: string(name), Group: group, Scopes: scopes})
	}
	return out
}

/*
Catalogue is every event type a subscription may ask for, in the order a
person choosing reads them.

Everything here is something an owner able to read the room it happened in
already sees there — or, for the two that belong to no room, something their
permission already lets them read through the interface. A subscription is
never a way to learn what its owner could not.
*/
var Catalogue = slices.Concat(
	types(GroupMessages, inRooms,
		events.TypeMessageSent, events.TypeMessageEdited, events.TypeMessageDeleted,
		events.TypeMessageUpdated, events.TypeActionPressed),
	types(GroupMembers, inRooms,
		events.TypeMemberJoined, events.TypeMemberLeft, events.TypeMemberRemoved),
	types(GroupChannels, onServer, events.TypeRoomCreated),
	types(GroupChannels, inRooms,
		events.TypeRoomRenamed, events.TypeRoomArchived, events.TypeRoomRestored,
		events.TypeRoomVisibilityChanged),
	types(GroupCalls, inRooms, events.TypeCallStarted, events.TypeCallEnded),
	types(GroupDirectory, inRooms, "engagement.changed"),
	types(GroupProjects, onServer, events.TypeProjectCreated),
	types(GroupProjects, inProjects,
		events.TypeProjectUpdated, events.TypeProjectMemberAdded, events.TypeProjectMemberRemoved,
		events.TypeProjectStateAdded, events.TypeProjectStateChanged, events.TypeProjectStateRemoved),
	types(GroupIssues, inProjects,
		events.TypeIssueCreated, events.TypeIssueTitleChanged, events.TypeIssueDescriptionChanged,
		events.TypeIssueStateChanged),
	[]EventType{
		{Name: TypeIssueClosed, Group: GroupIssues, Scopes: inProjects, Derived: true},
		{Name: TypeIssueReopened, Group: GroupIssues, Scopes: inProjects, Derived: true},
		{Name: TypeIssueCommented, Group: GroupIssues, Scopes: inProjects, Derived: true},
	},
	types(GroupIssues, inProjects,
		events.TypeIssueAssigned, events.TypeIssueUnassigned, events.TypeIssueLabeled,
		events.TypeIssueUnlabeled, events.TypeIssuePriorityChanged, events.TypeIssueRelated,
		events.TypeIssueUnrelated, events.TypeIssueDeleted),
	types(GroupPlanning, inProjects,
		events.TypeIssueCycleChanged, events.TypeIssueMilestoneChanged,
		events.TypeProjectCycleAdded, events.TypeProjectCycleChanged, events.TypeProjectCycleEnded,
		events.TypeProjectCycleRemoved, events.TypeProjectMilestoneAdded,
		events.TypeProjectMilestoneChanged, events.TypeProjectMilestoneRemoved),
	[]EventType{
		{Name: TypeAccountCreated, Group: GroupPeople, Scopes: onServer, Permission: string(authz.PermUserViewAll)},
		{Name: TypeAccountRoleChanged, Group: GroupPeople, Scopes: onServer, Permission: string(authz.PermUserViewAll)},
	},
)

/*
EventTypes are the names in the catalogue, for clients written before it
described them.
*/
var EventTypes = names()

func names() []string {
	out := make([]string, 0, len(Catalogue))
	for _, kind := range Catalogue {
		out = append(out, kind.Name)
	}
	return out
}

// Lookup returns a type's definition.
func Lookup(name string) (EventType, bool) {
	index := slices.IndexFunc(Catalogue, func(kind EventType) bool { return kind.Name == name })
	if index < 0 {
		return EventType{}, false
	}
	return Catalogue[index], true
}
