package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// Service keeps events.
type Service struct {
	pool    *db.Pool
	queries *dbgen.Queries
	rooms   *rooms.Service
	log     *slog.Logger
}

// NewService builds the calendar over the pool and the rooms its events are.
func NewService(pool *db.Pool, roomsService *rooms.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, queries: dbgen.New(pool), rooms: roomsService, log: log}
}

// Queries exposes the generated queries, for the HTTP layer's reads.
func (s *Service) Queries() *dbgen.Queries { return s.queries }

func (s *Service) inTransaction(ctx context.Context, work func(*dbgen.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := work(dbgen.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// roomName is what an event's conversation is called: its title, as long as a
// room's name may be.
func roomName(title string) string {
	if utf8.RuneCountInString(title) <= rooms.MaxRoomNameLength {
		return title
	}
	return string([]rune(title)[:rooms.MaxRoomNameLength-1]) + "…"
}

// Summary is what a log entry says about an event: enough for somebody reading
// the conversation, or a webhook receiver, to know what and when.
func Summary(event dbgen.CalendarEvent) map[string]any {
	summary := map[string]any{
		"event": event.ID.String(), "title": event.Title, "kind": event.Kind,
		"startsAt": event.StartsAt.UTC().Format(time.RFC3339), "endsAt": event.EndsAt.UTC().Format(time.RFC3339),
		"timeZone": event.TimeZone, "allDay": event.AllDay, "recurrence": event.Recurrence,
		"location": event.Location, "online": event.Online, "visibility": event.Visibility,
	}
	if event.ChannelID != nil {
		summary["channelId"] = event.ChannelID.String()
	}
	return summary
}

func (s *Service) append(
	ctx context.Context, q *dbgen.Queries, room dbgen.Room, actor *dbgen.User,
	eventType events.Type, content map[string]any,
) (dbgen.Event, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode %s: %w", eventType, err)
	}
	if actor == nil {
		return s.rooms.AppendIn(ctx, q, room, nil, s.rooms.ServerName(), eventType, encoded)
	}
	return s.rooms.AppendIn(ctx, q, room, &actor.ID, s.rooms.UserAddress(actor.Username), eventType, encoded)
}

/*
channelFor reads the channel an event is to belong to, for somebody who must
be able to read it. Only a channel: a direct conversation or a disposable chat
has no calendar.
*/
func (s *Service) channelFor(ctx context.Context, channelID uuid.UUID, viewer Viewer) (dbgen.Room, error) {
	room, err := s.rooms.RequireMembership(ctx, channelID, viewer.UserID)
	if err != nil || room.Kind != "channel" {
		return dbgen.Room{}, invalid("events belong to a channel you are in")
	}
	if !viewer.Permissions.AllowsChannel(authz.PermChannelRead, channelID).Allowed {
		return dbgen.Room{}, invalid("events belong to a channel you are in")
	}
	return room, nil
}

// Created is what scheduling an event made.
type Created struct {
	Event Listed
	Log   dbgen.Event
}

/*
Create schedules an event.

Creating takes event.create where the event will be: in its channel, or across
the server for one with none. The creator hosts it and is a member of its
conversation from the start.
*/
func (s *Service) Create(ctx context.Context, actor dbgen.User, viewer Viewer, draft Draft) (Created, error) {
	return s.create(ctx, actor, viewer, draft, nil, "")
}

func (s *Service) create(
	ctx context.Context, actor dbgen.User, viewer Viewer, draft Draft,
	subscription *uuid.UUID, uid string,
) (Created, error) {
	channelVisibility, channelName := "", ""
	if draft.ChannelID != nil {
		channel, err := s.channelFor(ctx, *draft.ChannelID, viewer)
		if err != nil {
			return Created{}, err
		}
		if !viewer.Permissions.AllowsChannel(authz.PermEventCreate, channel.ID).Allowed {
			return Created{}, ErrNotAllowed
		}
		channelVisibility, channelName = channel.Visibility, channel.Name
	} else if !viewer.Permissions.AllowsServer(authz.PermEventCreate).Allowed {
		return Created{}, ErrNotAllowed
	}
	if err := draft.Normalise(channelVisibility); err != nil {
		return Created{}, err
	}
	return s.insert(ctx, actor, draft, subscription, uid, channelName, channelVisibility)
}

// insert writes a checked, normalised event, its conversation and its first
// log entry, together.
func (s *Service) insert(
	ctx context.Context, actor dbgen.User, draft Draft,
	subscription *uuid.UUID, uid, channelName, channelVisibility string,
) (Created, error) {
	lastEnd, err := Series{
		Start: draft.StartsAt, End: draft.EndsAt, Zone: mustZone(draft.TimeZone),
		Rule: draft.Recurrence, Exceptions: draft.Exceptions,
	}.LastEnd()
	if err != nil {
		return Created{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	id := uuid.New()
	if uid == "" {
		uid = id.String() + "@" + s.rooms.ServerName()
	}
	var created Created
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		room, err := q.CreateRoom(ctx, dbgen.CreateRoomParams{
			ID: id, Name: roomName(draft.Title), Kind: "event", Visibility: "private",
			HomeServer: s.rooms.ServerName(), CreatedBy: &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("create the event's conversation: %w", err)
		}
		if err := q.AddRoomMember(ctx, dbgen.AddRoomMemberParams{RoomID: id, UserID: actor.ID}); err != nil {
			return fmt.Errorf("add the creator: %w", err)
		}
		event, err := q.CreateCalendarEvent(ctx, dbgen.CreateCalendarEventParams{
			ID: id, ChannelID: draft.ChannelID, Kind: draft.Kind, Title: draft.Title,
			Description: draft.Description, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt,
			TimeZone: draft.TimeZone, AllDay: draft.AllDay, Recurrence: draft.Recurrence,
			Exceptions: draft.Exceptions, LastEndsAt: lastEnd, Location: draft.Location,
			Online: draft.Online, OnlineUrl: draft.OnlineURL, Capacity: draft.Capacity,
			Visibility: draft.Visibility, Uid: uid, SubscriptionID: subscription, CreatedBy: &actor.ID,
		})
		if err != nil {
			return fmt.Errorf("create event: %w", err)
		}
		hosts := []uuid.UUID{actor.ID}
		if draft.Hosts != nil {
			hosts = append(hosts, *draft.Hosts...)
		}
		draft.Hosts = &hosts
		if err := s.setPeople(ctx, q, event.ID, draft.Hosts, draft.Speakers); err != nil {
			return err
		}
		if draft.Sessions != nil {
			if err := s.setSessions(ctx, q, event.ID, *draft.Sessions); err != nil {
				return err
			}
		}
		created.Event = Listed{CalendarEvent: event, ChannelName: channelName, ChannelVisibility: channelVisibility}
		created.Log, err = s.append(ctx, q, room, &actor, events.TypeEventCreated, Summary(event))
		return err
	})
	if err != nil {
		return Created{}, err
	}
	return created, nil
}

func mustZone(name string) *time.Location {
	zone, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return zone
}

/*
setPeople replaces an event's hosts and speakers, where given, and keeps its
conversation's membership in step: somebody added joins it, somebody removed
leaves it unless they are going anyway.
*/
func (s *Service) setPeople(ctx context.Context, q *dbgen.Queries, eventID uuid.UUID, hosts, speakers *[]uuid.UUID) error {
	if hosts == nil && speakers == nil {
		return nil
	}
	existing, err := q.ListCalendarEventPeople(ctx, eventID)
	if err != nil {
		return fmt.Errorf("read people: %w", err)
	}
	keep := map[uuid.UUID]string{}
	for _, person := range existing {
		if (person.Role == RoleHost && hosts == nil) || (person.Role == RoleSpeaker && speakers == nil) {
			keep[person.UserID] = person.Role
		}
	}
	if speakers != nil {
		for _, id := range *speakers {
			keep[id] = RoleSpeaker
		}
	}
	// A host who is also named a speaker is a host: running it is the larger part.
	if hosts != nil {
		for _, id := range *hosts {
			keep[id] = RoleHost
		}
	}
	if err := q.ClearCalendarEventPeople(ctx, eventID); err != nil {
		return fmt.Errorf("clear people: %w", err)
	}
	for id, role := range keep {
		if err := q.AddCalendarEventPerson(ctx, dbgen.AddCalendarEventPersonParams{
			EventID: eventID, UserID: id, Role: role,
		}); err != nil {
			return invalid("everybody named must have an account here")
		}
	}
	touched := map[uuid.UUID]bool{}
	for _, person := range existing {
		touched[person.UserID] = true
	}
	for id := range keep {
		touched[id] = true
	}
	for id := range touched {
		if err := s.syncMember(ctx, q, eventID, id); err != nil {
			return err
		}
	}
	return nil
}

/*
syncMember makes somebody's membership of an event's conversation match what
they are to the event: a member while they host, speak, or are going or maybe
going; not otherwise. The conversation is private and nobody joins it any
other way.
*/
func (s *Service) syncMember(ctx context.Context, q *dbgen.Queries, eventID, userID uuid.UUID) error {
	belongs := false
	people, err := q.ListCalendarEventPeople(ctx, eventID)
	if err != nil {
		return fmt.Errorf("read people: %w", err)
	}
	for _, person := range people {
		belongs = belongs || person.UserID == userID
	}
	if !belongs {
		answer, err := q.GetCalendarRSVP(ctx, dbgen.GetCalendarRSVPParams{EventID: eventID, UserID: userID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read answer: %w", err)
		}
		belongs = err == nil && (answer.Status == Going || answer.Status == Maybe)
	}
	if !belongs {
		event, err := q.GetCalendarEvent(ctx, eventID)
		if err == nil && event.CreatedBy != nil && *event.CreatedBy == userID {
			belongs = true
		}
	}
	if belongs {
		return q.AddRoomMember(ctx, dbgen.AddRoomMemberParams{RoomID: eventID, UserID: userID})
	}
	return q.RemoveRoomMember(ctx, dbgen.RemoveRoomMemberParams{RoomID: eventID, UserID: userID})
}

func (s *Service) setSessions(ctx context.Context, q *dbgen.Queries, eventID uuid.UUID, sessions []Session) error {
	if err := q.ClearCalendarSessions(ctx, eventID); err != nil {
		return fmt.Errorf("clear sessions: %w", err)
	}
	for _, session := range sessions {
		speakers := session.Speakers
		if speakers == nil {
			speakers = []uuid.UUID{}
		}
		if err := q.AddCalendarSession(ctx, dbgen.AddCalendarSessionParams{
			ID: uuid.New(), EventID: eventID, Title: session.Title, Description: session.Description,
			StartsAt: session.StartsAt, EndsAt: session.EndsAt, Speakers: speakers,
			GuestSpeakers: session.GuestSpeakers,
		}); err != nil {
			return fmt.Errorf("add session: %w", err)
		}
	}
	return nil
}

// listedOf converts any of the generated "event with its channel" rows.
func listedOf(event dbgen.CalendarEvent, name, visibility string) Listed {
	return Listed{CalendarEvent: event, ChannelName: name, ChannelVisibility: visibility}
}

/*
Get reads an event for somebody who may see it. One they may not see is
reported as missing, exactly as one that does not exist.
*/
func (s *Service) Get(ctx context.Context, viewer Viewer, id uuid.UUID) (Listed, error) {
	row, err := s.queries.GetCalendarEventWithChannel(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Listed{}, ErrNotFound
	}
	if err != nil {
		return Listed{}, fmt.Errorf("read event: %w", err)
	}
	event := listedOf(row.CalendarEvent, row.ChannelName, row.ChannelVisibility)
	if !viewer.Sees(event) {
		return Listed{}, ErrNotFound
	}
	return event, nil
}

// Happening is one occurrence of an event, as a list shows it.
type Happening struct {
	Event Listed
	Occurrence
}

// MaxRange is the longest span one listing covers.
const MaxRange = 400 * 24 * time.Hour

/*
Range lists the occurrences somebody may see that overlap a span, in order,
optionally in one channel.
*/
func (s *Service) Range(ctx context.Context, viewer Viewer, since, until time.Time, channel *uuid.UUID, limit int) ([]Happening, error) {
	if !until.After(since) || until.Sub(since) > MaxRange {
		return nil, invalid("a range is up to 400 days, ending after it starts")
	}
	rows, err := s.queries.ListCalendarEventsInRange(ctx, dbgen.ListCalendarEventsInRangeParams{
		Since: since, Until: until, ChannelID: channel,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	var happenings []Happening
	for _, row := range rows {
		event := listedOf(row.CalendarEvent, row.ChannelName, row.ChannelVisibility)
		if !viewer.Sees(event) {
			continue
		}
		occurrences, err := event.Series().Between(since, until)
		if err != nil {
			s.log.Warn("an event's repeat cannot be expanded", "event", event.ID, "error", err)
			continue
		}
		for _, occurrence := range occurrences {
			happenings = append(happenings, Happening{Event: event, Occurrence: occurrence})
		}
	}
	slices.SortStableFunc(happenings, func(a, b Happening) int { return a.Start.Compare(b.Start) })
	if limit > 0 && len(happenings) > limit {
		happenings = happenings[:limit]
	}
	return happenings, nil
}

// Changed is what an edit did.
type Changed struct {
	Event Listed
	Log   dbgen.Event
	// Material is a change the people going need to hear about: when, or
	// where.
	Material bool
	// Promoted are people moved up from the waiting list because places
	// were added.
	Promoted []uuid.UUID
}

/*
Update changes an event, for somebody who made it, hosts it, or manages events
where it is. It stays in its channel: moving an event between channels would
change who may see it underneath the people already invited.
*/
func (s *Service) Update(ctx context.Context, actor dbgen.User, viewer Viewer, id uuid.UUID, draft Draft) (Changed, error) {
	current, err := s.Get(ctx, viewer, id)
	if err != nil {
		return Changed{}, err
	}
	if !viewer.MayEdit(current.CalendarEvent) {
		return Changed{}, ErrNotAllowed
	}
	if current.SubscriptionID != nil {
		return Changed{}, invalid("this event comes from a subscribed calendar and changes there")
	}
	draft.ChannelID = current.ChannelID
	if err := draft.Normalise(current.ChannelVisibility); err != nil {
		return Changed{}, err
	}
	lastEnd, err := Series{
		Start: draft.StartsAt, End: draft.EndsAt, Zone: mustZone(draft.TimeZone),
		Rule: draft.Recurrence, Exceptions: draft.Exceptions,
	}.LastEnd()
	if err != nil {
		return Changed{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	changed := Changed{}
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		before, err := q.LockCalendarEvent(ctx, id)
		if err != nil {
			return fmt.Errorf("lock event: %w", err)
		}
		updated, err := q.UpdateCalendarEvent(ctx, dbgen.UpdateCalendarEventParams{
			ID: id, ChannelID: before.ChannelID, Kind: draft.Kind, Title: draft.Title,
			Description: draft.Description, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt,
			TimeZone: draft.TimeZone, AllDay: draft.AllDay, Recurrence: draft.Recurrence,
			Exceptions: draft.Exceptions, LastEndsAt: lastEnd, Location: draft.Location,
			Online: draft.Online, OnlineUrl: draft.OnlineURL, Capacity: draft.Capacity,
			Visibility: draft.Visibility,
		})
		if err != nil {
			return fmt.Errorf("update event: %w", err)
		}
		if err := s.setPeople(ctx, q, id, draft.Hosts, draft.Speakers); err != nil {
			return err
		}
		if draft.Sessions != nil {
			if err := s.setSessions(ctx, q, id, *draft.Sessions); err != nil {
				return err
			}
		}
		room, err := q.GetRoomByID(ctx, id)
		if err != nil {
			return fmt.Errorf("read the event's conversation: %w", err)
		}
		if updated.Title != before.Title {
			if room, err = q.RenameRoom(ctx, dbgen.RenameRoomParams{ID: id, Name: roomName(updated.Title), Topic: room.Topic}); err != nil {
				return fmt.Errorf("rename the event's conversation: %w", err)
			}
		}
		fields := differences(before, updated)
		changed.Material = slices.ContainsFunc(fields, func(field string) bool {
			return slices.Contains([]string{"startsAt", "endsAt", "timeZone", "recurrence", "exceptions", "location", "online", "onlineUrl"}, field)
		})
		if capacityGrew(before.Capacity, updated.Capacity) {
			if changed.Promoted, err = s.promote(ctx, q, updated); err != nil {
				return err
			}
		}
		content := Summary(updated)
		content["changed"] = fields
		changed.Log, err = s.append(ctx, q, room, &actor, events.TypeEventUpdated, content)
		changed.Event = listedOf(updated, current.ChannelName, current.ChannelVisibility)
		return err
	})
	if err != nil {
		return Changed{}, err
	}
	return changed, nil
}

func capacityGrew(before, after *int32) bool {
	return before != nil && (after == nil || *after > *before)
}

// differences names the fields an edit changed, in the words the API uses.
func differences(before, after dbgen.CalendarEvent) []string {
	fields := []string{}
	add := func(changed bool, name string) {
		if changed {
			fields = append(fields, name)
		}
	}
	add(before.Kind != after.Kind, "kind")
	add(before.Title != after.Title, "title")
	add(before.Description != after.Description, "description")
	add(!before.StartsAt.Equal(after.StartsAt), "startsAt")
	add(!before.EndsAt.Equal(after.EndsAt), "endsAt")
	add(before.TimeZone != after.TimeZone, "timeZone")
	add(before.AllDay != after.AllDay, "allDay")
	add(before.Recurrence != after.Recurrence, "recurrence")
	add(!slices.EqualFunc(before.Exceptions, after.Exceptions, time.Time.Equal), "exceptions")
	add(before.Location != after.Location, "location")
	add(before.Online != after.Online, "online")
	add(before.OnlineUrl != after.OnlineUrl, "onlineUrl")
	add(!equalCapacity(before.Capacity, after.Capacity), "capacity")
	add(before.Visibility != after.Visibility, "visibility")
	return fields
}

func equalCapacity(a, b *int32) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Cancelled is what cancelling did.
type Cancelled struct {
	Event Listed
	Log   dbgen.Event
}

// Cancel calls an event off, keeping it, its answers and its conversation.
func (s *Service) Cancel(ctx context.Context, actor dbgen.User, viewer Viewer, id uuid.UUID, reason string) (Cancelled, error) {
	current, err := s.Get(ctx, viewer, id)
	if err != nil {
		return Cancelled{}, err
	}
	if !viewer.MayEdit(current.CalendarEvent) {
		return Cancelled{}, ErrNotAllowed
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > MaxLocation {
		return Cancelled{}, invalid("a reason is at most %d characters", MaxLocation)
	}
	var result Cancelled
	err = s.inTransaction(ctx, func(q *dbgen.Queries) error {
		event, err := q.CancelCalendarEvent(ctx, dbgen.CancelCalendarEventParams{ID: id, CancelReason: reason})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCancelled
		}
		if err != nil {
			return fmt.Errorf("cancel event: %w", err)
		}
		room, err := q.GetRoomByID(ctx, id)
		if err != nil {
			return fmt.Errorf("read the event's conversation: %w", err)
		}
		content := Summary(event)
		content["reason"] = reason
		result.Log, err = s.append(ctx, q, room, &actor, events.TypeEventCancelled, content)
		result.Event = listedOf(event, current.ChannelName, current.ChannelVisibility)
		return err
	})
	return result, err
}

/*
Delete removes an event and its conversation altogether, for whoever made it or
manages events where it is. Returns the event as it was, for the record.
*/
func (s *Service) Delete(ctx context.Context, viewer Viewer, id uuid.UUID) (Listed, error) {
	current, err := s.Get(ctx, viewer, id)
	if err != nil {
		return Listed{}, err
	}
	if !viewer.MayDelete(current.CalendarEvent) {
		return Listed{}, ErrNotAllowed
	}
	if err := s.queries.DeleteCalendarEvent(ctx, id); err != nil {
		return Listed{}, fmt.Errorf("delete event: %w", err)
	}
	return current, nil
}

// People are an event's hosts and speakers.
func (s *Service) People(ctx context.Context, id uuid.UUID) ([]dbgen.ListCalendarEventPeopleRow, error) {
	return s.queries.ListCalendarEventPeople(ctx, id)
}

// Sessions are an event's agenda, in order.
func (s *Service) Sessions(ctx context.Context, id uuid.UUID) ([]dbgen.CalendarSession, error) {
	return s.queries.ListCalendarSessions(ctx, id)
}

// SetCover makes an attachment in the event's conversation its cover, or
// takes the cover away.
func (s *Service) SetCover(ctx context.Context, viewer Viewer, id uuid.UUID, attachment *uuid.UUID, attachmentRoom uuid.UUID) error {
	current, err := s.Get(ctx, viewer, id)
	if err != nil {
		return err
	}
	if !viewer.MayEdit(current.CalendarEvent) {
		return ErrNotAllowed
	}
	if attachment != nil && attachmentRoom != id {
		return invalid("a cover is uploaded to the event itself")
	}
	return s.queries.SetCalendarEventCover(ctx, dbgen.SetCalendarEventCoverParams{ID: id, CoverAttachmentID: attachment})
}
