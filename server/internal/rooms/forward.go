package rooms

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Forward sends a copy of a message to another conversation.

A copy rather than a reference, deliberately. A reference would mean the
forwarded message changes when the original is edited, vanishes when it is
deleted, and has to be readable by everybody who can see where it was forwarded
to — which would leak a private room's contents into a public one the moment
somebody forwarded from it.

The attribution travels with the copy, so a forwarded message says where it
came from rather than appearing to be the forwarder's own words.
*/
func (s *Service) Forward(
	ctx context.Context,
	fromRoomID, toRoomID, senderID uuid.UUID,
	eventID string,
	comment string,
	clientToken string,
) (dbgen.Event, error) {
	// Readable at the source and a member at the destination. Both are needed:
	// forwarding out of a room you cannot read would be a way to extract it,
	// and forwarding into one you have not joined would be posting in it.
	if _, err := s.Visible(ctx, fromRoomID, senderID); err != nil {
		return dbgen.Event{}, err
	}
	destination, err := s.RequireMembership(ctx, toRoomID, senderID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if destination.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	original, err := s.EventInRoom(ctx, fromRoomID, eventID)
	if err != nil {
		return dbgen.Event{}, err
	}

	// A deleted message is not forwardable. Its content has been withdrawn,
	// and copying it would undo that.
	if original.RedactedAt != nil {
		return dbgen.Event{}, ErrRoomNotFound
	}

	var content struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(original.Content, &content); err != nil {
		return dbgen.Event{}, fmt.Errorf("read the message being forwarded: %w", err)
	}

	sender, err := s.store.GetUserByID(ctx, senderID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read sender: %w", err)
	}

	// The origin is recorded as an address rather than as a room identifier: a
	// reader who cannot see the source room should still be told where this
	// came from, and an identifier would be meaningless to them.
	forwarded := map[string]any{
		"body": content.Body,
		"forwardedFrom": map[string]any{
			"sender": original.SenderAddress,
			"room":   original.RoomAddress,
			"sentAt": original.CreatedAt,
			// The original identifier, so a reader who can see the source can
			// be taken to it — and a reader who cannot simply gets a name.
			"eventId": original.ID,
		},
	}
	if comment != "" {
		forwarded["comment"] = comment
	}

	encoded, err := json.Marshal(forwarded)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode a forwarded message: %w", err)
	}

	return s.append(ctx, destination, sender, events.TypeMessageSent, encoded, clientToken)
}
