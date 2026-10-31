package calendar

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// Answered is what answering an event did.
type Answered struct {
	// Status is the answer as recorded: "going" to a full event is
	// "waitlisted".
	Status string
	// Position is a waiting person's place in the queue, from one.
	Position int64
	// Log is the entry recording the answer, absent for somebody who keeps
	// their attendance to themselves.
	Log *dbgen.Event
	// Promoted are people moved up from the waiting list by this answer.
	Promoted []uuid.UUID
}

/*
Answer records somebody's answer to an event they can see.

In one transaction holding the event's row, so two people answering for the
last place serialise: the second finds it taken and waits. Somebody leaving
"going" frees a place, which the first person waiting takes in the same
transaction.
*/
func (s *Service) Answer(ctx context.Context, actor dbgen.User, viewer Viewer, id uuid.UUID, status string) (Answered, error) {
	if !slices.Contains([]string{Going, Maybe, Declined}, status) {
		return Answered{}, invalid("an answer is going, maybe or declined")
	}
	if _, err := s.Get(ctx, viewer, id); err != nil {
		return Answered{}, err
	}

	var result Answered
	err := s.inTransaction(ctx, func(q *dbgen.Queries) error {
		event, err := q.LockCalendarEvent(ctx, id)
		if err != nil {
			return fmt.Errorf("lock event: %w", err)
		}
		if event.CancelledAt != nil {
			return ErrCancelled
		}
		if event.LastEndsAt != nil && event.LastEndsAt.Before(time.Now()) {
			return ErrEnded
		}

		previous := ""
		if answer, err := q.GetCalendarRSVP(ctx, dbgen.GetCalendarRSVPParams{EventID: id, UserID: actor.ID}); err == nil {
			previous = answer.Status
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read answer: %w", err)
		}

		recorded := status
		if status == Going && previous != Going && event.Capacity != nil && !viewer.Hosting[id] {
			going, err := q.CountCalendarGoing(ctx, id)
			if err != nil {
				return fmt.Errorf("count places: %w", err)
			}
			if going >= int64(*event.Capacity) {
				recorded = Waitlisted
			}
		}
		// Somebody already waiting who asks again keeps their place.
		if recorded == Waitlisted && previous == Waitlisted {
			recorded = previous
		}
		if err := q.SetCalendarRSVP(ctx, dbgen.SetCalendarRSVPParams{EventID: id, UserID: actor.ID, Status: recorded}); err != nil {
			return fmt.Errorf("record answer: %w", err)
		}
		if err := s.syncMember(ctx, q, id, actor.ID); err != nil {
			return err
		}
		result.Status = recorded
		if recorded == Waitlisted {
			if result.Position, err = q.WaitlistPosition(ctx, dbgen.WaitlistPositionParams{EventID: id, UserID: actor.ID}); err != nil {
				return fmt.Errorf("read place in queue: %w", err)
			}
		}

		if previous == Going && recorded != Going {
			if result.Promoted, err = s.promote(ctx, q, event); err != nil {
				return err
			}
		}

		if previous == recorded {
			return nil
		}
		discloses, err := q.ShowsEventAttendance(ctx, actor.ID)
		if err != nil {
			return fmt.Errorf("read attendance setting: %w", err)
		}
		if !discloses {
			return nil
		}
		room, err := q.GetRoomByID(ctx, id)
		if err != nil {
			return fmt.Errorf("read the event's conversation: %w", err)
		}
		entry, err := s.append(ctx, q, room, &actor, events.TypeEventRSVP, map[string]any{
			"event": id.String(), "title": event.Title, "status": recorded,
		})
		if err != nil {
			return err
		}
		result.Log = &entry
		return nil
	})
	if err != nil {
		return Answered{}, err
	}
	return result, nil
}

/*
promote fills free places from the waiting list, first come first served, and
returns who was moved up. Inside the caller's transaction, which holds the
event's row.
*/
func (s *Service) promote(ctx context.Context, q *dbgen.Queries, event dbgen.CalendarEvent) ([]uuid.UUID, error) {
	var promoted []uuid.UUID
	for {
		if event.Capacity != nil {
			going, err := q.CountCalendarGoing(ctx, event.ID)
			if err != nil {
				return nil, fmt.Errorf("count places: %w", err)
			}
			if going >= int64(*event.Capacity) {
				return promoted, nil
			}
		}
		next, err := q.FirstWaitlisted(ctx, event.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return promoted, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read the waiting list: %w", err)
		}
		if err := q.SetCalendarRSVP(ctx, dbgen.SetCalendarRSVPParams{EventID: event.ID, UserID: next, Status: Going}); err != nil {
			return nil, fmt.Errorf("move up: %w", err)
		}
		if err := s.syncMember(ctx, q, event.ID, next); err != nil {
			return nil, err
		}
		promoted = append(promoted, next)
	}
}

// Counts are how many people gave each answer.
type Counts struct {
	Going, Maybe, Declined, Waitlisted int64
}

// CountsFor reads how an event has been answered.
func (s *Service) CountsFor(ctx context.Context, id uuid.UUID) (Counts, error) {
	rows, err := s.queries.CountCalendarRSVPs(ctx, id)
	if err != nil {
		return Counts{}, fmt.Errorf("count answers: %w", err)
	}
	var counts Counts
	for _, row := range rows {
		switch row.Status {
		case Going:
			counts.Going = row.People
		case Maybe:
			counts.Maybe = row.People
		case Declined:
			counts.Declined = row.People
		case Waitlisted:
			counts.Waitlisted = row.People
		}
	}
	return counts, nil
}

