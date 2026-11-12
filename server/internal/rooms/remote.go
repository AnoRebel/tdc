package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

/*
People in a room who are not on this server.

The write path, and the reason a room has one authoritative server. A remote
member's message reaches the room by being handed to the room's home server,
which sequences it and signs it, exactly as it would a local member's. The
member's own server does not sequence anything: it asks, and the home server
answers.

That is more round trips than letting each server number its own events, and it
is the entire reason the room has a single order everybody agrees on. A message
that appeared at position 4 on one server and position 6 on another would not
be a performance problem; it would be two different conversations.
*/

// ErrRemoteJoinRefused means the room's own rules refused the join.
var ErrRemoteJoinRefused = errors.New("the room refused the join")

/*
JoinRemote admits somebody from another server, by this room's own rules.

The rules are the same ones a local person meets — the room must be public and
not archived — and that is the point of the requirement. A joining server has
its own idea of who may do what, and none of it applies here: the room's home
server decides who is in its rooms, or membership means nothing.
*/
func (s *Service) JoinRemote(ctx context.Context, room dbgen.Room, address string) error {
	homeServer, err := federation.ServerOf(address)
	if err != nil {
		return err
	}
	if strings.EqualFold(homeServer, s.serverName) {
		return fmt.Errorf("%w: %s is on this server", ErrRemoteJoinRefused, address)
	}

	if !strings.EqualFold(room.HomeServer, s.serverName) {
		// A replica cannot admit anybody: it does not decide membership, and
		// pretending otherwise is how two servers end up with different
		// answers about who is in a room.
		return ErrNotHome
	}

	// Exactly the rules a local join meets, in the same order.
	if room.Visibility != "public" {
		// Reported as missing rather than forbidden, so a private room's
		// existence cannot be probed from another server either.
		return ErrRoomNotFound
	}
	if room.ArchivedAt != nil {
		return ErrArchived
	}

	already, err := s.store.IsRemoteMember(ctx, dbgen.IsRemoteMemberParams{
		RoomID: room.ID, Address: address,
	})
	if err != nil {
		return fmt.Errorf("check remote membership: %w", err)
	}
	if already {
		return nil
	}

	if err := s.store.AddRemoteMember(ctx, dbgen.AddRemoteMemberParams{
		RoomID: room.ID, Address: address, HomeServer: homeServer, Role: "member",
	}); err != nil {
		return fmt.Errorf("add remote member: %w", err)
	}

	/*
		The join is an event like any other, so every participating server
		learns about it through the same replication as everything else.

		Sent by this server rather than attributed to the person joining.
		A membership decision is this server's to make and to state — it is
		what admitted them — and an event claiming to be *from* somebody on
		another server, signed by this one, is precisely the impersonation the
		receiving side refuses. The person is named in the content, which is
		where the subject of a decision belongs.
	*/
	content, err := json.Marshal(map[string]any{"member": address})
	if err != nil {
		return fmt.Errorf("encode join: %w", err)
	}
	if _, err := s.appendFrom(ctx, room, s.serverAddress(), events.TypeMemberJoined, content); err != nil {
		return err
	}
	return nil
}

/*
serverAddress is how this server names itself as the sender of its own
statements.

A room's membership decisions, and anything else the server says rather than
relays, are attributed to the server itself. The form keeps addresses uniform —
everything in the log is name@server — while making it unambiguous that no
person wrote it.
*/
func (s *Service) serverAddress() string { return "server@" + s.serverName }

// LeaveRemote removes somebody on another server from a room.
func (s *Service) LeaveRemote(ctx context.Context, room dbgen.Room, address string) error {
	if !strings.EqualFold(room.HomeServer, s.serverName) {
		return ErrNotHome
	}

	if err := s.store.RemoveRemoteMember(ctx, dbgen.RemoveRemoteMemberParams{
		RoomID: room.ID, Address: address,
	}); err != nil {
		return fmt.Errorf("remove remote member: %w", err)
	}

	content, err := json.Marshal(map[string]any{"member": address})
	if err != nil {
		return fmt.Errorf("encode leave: %w", err)
	}
	// This server's statement about its own room, as with the join above.
	_, err = s.appendFrom(ctx, room, s.serverAddress(), events.TypeMemberLeft, content)
	return err
}

