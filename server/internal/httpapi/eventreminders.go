package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/AnoRebel/tdc/server/internal/calendar"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

// reminderInterval is how often occurrences coming up are looked for.
const reminderInterval = time.Minute

// RunEventReminders reminds people of events they are going to, until ctx ends.
func (s *Server) RunEventReminders(ctx context.Context) {
	if s.calendar == nil {
		return
	}
	ticker := time.NewTicker(reminderInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RemindOfEvents(ctx, time.Now())
		}
	}
}

/*
RemindOfEvents makes one pass, as of now, and returns how many reminders it
sent. Exported so a test can make a pass at a moment it chooses.

For each occurrence starting within a lead time, the shortest lead that covers
it is the one sent — a server that was down for a day sends one reminder, not
a day's worth — and a lead is skipped when the event was scheduled after it
would have fallen, since somebody who has just been told about an event needs
no reminder of it yet. The claim is a row only one node can insert, so a
reminder goes out once however many nodes look.
*/
func (s *Server) RemindOfEvents(ctx context.Context, now time.Time) int {
	if s.calendar == nil || !s.settings.Enabled(ctx, settings.FeatureEvents) {
		return 0
	}
	leads := settings.ReminderLeads(s.settings.Get(ctx, settings.EventsReminderMinutes))
	if len(leads) == 0 {
		return 0
	}
	slices.Sort(leads)
	horizon := time.Duration(leads[len(leads)-1]) * time.Minute

	candidates, err := s.calendar.Queries().ListCalendarEventsForReminders(ctx, now.Add(horizon))
	if err != nil {
		s.log.Warn("could not look for events to remind people of", "error", err)
		return 0
	}
	sent := 0
	for _, event := range candidates {
		occurrences, err := calendar.SeriesOf(event).Between(now, now.Add(horizon))
		if err != nil {
			continue
		}
		for _, occurrence := range occurrences {
			if !occurrence.Start.After(now) {
				continue
			}
			for _, lead := range leads {
				before := time.Duration(lead) * time.Minute
				if occurrence.Start.After(now.Add(before)) {
					continue
				}
				// The shortest covering lead only; and not one that fell
				// before anybody knew of the event.
				if !occurrence.Start.Add(-before).Before(event.CreatedAt) {
					if s.remind(ctx, event, occurrence, lead) {
						sent++
					}
				}
				break
			}
		}
	}
	return sent
}

func (s *Server) remind(ctx context.Context, event dbgen.CalendarEvent, occurrence calendar.Occurrence, lead int) bool {
	claimed, err := s.calendar.Queries().ClaimCalendarReminder(ctx, dbgen.ClaimCalendarReminderParams{
		EventID: event.ID, Occurrence: occurrence.Start, LeadMinutes: int32(lead),
	})
	if err != nil || claimed == 0 {
		return false
	}
	room, err := s.rooms.ByID(ctx, event.ID)
	if err != nil {
		return false
	}
	content, _ := json.Marshal(map[string]any{
		"event": event.ID.String(), "title": event.Title,
		"occurrence": rfc3339(occurrence.Start), "minutes": lead,
	})
	entry, err := s.rooms.AppendIn(ctx, s.queries, room, nil, s.rooms.ServerName(), events.TypeEventReminder, content)
	if err != nil {
		s.log.Warn("could not record a reminder", "event", event.ID, "error", err)
		return false
	}
	s.rooms.Told(ctx, entry)
	s.publishEvent(ctx, entry)

	people := s.recipientsOf(ctx, event.ID)
	body := fmt.Sprintf("Starts %s", whenIn(occurrence.Start, event.TimeZone))
	s.tell(ctx, eventNotice{
		event: event, reason: notify.ReasonEventReminder, entry: entry.ID,
		pushTitle: event.Title, pushBody: body,
	}, people)

	// And a live frame, which an open client shows at once and the desktop
	// application turns into a banner when its window is hidden.
	if s.hub != nil {
		payload, _ := json.Marshal(map[string]any{
			"event": event.ID.String(), "title": event.Title, "startsAt": rfc3339(occurrence.Start),
			"minutes": lead, "online": event.Online,
		})
		frame := realtime.Frame{Type: realtime.FrameEventReminder, RoomID: event.ID.String(), Payload: payload}
		for _, person := range people {
			if s.notify != nil && s.notify.Muted(ctx, person, event.ID) {
				continue
			}
			here := s.hub.SendToUser(person, frame)
			s.whisperElsewhere(ctx, person, frame, here)
		}
	}
	return true
}
