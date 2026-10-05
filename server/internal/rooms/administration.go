package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Administering a channel: renaming it, archiving and restoring it, changing who
may find it, and taking somebody out of it.

Each is recorded as an event in the channel's own log as well as changing the
row, so the change appears where its members can see who made it, and each
returns that event so the caller can deliver it live. An empty event means
nothing changed and nothing was recorded.

Only a channel this server holds. A direct conversation is between the people in
it and nobody else's to rename or archive — which covers an enquiry's and an
engagement's conversation too, since those are direct rooms. A project's stream
and an issue's discussion belong to the project, which decides who sees them and
what they are called. A replica of a channel another server owns is that
server's to change: an event written here would take a position in an order
this server does not own, after which replication into the room stops for good.

The actor must be a member, as for posting: these events are written by
somebody the room can attribute them to.
*/

var (
	// ErrNotAChannel refuses administering something that is not a channel.
	ErrNotAChannel = errors.New("only a channel can be changed this way")
	// ErrHostedElsewhere refuses administering a replica of another server's
	// channel.
	ErrHostedElsewhere = errors.New("this channel belongs to another server, which decides this")
	// ErrInvalidVisibility refuses a visibility other than public or private.
	ErrInvalidVisibility = errors.New("a channel is either public or private")
	/*
		ErrFederatedVisibility refuses changing the visibility of a channel
		other servers carry.

		Each replica took its visibility when it was adopted and has no way to
		hear of a change, so a channel made private here would stay public on
		every peer — a change that appeared to take effect and did not.
	*/
	ErrFederatedVisibility = errors.New("a channel shared with other servers keeps its visibility; stop sharing it first")
	// ErrInvalidDetails refuses a name or topic that cannot be used.
	ErrInvalidDetails = errors.New("invalid channel details")
	// ErrNoSuchMember refuses removing somebody who is not in the channel.
	ErrNoSuchMember = errors.New("that person is not in this channel")
)

// Limits on what a channel is called and what it says it is for, counted in
// characters rather than bytes so every language is held to the same length.
const (
	MaxRoomNameLength  = 80
	MaxRoomTopicLength = 500
)

// administrable reports why a room may not be administered as a channel, or
// nil when it may.
func (s *Service) administrable(room dbgen.Room) error {
	if room.Kind != "channel" && room.Kind != "disposable" {
		return ErrNotAChannel
	}
	if !s.HostedHere(room) {
		return ErrHostedElsewhere
	}
	return nil
}

// Administrable reports whether a room is one these operations apply to, so a
// client can be told before it offers them.
func (s *Service) Administrable(room dbgen.Room) bool {
	return s.administrable(room) == nil
}

/*
Rename changes a channel's name and topic.

An empty name keeps the one it has, since a channel with no name cannot be
found or spoken of; an empty topic clears it.
*/
func (s *Service) Rename(ctx context.Context, roomID, actorID uuid.UUID, name, topic string) (dbgen.Room, dbgen.Event, error) {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, err
	}
	if err := s.administrable(room); err != nil {
		return dbgen.Room{}, dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Room{}, dbgen.Event{}, ErrArchived
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = room.Name
	}
	trimmedTopic := strings.TrimSpace(topic)
	if utf8.RuneCountInString(trimmedName) > MaxRoomNameLength {
		return dbgen.Room{}, dbgen.Event{}, fmt.Errorf("%w: a name is at most %d characters",
			ErrInvalidDetails, MaxRoomNameLength)
	}
	if utf8.RuneCountInString(trimmedTopic) > MaxRoomTopicLength {
		return dbgen.Room{}, dbgen.Event{}, fmt.Errorf("%w: a topic is at most %d characters",
			ErrInvalidDetails, MaxRoomTopicLength)
	}

	// Nothing changed, so nothing to record: an event saying a channel was
	// renamed to what it was already called is noise in its timeline.
	if trimmedName == room.Name && trimmedTopic == room.Topic {
		return room, dbgen.Event{}, nil
	}

	updated, err := s.store.RenameRoom(ctx, dbgen.RenameRoomParams{
		ID:    roomID,
		Name:  trimmedName,
		Topic: trimmedTopic,
	})
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, fmt.Errorf("rename room: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"name":  updated.Name,
		"topic": updated.Topic,
		// The previous values, so the timeline reads as a change rather than
		// a bare assertion of the new one.
		"previousName":  room.Name,
		"previousTopic": room.Topic,
	})
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, fmt.Errorf("encode rename: %w", err)
	}

	event, err := s.appendBy(ctx, updated, actorID, events.TypeRoomRenamed, content)
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, err
	}
	return updated, event, nil
}

