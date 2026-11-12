package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
appendTo signs an event and adds it to a room's log, inside the caller's
transaction.

The same steps as the rooms package's own append — allocate the next position,
sign, store with the digest of what was signed — done here because they must
share a transaction with whatever the event records. An issue moved and an
event saying so are one fact; written separately, a failure between them would
leave a board showing a move the history never recorded, or a history
recording a move the board never made.

The position is allocated under room_sequences' row lock, which is held until
the transaction ends. Two changes to one project therefore serialise here and
take consecutive positions, whichever issues they touch.
*/
func (s *Service) appendTo(
	ctx context.Context,
	q *dbgen.Queries,
	room dbgen.Room,
	sender dbgen.User,
	eventType events.Type,
	content any,
) (dbgen.Event, error) {
	return s.appendSigned(ctx, q, room, s.rooms.UserAddress(sender.Username), &sender.ID, eventType, content)
}

/*
appendAsServer records something the server did by itself, on a schedule
rather than because anybody asked: the end of a cycle.

The sender is the server's own name, with no person in it, and no account is
linked to the event. Attributing it to whoever made the cycle would be a
record of something they did not do, at a time they may have been asleep.
*/
func (s *Service) appendAsServer(
	ctx context.Context,
	q *dbgen.Queries,
	room dbgen.Room,
	eventType events.Type,
	content any,
) (dbgen.Event, error) {
	return s.appendSigned(ctx, q, room, s.rooms.ServerName(), nil, eventType, content)
}

func (s *Service) appendSigned(
	ctx context.Context,
	q *dbgen.Queries,
	room dbgen.Room,
	senderAddress string,
	senderID *uuid.UUID,
	eventType events.Type,
	content any,
) (dbgen.Event, error) {
	/*
		Never on a room another server orders.

		Projects are not federated, so this cannot happen today — and the
		guard is here because the day it could, taking a position in somebody
		else's order would collide with theirs and stop replication for good.
	*/
	if room.HomeServer != "" && !strings.EqualFold(room.HomeServer, s.rooms.ServerName()) {
		return dbgen.Event{}, ErrReplicatedEvent
	}

	encoded, err := encode(content)
	if err != nil {
		return dbgen.Event{}, err
	}

	sequence, err := q.NextRoomSequence(ctx, room.ID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("allocate sequence: %w", err)
	}

	nonce, err := events.NewNonce()
	if err != nil {
		return dbgen.Event{}, err
	}

	event := events.Event{
		Room:      s.rooms.RoomAddress(room),
		Sender:    senderAddress,
		Origin:    s.rooms.ServerName(),
		Type:      eventType,
		CreatedAt: events.Now(),
		Nonce:     nonce,
		Content:   encoded,
	}
	if err := s.rooms.Keyring().Sign(&event); err != nil {
		return dbgen.Event{}, fmt.Errorf("sign event: %w", err)
	}

	hash := events.HashContent(encoded)
	stored, err := q.AppendEvent(ctx, dbgen.AppendEventParams{
		ID:            event.ID,
		RoomID:        room.ID,
		Sequence:      sequence,
		SenderID:      senderID,
		SenderAddress: event.Sender,
		RoomAddress:   event.Room,
		OriginServer:  event.Origin,
		Type:          string(event.Type),
		Content:       encoded,
		// The digest of what was signed, before jsonb normalises it into
		// something that no longer hashes to the same value.
		ContentHash: &hash,
		CreatedAt:   event.CreatedAt,
		Nonce:       event.Nonce,
		Signature:   event.Signature,
		KeyID:       event.KeyID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("append event: %w", err)
	}
	s.stored(ctx, q, stored)
	return stored, nil
}

// appendProjectEvent appends to a project's own stream, by the project's
// identifier, as the given person.
func (s *Service) appendProjectEvent(
	ctx context.Context,
	q *dbgen.Queries,
	projectID uuid.UUID,
	actor Actor,
	eventType events.Type,
	content any,
) error {
	_, err := s.appendProjectEventReturning(ctx, q, projectID, actor, eventType, content)
	return err
}

func (s *Service) appendProjectEventReturning(
	ctx context.Context,
	q *dbgen.Queries,
	projectID uuid.UUID,
	actor Actor,
	eventType events.Type,
	content any,
) (dbgen.Event, error) {
	stream, err := q.GetRoomByID(ctx, projectID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read project stream: %w", err)
	}
	return s.appendBy(ctx, q, stream, actor, eventType, content)
}

/*
appendBy appends as the person acting: in their own name for somebody here,
and for somebody on another server in this server's, naming them as "by".

This server cannot sign in the name of somebody it does not host — a receiving
server refuses an event whose sender's server is not the one that signed it —
and it should not: what it can truthfully say is that it made this change at
their request, which their server signed. So the event is the server's, and
the person is in its content.
*/
func (s *Service) appendBy(
	ctx context.Context,
	q *dbgen.Queries,
	room dbgen.Room,
	actor Actor,
	eventType events.Type,
	content any,
) (dbgen.Event, error) {
	if actor.Remote() {
		attributed, err := withBy(content, actor.Address)
		if err != nil {
			return dbgen.Event{}, err
		}
		return s.appendAsServer(ctx, q, room, eventType, attributed)
	}
	person, err := q.GetUserByID(ctx, actor.ID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read actor: %w", err)
	}
	return s.appendTo(ctx, q, room, person, eventType, content)
}

// withBy is content with the person a change was made for added as "by".
func withBy(content any, address string) (map[string]any, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("encode content: %w", err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, fmt.Errorf("content is not an object: %w", err)
	}
	fields["by"] = address
	return fields, nil
}

/*
issueContent is what every event about an issue carries to say which issue.

The identifier is what the history index reads; the key is for a person reading
the raw stream, who should not have to look an identifier up to know it is
WEB-12 being talked about.
*/
func issueContent(issue dbgen.Issue, key string, extra map[string]any) map[string]any {
	content := map[string]any{
		"issue": issue.ID.String(),
		"key":   identifier(key, issue.Number),
	}
	for name, value := range extra {
		content[name] = value
	}
	return content
}

// identifier is the name people use for an issue: the project's key and its
// number, WEB-12.
func identifier(key string, number int64) string {
	return fmt.Sprintf("%s-%d", key, number)
}
