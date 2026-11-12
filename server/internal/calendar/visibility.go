package calendar

import (
	"context"
	"fmt"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Viewer is who is asking, with the facts that decide what of the calendar they
may see.

Loaded once per request — the channels they are in, the events they host,
answered or were invited to — so deciding about a thousand events is a
thousand lookups in memory rather than a thousand queries, and so the list,
the event page, the feed and the reminder pass all decide by the same rule.
*/
type Viewer struct {
	UserID      uuid.UUID
	Permissions authz.Permissions
	// Channels they are a member of.
	Channels map[uuid.UUID]bool
	// Hosting are events they host or speak at.
	Hosting map[uuid.UUID]bool
	// Invited are events they were invited to, directly or through a
	// channel, or have answered.
	Invited map[uuid.UUID]bool
}

// LoadViewer gathers the facts about somebody that decide what they see.
func (s *Service) LoadViewer(ctx context.Context, userID uuid.UUID, permissions authz.Permissions) (Viewer, error) {
	viewer := Viewer{
		UserID: userID, Permissions: permissions,
		Channels: map[uuid.UUID]bool{}, Hosting: map[uuid.UUID]bool{}, Invited: map[uuid.UUID]bool{},
	}
	rooms, err := s.queries.ListRoomsUserIsIn(ctx, userID)
	if err != nil {
		return Viewer{}, fmt.Errorf("read channels: %w", err)
	}
	for _, room := range rooms {
		viewer.Channels[room.ID] = true
	}
	involved, err := s.queries.CalendarEventsInvolving(ctx, userID)
	if err != nil {
		return Viewer{}, fmt.Errorf("read involvement: %w", err)
	}
	for _, row := range involved {
		if row.How == "host" {
			viewer.Hosting[row.EventID] = true
		} else {
			viewer.Invited[row.EventID] = true
		}
	}
	return viewer, nil
}

/*
Manages reports whether somebody may act on any event where this one is: with
event.manage.any in its channel, or across the server for one with no channel.
*/
func (v Viewer) Manages(event dbgen.CalendarEvent) bool {
	if event.ChannelID != nil {
		return v.Permissions.AllowsChannel(authz.PermEventManageAny, *event.ChannelID).Allowed
	}
	return v.Permissions.AllowsServer(authz.PermEventManageAny).Allowed
}

// Runs reports whether somebody made or hosts an event.
func (v Viewer) Runs(event dbgen.CalendarEvent) bool {
	return (event.CreatedBy != nil && *event.CreatedBy == v.UserID) || v.Hosting[event.ID]
}

// MayEdit is whether somebody may change, cancel and invite to an event.
func (v Viewer) MayEdit(event dbgen.CalendarEvent) bool {
	return v.Runs(event) || v.Manages(event)
}

// MayDelete is whether somebody may remove an event altogether: who made it,
// or somebody who manages events there.
func (v Viewer) MayDelete(event dbgen.CalendarEvent) bool {
	return (event.CreatedBy != nil && *event.CreatedBy == v.UserID) || v.Manages(event)
}

/*
Sees decides whether somebody may see an event. The one rule, asked by every
route, feed and pass.

A public event in a channel that has since been made private is treated as the
channel's: public was allowed only because the channel was, and its name is
not to be disclosed now that it is not.
*/
func (v Viewer) Sees(event Listed) bool {
	if v.Runs(event.CalendarEvent) || v.Manages(event.CalendarEvent) {
		return true
	}
	visibility := event.Visibility
	if visibility == VisibilityPublic && event.ChannelID != nil && event.ChannelVisibility != "public" {
		visibility = VisibilityChannel
	}
	switch visibility {
	case VisibilityPublic:
		return true
	case VisibilityChannel:
		return event.ChannelID != nil && v.Channels[*event.ChannelID] &&
			v.Permissions.AllowsChannel(authz.PermChannelRead, *event.ChannelID).Allowed
	case VisibilityInvite:
		return v.Invited[event.ID]
	}
	return false
}
