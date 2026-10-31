package rooms

import (
	"context"
	"fmt"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// PinnedMessage is a message a room has marked as important.
type PinnedMessage struct {
	Event    dbgen.Event
	PinnedBy string
	PinnedAt int64
}

/*
PinMessage marks a message as important for everybody in the room.

A permission rather than something any member may do. A pin is the channel
speaking rather than a person, so in a room with moderation it belongs to
whoever moderates — the caller supplies that decision, since permissions live
above this package.
*/
func (s *Service) PinMessage(
	ctx context.Context,
	roomID, userID uuid.UUID,
	eventID string,
) error {
	if _, err := s.RequireMembership(ctx, roomID, userID); err != nil {
		return err
	}
	if _, err := s.EventInRoom(ctx, roomID, eventID); err != nil {
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("read pinner: %w", err)
	}

	return s.store.PinMessage(ctx, dbgen.PinMessageParams{
		RoomID:          roomID,
		EventID:         eventID,
		PinnedBy:        &userID,
		PinnedByAddress: s.UserAddress(user.Username),
	})
}

// UnpinMessage removes a pin.
func (s *Service) UnpinMessage(
	ctx context.Context,
	roomID, userID uuid.UUID,
	eventID string,
) error {
	if _, err := s.RequireMembership(ctx, roomID, userID); err != nil {
		return err
	}

	return s.store.UnpinMessage(ctx, dbgen.UnpinMessageParams{
		RoomID:  roomID,
		EventID: eventID,
	})
}

// PinnedMessages lists what a room has pinned, most recent first.
//
// Visibility rather than membership: a public room can be read without
// joining, and its pinned messages are part of reading it.
func (s *Service) PinnedMessages(
	ctx context.Context,
	roomID, userID uuid.UUID,
) ([]PinnedMessage, error) {
	if _, err := s.Visible(ctx, roomID, userID); err != nil {
		return nil, err
	}

	rows, err := s.store.ListPinnedMessages(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("read pinned messages: %w", err)
	}

	pinned := make([]PinnedMessage, 0, len(rows))
	for _, row := range rows {
		pinned = append(pinned, PinnedMessage{
			Event:    row.Event,
			PinnedBy: row.PinnedByAddress,
			PinnedAt: row.PinnedAt.UnixMilli(),
		})
	}

	return pinned, nil
}

// PinnedEventIDs is the set a timeline uses to mark which messages are pinned.
func (s *Service) PinnedEventIDs(ctx context.Context, roomID uuid.UUID) (map[string]bool, error) {
	ids, err := s.store.ListPinnedEventIDs(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("read pinned identifiers: %w", err)
	}

	pinned := make(map[string]bool, len(ids))
	for _, id := range ids {
		pinned[id] = true
	}
	return pinned, nil
}

/*
PinRoom keeps a conversation at the top of somebody's own list.

A different thing from pinning a message, despite the name they share in every
product that has both. This one is per person and says nothing to anybody else:
it is a bookmark, not an announcement.
*/
func (s *Service) PinRoom(ctx context.Context, userID, roomID uuid.UUID, position int32) error {
	// Visible rather than a member: somebody may want a public channel they
	// read but have not joined kept at the top.
	if _, err := s.Visible(ctx, roomID, userID); err != nil {
		return err
	}

	return s.store.PinRoom(ctx, dbgen.PinRoomParams{
		UserID:   userID,
		RoomID:   roomID,
		Position: position,
	})
}

// UnpinRoom removes a conversation from somebody's pinned list.
func (s *Service) UnpinRoom(ctx context.Context, userID, roomID uuid.UUID) error {
	return s.store.UnpinRoom(ctx, dbgen.UnpinRoomParams{UserID: userID, RoomID: roomID})
}

// PinnedRooms is the conversations somebody has pinned, in their chosen order.
func (s *Service) PinnedRooms(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.store.ListPinnedRooms(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read pinned rooms: %w", err)
	}

	rooms := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		rooms = append(rooms, row.RoomID)
	}
	return rooms, nil
}
