// Package rooms owns channels, membership and the messages within them.
package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// Errors a caller is expected to handle.
var (
	// ErrRoomNotFound is returned both for a room that does not exist and for
	// a private room the requester may not see. The two must be
	// indistinguishable, or the existence of a private room could be probed.
	ErrRoomNotFound = errors.New("room not found")

	ErrSlugTaken    = errors.New("that room address is already taken")
	ErrInvalidSlug  = errors.New("invalid room address")
	ErrArchived     = errors.New("this room is archived")
	ErrNotAMember   = errors.New("you are not a member of this room")
	ErrNotTheAuthor = errors.New("only the author may change this message")
	/*
		ErrRedactedLocally reports a redaction that happened and produced no
		event, which is what a redaction on a replica is.

		A sentinel rather than a silent success, because a caller that
		mistakes it for an ordinary deletion looks for fields in an event that
		was never written — which is how the audit entry for a unilateral
		redaction came to be silently skipped. Not an error in the sense of
		something having gone wrong: the content is gone, which is the whole
		of what was asked.
	*/
	ErrRedactedLocally = errors.New("redacted locally, without a deletion event")
)

// Store is the database surface this package needs.
type Store interface {
	dbgen.Querier
}

// Service manages rooms and their events.
type Service struct {
	store   Store
	keyring *events.Keyring
	// serverName is this server's own name, used to build addresses and to
	// mark events as originating here.
	serverName string
	log        *slog.Logger

	// deciders answer who may see rooms that belong to something else, by
	// kind. See DelegateAccess.
	deciders map[string]AccessDecider
	// remoteDeciders answer who elsewhere takes part in such rooms.
	remoteDeciders map[string]RemoteMemberDecider

	// appended is told about every event this server stores, once it is
	// stored. See OnAppended.
	appended func(context.Context, dbgen.Event)
}

/*
OnAppended sets what is told about each event once it is stored.

One place every stored event passes, so that whatever must follow an event —
an outgoing webhook, today — follows every event, rather than only the ones
whose route remembered to ask. A join, a leave and a channel's creation are
stored by paths that publish nothing live, and were for that reason never
delivered to anybody subscribed to them.

Called once while the server is being built, before it serves anything.
*/
func (s *Service) OnAppended(tell func(context.Context, dbgen.Event)) { s.appended = tell }

// Told hands an event appended with AppendIn to whatever asked to hear of
// it, once the transaction that wrote it has committed.
func (s *Service) Told(ctx context.Context, event dbgen.Event) { s.told(ctx, event) }

// told hands a stored event to whatever asked to hear of it.
func (s *Service) told(ctx context.Context, event dbgen.Event) {
	if s.appended != nil {
		s.appended(ctx, event)
	}
}

// NewService builds a rooms service.
func NewService(store Store, keyring *events.Keyring, serverName string, log *slog.Logger) *Service {
	return &Service{store: store, keyring: keyring, serverName: serverName, log: log}
}

/*
HostedHere reports whether this server owns a room.

Asked of the name rooms are stamped with when they are made, and of nothing
else. Comparing against the configured address instead treated every room as
somebody else's on a server with no address configured — the default — because
rooms are stamped "localhost" there and the address is empty, so the client
sent every message through federation and every message was refused.
*/
func (s *Service) HostedHere(room dbgen.Room) bool {
	return strings.EqualFold(room.HomeServer, s.serverName)
}

// Address returns the federated address of a room on this server.
//
// A direct conversation has no slug: it is found by its participants rather
// than by name, because a name would make it discoverable, which a private
// conversation must not be. Its address names the room's identifier instead.
func (s *Service) Address(slug *string) string {
	if slug == nil || *slug == "" {
		return ""
	}
	return *slug + "@" + s.serverName
}

/*
RoomAddress returns an address for any room, including one with no slug.

Named after the room's home server, not after whichever server is asking. A
room replicated onto three servers has one address on all three, because the
address is part of what its events are signed over — if each server computed
its own, every server would reject every other's events as belonging to a room
it had never heard of.

home_server is this server for a room created here, so the ordinary case is
unchanged.
*/
func (s *Service) RoomAddress(room dbgen.Room) string {
	home := room.HomeServer
	if home == "" {
		home = s.serverName
	}

	if room.Slug != nil && *room.Slug != "" {
		return *room.Slug + "@" + home
	}
	return room.ID.String() + "@" + home
}

// UserAddress returns the federated address of a local user.
func (s *Service) UserAddress(username string) string {
	return username + "@" + s.serverName
}

// NewRoom describes a room to create.
type NewRoom struct {
	Slug       string
	Name       string
	Topic      string
	Kind       string
	Visibility string
	CreatedBy  uuid.UUID
	// MessageTTLSeconds, when set, makes this a disposable room whose messages
	// the server removes after that long.
	MessageTTLSeconds *int32
}