/*
PostRemote accepts a message a member's own server signed.

The event arrives already signed by the sender's home server, and this server
does not re-sign it. That is the whole security property: a server holding this
event later can check the sender's server really produced it, so even this
server — which decides the room's order and could drop or reorder anything —
cannot invent a message attributed to somebody it does not host.

What this server adds is the position. Sequence is deliberately not part of
what is signed, so assigning one leaves the signature intact: ordering is this
server's to decide, authorship is not, and the split falls exactly where the
trust does.

Membership is checked here because this is the server that decides it. A
forwarding server checking first is welcome to — it saves a round trip — but
its answer is advice, and this one is the ruling.
*/
func (s *Service) PostRemote(
	ctx context.Context,
	room dbgen.Room,
	keyring *events.Keyring,
	event events.Event,
) (dbgen.Event, error) {
	if !strings.EqualFold(room.HomeServer, s.serverName) {
		return dbgen.Event{}, ErrNotHome
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	/*
		Verified before anything else, and against the sender's own server's
		keys rather than this server's. An event this server cannot verify is
		one it must not put its name to by sequencing it.
	*/
	if err := federation.Accept(event, keyring, s.RoomAddress(room), event.Origin); err != nil {
		return dbgen.Event{}, err
	}

	member, err := s.remotelyMember(ctx, room, event.Sender)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("check remote membership: %w", err)
	}
	if !member {
		return dbgen.Event{}, ErrNotAMember
	}

	sequence, err := s.nextSequence(ctx, room)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("allocate sequence: %w", err)
	}

	stored, err := s.store.AppendEvent(ctx, dbgen.AppendEventParams{
		ID:       event.ID,
		RoomID:   room.ID,
		Sequence: sequence,
		// No local user: the sender has no account here, which is what makes
		// them remote.
		SenderID:      nil,
		SenderAddress: event.Sender,
		RoomAddress:   event.Room,
		OriginServer:  event.Origin,
		Type:          string(event.Type),
		Content:       event.Content,
		ContentHash:   pointerTo(events.HashContent(event.Content)),
		CreatedAt:     event.CreatedAt,
		Nonce:         event.Nonce,
		// The sender's server's signature, carried unchanged. Re-signing here
		// would replace a proof of authorship with a claim by this server.
		Signature: event.Signature,
		KeyID:     event.KeyID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("append event: %w", err)
	}
	s.told(ctx, stored)
	return stored, nil
}

/*
appendFrom writes an event attributed to an address rather than a local user.

The sibling of append, and deliberately a separate function rather than a
nullable parameter on it: every line that differs is a line where a remote
sender is not a local one, and merging them would mean a chain of conditionals
in the path that every local message also takes.

This server signs it, because this server is accepting it — Origin is who
accepted an event, not who wrote it. The sender's own server signed the request
that carried it here, which is what proved the attribution; the event's
signature is this server saying "I accepted this, at this position".
*/
func (s *Service) appendFrom(
	ctx context.Context,
	room dbgen.Room,
	sender string,
	eventType events.Type,
	content json.RawMessage,
) (dbgen.Event, error) {
	sequence, err := s.nextSequence(ctx, room)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("allocate sequence: %w", err)
	}

	nonce, err := events.NewNonce()
	if err != nil {
		return dbgen.Event{}, err
	}

	event := events.Event{
		Room:      s.RoomAddress(room),
		Sender:    sender,
		Origin:    s.serverName,
		Type:      eventType,
		CreatedAt: events.Now(),
		Nonce:     nonce,
		Content:   content,
	}
	if err := s.keyring.Sign(&event); err != nil {
		return dbgen.Event{}, fmt.Errorf("sign event: %w", err)
	}

	stored, err := s.store.AppendEvent(ctx, dbgen.AppendEventParams{
		ID:       event.ID,
		RoomID:   room.ID,
		Sequence: sequence,
		// No local user: the sender is somebody this server has no account
		// for, which is exactly what makes them remote.
		SenderID:      nil,
		SenderAddress: event.Sender,
		RoomAddress:   event.Room,
		OriginServer:  event.Origin,
		Type:          string(event.Type),
		Content:       content,
		ContentHash:   pointerTo(events.HashContent(content)),
		CreatedAt:     event.CreatedAt,
		Nonce:         event.Nonce,
		Signature:     event.Signature,
		KeyID:         event.KeyID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("append event: %w", err)
	}
	s.told(ctx, stored)
	return stored, nil
}

// RemoteMembers lists the people in a room who are on other servers.
func (s *Service) RemoteMembers(ctx context.Context, roomID uuid.UUID) ([]dbgen.RemoteMember, error) {
	return s.store.RemoteMembers(ctx, roomID)
}
