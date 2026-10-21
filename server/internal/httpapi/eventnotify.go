package httpapi

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/calendar"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/push"
)

/*
Telling people about events.

Through the notification system every other notice uses: a notification points
at the event's conversation and an entry in its log, so it is muted when the
event is muted, appears in the list and in email summaries, and opens the
event. Web push goes with the ones that are worth interrupting somebody for.
*/

// eventNotice is one thing to tell people about an event.
type eventNotice struct {
	event  dbgen.CalendarEvent
	reason notify.Reason
	// entry is the log entry the notification points at.
	entry string
	// actor did it; uuid.Nil and an empty address for the server itself.
	actorID      uuid.UUID
	actorAddress string
	// push also reaches devices with the application closed, with this
	// title and body.
	pushTitle, pushBody string
}

// tell raises the notice for each person, in the background.
func (s *Server) tell(ctx context.Context, notice eventNotice, people []uuid.UUID) {
	if s.notify == nil || len(people) == 0 {
		return
	}
	if notice.actorAddress == "" {
		notice.actorAddress = s.rooms.ServerName()
	}
	s.inBackground(ctx, "event notifications", func(ctx context.Context) {
		for _, person := range people {
			if person == notice.actorID {
				continue
			}
			s.notify.About(ctx, person, notice.actorID, notice.actorAddress,
				notice.event.ID, notice.entry, notice.reason, notice.event.Title)
			if notice.pushTitle != "" && !s.notify.Muted(ctx, person, notice.event.ID) {
				s.pushEvent(person, notice.event, notice.pushTitle, notice.pushBody)
			}
		}
	})
}

// pushEvent reaches somebody's devices about an event; opening it opens the
// event, through its conversation.
func (s *Server) pushEvent(person uuid.UUID, event dbgen.CalendarEvent, title, body string) {
	if s.push == nil || !s.push.Enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.push.Send(ctx, person, push.Payload{Title: title, Body: body, Room: event.ID.String()})
	}()
}

// anchorOf is the entry an event began with, which notices about the event as
// a whole point at.
func (s *Server) anchorOf(ctx context.Context, eventID uuid.UUID) string {
	id, err := s.queries.CalendarAnchorEvent(ctx, eventID)
	if err != nil {
		return ""
	}
	return id
}

func (s *Server) recipientsOf(ctx context.Context, eventID uuid.UUID) []uuid.UUID {
	people, err := s.calendar.Recipients(ctx, eventID)
	if err != nil {
		s.log.Warn("could not read who is going to an event", "event", eventID, "error", err)
	}
	return people
}

// afterEventChanged tells the people going about a change to when or where,
// and anybody a larger capacity moved up from the waiting list.
func (s *Server) afterEventChanged(ctx context.Context, actor dbgen.User, changed calendar.Changed) {
	event := changed.Event.CalendarEvent
	if changed.Material && event.CancelledAt == nil {
		s.tell(ctx, eventNotice{
			event: event, reason: notify.ReasonEventChanged, entry: changed.Log.ID,
			actorID: actor.ID, actorAddress: s.rooms.UserAddress(actor.Username),
			pushTitle: event.Title, pushBody: "The time or place has changed",
		}, s.recipientsOf(ctx, event.ID))
	}
	s.tellPromoted(ctx, event, changed.Promoted)
}

// afterEventCancelled tells the people going that it is off.
func (s *Server) afterEventCancelled(ctx context.Context, actor dbgen.User, cancelled calendar.Cancelled) {
	event := cancelled.Event.CalendarEvent
	body := "Cancelled"
	if event.CancelReason != "" {
		body = "Cancelled: " + event.CancelReason
	}
	s.tell(ctx, eventNotice{
		event: event, reason: notify.ReasonEventCancelled, entry: cancelled.Log.ID,
		actorID: actor.ID, actorAddress: s.rooms.UserAddress(actor.Username),
		pushTitle: event.Title, pushBody: body,
	}, s.recipientsOf(ctx, event.ID))
}

// tellPromoted tells people moved up from the waiting list that they have a
// place now.
func (s *Server) tellPromoted(ctx context.Context, event dbgen.CalendarEvent, promoted []uuid.UUID) {
	if len(promoted) == 0 {
		return
	}
	s.tell(ctx, eventNotice{
		event: event, reason: notify.ReasonEventPromoted, entry: s.anchorOf(ctx, event.ID),
		pushTitle: event.Title, pushBody: "A place freed up — you are going",
	}, promoted)
}

// whenIn says when something starts in its own zone, for a line of text.
func whenIn(at time.Time, zone string) string {
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.UTC
	}
	return at.In(location).Format("Mon 2 Jan 15:04 MST")
}

/*
eventChannelFor is the channel whose webhooks should hear about an entry in an
event's conversation: the event's channel, unless the event is invite-only —
a channel's subscriber may not be somebody the event was shown to.
*/
func (s *Server) eventChannelFor(ctx context.Context, entry dbgen.Event) (uuid.UUID, bool) {
	if s.calendar == nil || !strings.HasPrefix(entry.Type, "event.") {
		return uuid.Nil, false
	}
	event, err := s.calendar.Queries().GetCalendarEvent(ctx, entry.RoomID)
	if err != nil || event.ChannelID == nil || event.Visibility == calendar.VisibilityInvite {
		return uuid.Nil, false
	}
	return *event.ChannelID, true
}