// Create makes a room and records its creation as the first event.
//
// The creator joins immediately: a room nobody is in cannot be administered,
// and the person who made it is the obvious first member.
func (s *Service) Create(ctx context.Context, input NewRoom) (dbgen.Room, error) {
	slug := strings.TrimSpace(input.Slug)
	if err := ValidateSlug(slug); err != nil {
		return dbgen.Room{}, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = slug
	}

	kind := input.Kind
	if kind == "" {
		kind = "channel"
	}
	visibility := input.Visibility
	if visibility == "" {
		visibility = "public"
	}

	folded := strings.ToLower(slug)
	room, err := s.store.CreateRoom(ctx, dbgen.CreateRoomParams{
		ID:                uuid.New(),
		Slug:              &slug,
		SlugFolded:        &folded,
		Name:              name,
		Topic:             strings.TrimSpace(input.Topic),
		Kind:              kind,
		Visibility:        visibility,
		HomeServer:        s.serverName,
		CreatedBy:         &input.CreatedBy,
		MessageTtlSeconds: input.MessageTTLSeconds,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return dbgen.Room{}, ErrSlugTaken
		}
		return dbgen.Room{}, fmt.Errorf("create room: %w", err)
	}

	if err := s.store.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
		RoomID: room.ID,
		UserID: input.CreatedBy,
	}); err != nil {
		return dbgen.Room{}, fmt.Errorf("add creator to room: %w", err)
	}

	creator, err := s.store.GetUserByID(ctx, input.CreatedBy)
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("read creator: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"name":       room.Name,
		"topic":      room.Topic,
		"visibility": room.Visibility,
		"kind":       room.Kind,
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("encode room creation: %w", err)
	}

	if _, err := s.append(ctx, room, creator, events.TypeRoomCreated, content, ""); err != nil {
		return dbgen.Room{}, err
	}

	return room, nil
}

// ValidateSlug reports whether a room address may be used.
//
// The permitted set matches usernames: these identifiers are embedded in
// addresses of the form room@server, so they must survive being written in a
// URL and read aloud.
func ValidateSlug(slug string) error {
	if length := len([]rune(slug)); length < 2 || length > 48 {
		return fmt.Errorf("%w: must be between 2 and 48 characters", ErrInvalidSlug)
	}

	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			unicode.IsDigit(r),
			r == '_', r == '.', r == '-':
		default:
			return fmt.Errorf("%w: may contain only letters, digits, underscore, dot and hyphen", ErrInvalidSlug)
		}
	}

	if strings.ContainsAny(slug[:1], "_.-") || strings.ContainsAny(slug[len(slug)-1:], "_.-") {
		return fmt.Errorf("%w: must start and end with a letter or digit", ErrInvalidSlug)
	}

	return nil
}

// Visible returns a room the user is permitted to see.
//
// A private room the user is not in is reported as not found, so its existence
// cannot be probed by identifier.
func (s *Service) Visible(ctx context.Context, roomID, userID uuid.UUID) (dbgen.Room, error) {
	room, err := s.store.GetRoomByID(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Room{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("read room: %w", err)
	}

	// A room belonging to something else is visible to whoever may see that
	// thing, and decided there — before anything about the room's own
	// visibility or membership, which for such a room are not the answer.
	if handled, visible, err := s.delegated(ctx, room, userID); handled {
		if err != nil {
			return dbgen.Room{}, fmt.Errorf("decide access: %w", err)
		}
		if !visible {
			return dbgen.Room{}, ErrRoomNotFound
		}
		return room, nil
	}

	if room.Visibility == "public" {
		return room, nil
	}

	member, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: roomID,
		UserID: userID,
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("check membership: %w", err)
	}
	if !member {
		return dbgen.Room{}, ErrRoomNotFound
	}

	return room, nil
}

// RequireMembership returns the room only if the user is in it.
func (s *Service) RequireMembership(ctx context.Context, roomID, userID uuid.UUID) (dbgen.Room, error) {
	room, err := s.Visible(ctx, roomID, userID)
	if err != nil {
		return dbgen.Room{}, err
	}

	member, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: roomID,
		UserID: userID,
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("check membership: %w", err)
	}
	if !member {
		return dbgen.Room{}, ErrNotAMember
	}

	return room, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

/*
Keyring is what this server signs with, and publishes.

Exposed so that verification can be done outside this package — by a federation
peer holding only the published half, and by the tests that prove an event can
be checked without the database it came from.
*/
func (s *Service) Keyring() *events.Keyring { return s.keyring }

/*
SenderName is what to call whoever sent an event, for a reader: a local
sender's display name, or their username without one.

Empty for an event from another server, whose sender this server knows only by
address; a client calls them by that address instead. Read by identifier rather
than address, so a sender who has since changed their username is called what
they are called now.
*/
func (s *Service) SenderName(ctx context.Context, event dbgen.Event) string {
	if event.SenderID == nil {
		return ""
	}
	user, err := s.store.GetUserByID(ctx, *event.SenderID)
	if err != nil {
		return ""
	}
	return identity.NameOf(user.Username, user.DisplayName)
}

/*
NameAt is what to call the person at an address, for a reader: a local
person's display name, or their username without one.

Empty for an address elsewhere, or one nobody here holds. An address under a
name somebody has since given up still finds them, as the name lookup does.
*/
func (s *Service) NameAt(ctx context.Context, address string) string {
	local, host, found := strings.Cut(address, "@")
	if !found || !strings.EqualFold(host, s.serverName) {
		return ""
	}
	user, err := s.store.GetUserByUsername(ctx, identity.NormaliseUsername(local))
	if err != nil {
		return ""
	}
	return identity.NameOf(user.Username, user.DisplayName)
}
