package authz

/*
Permissions for events.

None is administrative: they govern gatherings, not the server.
*/
const (
	/*
		PermEventCreate is scheduling an event: across the server when granted
		there, or in one channel. Held by the Member role, as channel.create
		is — planning a meetup is what members of a community do.
	*/
	PermEventCreate Permission = "event.create"

	/*
		PermEventManageAny is acting on events somebody else made: editing,
		cancelling and deleting them, inviting to them, and seeing everybody
		who is going. Nobody holds it until it is given.
	*/
	PermEventManageAny Permission = "event.manage.any"

	/*
		PermEventCalendarsManage is subscribing the server, or a channel, to
		a calendar published elsewhere. Nobody holds it until it is given:
		it makes the server fetch an address on a schedule.
	*/
	PermEventCalendarsManage Permission = "event.calendars.manage"
)

func init() {
	define(map[Permission]Definition{
		PermEventCreate: {
			Name:        PermEventCreate,
			Description: "Schedule events — meetups, talks, workshops, office hours — for the whole server or in a channel",
			Scopes:      []Scope{ScopeServer, ScopeChannel},
		},
		PermEventManageAny: {
			Name:        PermEventManageAny,
			Description: "Edit, cancel and delete other people's events, invite to them, and see everybody going",
			Scopes:      []Scope{ScopeServer, ScopeChannel},
		},
		PermEventCalendarsManage: {
			Name:        PermEventCalendarsManage,
			Description: "Subscribe the server or a channel to a calendar published elsewhere, which this server then fetches on a schedule",
			Scopes:      []Scope{ScopeServer, ScopeChannel},
		},
	})
}
