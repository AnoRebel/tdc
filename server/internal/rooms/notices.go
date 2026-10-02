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
TypeEngagementChanged records that an engagement attached to a conversation
moved from one state to another.

An event in the conversation's own log rather than a message: it is not
something either person said, it is not unread, and a client rendering a
timeline of messages passes over it. What it buys is delivery — the people in
the conversation hear about it the moment it happens, over the connection they
already hold, and a client that missed it finds it on reconnection like any
other event.

A local type rather than one of the protocol's: an engagement's conversation is
a direct conversation, which never leaves this server.
*/
const TypeEngagementChanged events.Type = "engagement.changed"

// RecordNotice appends a notice of something that happened to a conversation,
// in the name of whoever caused it, who must be in it.
func (s *Service) RecordNotice(
	ctx context.Context,
	roomID, actorID uuid.UUID,
	eventType events.Type,
	content any,
) (dbgen.Event, error) {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Event{}, err
	}
	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read actor: %w", err)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode notice: %w", err)
	}
	return s.append(ctx, room, actor, eventType, encoded, "")
}