/*
SetVisibility makes a channel public or private.

Making a channel public discloses its whole history to everybody on the server,
not only what is said from now on; making it private takes it out of everybody's
sight but its members'. Deciding whether that is wanted is the caller's — this
refuses only what cannot be made to work.
*/
func (s *Service) SetVisibility(ctx context.Context, roomID, actorID uuid.UUID, visibility string) (dbgen.Room, dbgen.Event, error) {
	if visibility != "public" && visibility != "private" {
		return dbgen.Room{}, dbgen.Event{}, ErrInvalidVisibility
	}

	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, err
	}
	if err := s.administrable(room); err != nil {
		return dbgen.Room{}, dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Room{}, dbgen.Event{}, ErrArchived
	}
	if room.Visibility == visibility {
		return room, dbgen.Event{}, nil
	}
	if room.Federated {
		return dbgen.Room{}, dbgen.Event{}, ErrFederatedVisibility
	}

	updated, err := s.store.SetRoomVisibility(ctx, dbgen.SetRoomVisibilityParams{
		ID:         roomID,
		Visibility: visibility,
	})
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, fmt.Errorf("change visibility: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"visibility":         updated.Visibility,
		"previousVisibility": room.Visibility,
	})
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, fmt.Errorf("encode visibility change: %w", err)
	}

	event, err := s.appendBy(ctx, updated, actorID, events.TypeRoomVisibilityChanged, content)
	if err != nil {
		return dbgen.Room{}, dbgen.Event{}, err
	}
	return updated, event, nil
}

// Archive stops new messages while preserving history.
func (s *Service) Archive(ctx context.Context, roomID, actorID uuid.UUID) (dbgen.Event, error) {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if err := s.administrable(room); err != nil {
		return dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		// Already archived; nothing to do and nothing to record.
		return dbgen.Event{}, nil
	}

	if err := s.store.ArchiveRoom(ctx, dbgen.ArchiveRoomParams{
		ID:         roomID,
		ArchivedBy: &actorID,
	}); err != nil {
		return dbgen.Event{}, fmt.Errorf("archive room: %w", err)
	}

	// Recorded after the room is archived, so the archive event is the last
	// thing in the timeline.
	return s.appendBy(ctx, room, actorID, events.TypeRoomArchived, json.RawMessage(`{}`))
}

// Restore reverses archiving, so the decision is not permanent.
func (s *Service) Restore(ctx context.Context, roomID, actorID uuid.UUID) (dbgen.Event, error) {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if err := s.administrable(room); err != nil {
		return dbgen.Event{}, err
	}
	if room.ArchivedAt == nil {
		return dbgen.Event{}, nil
	}

	if err := s.store.RestoreRoom(ctx, roomID); err != nil {
		return dbgen.Event{}, fmt.Errorf("restore room: %w", err)
	}

	return s.appendBy(ctx, room, actorID, events.TypeRoomRestored, json.RawMessage(`{}`))
}

/*
Remove takes somebody out of a channel. The caller must hold the permission to
do so, which the HTTP layer checks.

Distinct from a ban: nothing stops them coming back to a public channel, and a
private one needs an invitation as it did the first time. The removal is
recorded while they are still a member, so it is in the log they were reading.
*/
func (s *Service) Remove(ctx context.Context, roomID, actorID, memberID uuid.UUID) (dbgen.Event, error) {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if err := s.administrable(room); err != nil {
		return dbgen.Event{}, err
	}

	present, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: roomID, UserID: memberID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("check membership: %w", err)
	}
	if !present {
		return dbgen.Event{}, ErrNoSuchMember
	}

	member, err := s.store.GetUserByID(ctx, memberID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read member: %w", err)
	}
	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read actor: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"member":    s.UserAddress(member.Username),
		"removedBy": s.UserAddress(actor.Username),
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode removal: %w", err)
	}

	event, err := s.append(ctx, room, actor, events.TypeMemberRemoved, content, "")
	if err != nil {
		return dbgen.Event{}, err
	}

	if err := s.store.RemoveRoomMember(ctx, dbgen.RemoveRoomMemberParams{
		RoomID: roomID, UserID: memberID,
	}); err != nil {
		return dbgen.Event{}, fmt.Errorf("remove member: %w", err)
	}

	return event, nil
}

// appendBy records an event written by an account known only by identifier.
func (s *Service) appendBy(
	ctx context.Context,
	room dbgen.Room,
	actorID uuid.UUID,
	eventType events.Type,
	content json.RawMessage,
) (dbgen.Event, error) {
	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read actor: %w", err)
	}
	return s.append(ctx, room, actor, eventType, content, "")
}
