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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
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
}

// NewService builds a rooms service.
func NewService(store Store, keyring *events.Keyring, serverName string, log *slog.Logger) *Service {
	return &Service{store: store, keyring: keyring, serverName: serverName, log: log}
}

// Address returns the federated address of a room on this server.
func (s *Service) Address(slug string) string {
	return slug + "@" + s.serverName
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

	room, err := s.store.CreateRoom(ctx, dbgen.CreateRoomParams{
		ID:                uuid.New(),
		Slug:              slug,
		SlugFolded:        strings.ToLower(slug),
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