// MyAnswer is somebody's own answer, and their place if waiting.
func (s *Service) MyAnswer(ctx context.Context, id, userID uuid.UUID) (string, int64) {
	answer, err := s.queries.GetCalendarRSVP(ctx, dbgen.GetCalendarRSVPParams{EventID: id, UserID: userID})
	if err != nil {
		return "", 0
	}
	if answer.Status != Waitlisted {
		return answer.Status, 0
	}
	position, _ := s.queries.WaitlistPosition(ctx, dbgen.WaitlistPositionParams{EventID: id, UserID: userID})
	return answer.Status, position
}

// Attendee is one person who answered, as somebody else is shown them.
type Attendee struct {
	UserID      uuid.UUID
	Username    string
	DisplayName string
	Status      string
}

// Attendance is who answered, with those who keep it to themselves counted
// rather than named.
type Attendance struct {
	Named []Attendee
	// Unnamed counts, by answer, the people not shown.
	Unnamed map[string]int
}

/*
Attendees lists who answered an event, as the viewer may be shown them.

Hosts, speakers, the creator and people who manage events there see everybody:
running an event means knowing who is coming. Anybody else sees the people who
let others see the events they go to, and a count of the rest. Everybody sees
themselves.
*/
func (s *Service) Attendees(ctx context.Context, viewer Viewer, event dbgen.CalendarEvent) (Attendance, error) {
	rows, err := s.queries.ListCalendarAttendees(ctx, event.ID)
	if err != nil {
		return Attendance{}, fmt.Errorf("read attendees: %w", err)
	}
	everything := viewer.MayEdit(event)
	attendance := Attendance{Named: []Attendee{}, Unnamed: map[string]int{}}
	for _, row := range rows {
		if row.Status == Declined && !everything && row.UserID != viewer.UserID {
			// Who is not coming is the organisers' business.
			continue
		}
		if everything || row.Discloses || row.UserID == viewer.UserID {
			attendance.Named = append(attendance.Named, Attendee{
				UserID: row.UserID, Username: row.Username, DisplayName: row.DisplayName, Status: row.Status,
			})
			continue
		}
		attendance.Unnamed[row.Status]++
	}
	return attendance, nil
}

// Recipients are the people who hear about an event's occurrences: going,
// maybe going, hosting and speaking.
func (s *Service) Recipients(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return s.queries.ListCalendarRecipients(ctx, id)
}

// Invited is what inviting did: who should be told.
type Invited struct {
	People []uuid.UUID
	// Channels invited, whose members should be told.
	Channels []uuid.UUID
	Invites  []dbgen.CalendarInvite
}

/*
Invite asks people and channels to an event, for somebody who may edit it.

A channel is invited only by somebody in it, so an invitation cannot be the
way to learn a private channel exists. People must have accounts here;
anybody already invited is skipped.
*/
func (s *Service) Invite(ctx context.Context, actor dbgen.User, viewer Viewer, id uuid.UUID, people, channels []uuid.UUID) (Invited, error) {
	event, err := s.Get(ctx, viewer, id)
	if err != nil {
		return Invited{}, err
	}
	if !viewer.MayEdit(event.CalendarEvent) {
		return Invited{}, ErrNotAllowed
	}
	if event.CancelledAt != nil {
		return Invited{}, ErrCancelled
	}
	if len(people)+len(channels) > 200 {
		return Invited{}, invalid("at most 200 invitations at once")
	}
	for _, channel := range channels {
		if _, err := s.channelFor(ctx, channel, viewer); err != nil {
			return Invited{}, err
		}
	}
	var result Invited
	for _, person := range people {
		if person == actor.ID {
			continue
		}
		invite, err := s.queries.CreateCalendarInvite(ctx, dbgen.CreateCalendarInviteParams{
			ID: uuid.New(), EventID: id, UserID: &person, InvitedBy: &actor.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, invalid("everybody invited must have an account here")
		}
		result.People = append(result.People, person)
		result.Invites = append(result.Invites, invite)
	}
	for _, channel := range channels {
		invite, err := s.queries.CreateCalendarInvite(ctx, dbgen.CreateCalendarInviteParams{
			ID: uuid.New(), EventID: id, ChannelID: &channel, InvitedBy: &actor.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, fmt.Errorf("invite channel: %w", err)
		}
		result.Channels = append(result.Channels, channel)
		result.Invites = append(result.Invites, invite)
	}
	return result, nil
}

// Invitations lists who an event's organisers have invited.
func (s *Service) Invitations(ctx context.Context, viewer Viewer, id uuid.UUID) ([]dbgen.ListCalendarInvitesRow, error) {
	event, err := s.Get(ctx, viewer, id)
	if err != nil {
		return nil, err
	}
	if !viewer.MayEdit(event.CalendarEvent) {
		return nil, ErrNotAllowed
	}
	return s.queries.ListCalendarInvites(ctx, id)
}

// Uninvite withdraws one invitation. Somebody who already answered keeps
// their answer, and so keeps seeing the event.
func (s *Service) Uninvite(ctx context.Context, viewer Viewer, id, inviteID uuid.UUID) error {
	event, err := s.Get(ctx, viewer, id)
	if err != nil {
		return err
	}
	if !viewer.MayEdit(event.CalendarEvent) {
		return ErrNotAllowed
	}
	removed, err := s.queries.DeleteCalendarInvite(ctx, dbgen.DeleteCalendarInviteParams{ID: inviteID, EventID: id})
	if err != nil {
		return fmt.Errorf("withdraw invitation: %w", err)
	}
	if removed == 0 {
		return ErrNotFound
	}
	return nil
}
