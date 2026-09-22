package rooms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

/*
A room that exists on more than one server.

Federating a room is always an explicit act about that room. Enabling
federation on a server does not federate anything it already holds — a server
operator turning the feature on must not thereby publish every conversation
their community has had.

What crosses is the event log, unchanged. A receiving server stores what the
home server assigned, including the sequence, and never renumbers: the moment a
second server starts deciding a room's order, the two can disagree, and there
is no correct way to reconcile that. So there is exactly one server deciding,
always, and the others replicate.
*/

var (
	// ErrNotFederated means a room has not been opened to peers.
	ErrNotFederated = errors.New("room is not federated")
	// ErrNotParticipating means a peer has no part in a room.
	ErrNotParticipating = errors.New("server does not participate in that room")
	// ErrNotHome means this server does not own the room.
	ErrNotHome = errors.New("this server is not the room's home server")
)

// Federate opens a room to peers, or closes it.
//
// Closing does not remove the peers: an operator turning federation off for a
// room while investigating something should be able to turn it back on without
// reconstructing who was participating.
func (s *Service) Federate(ctx context.Context, roomID uuid.UUID, federated bool) error {
	return s.store.SetRoomFederated(ctx, dbgen.SetRoomFederatedParams{
		ID: roomID, Federated: federated,
	})
}

/*
AddPeer gives a peer a part in a room.

The peer is stored as a host, matching how the trust lists compare servers —
the same peer written two ways would otherwise become two rows with two
cursors, each believing it had told the peer a different amount.
*/
func (s *Service) AddPeer(ctx context.Context, roomID uuid.UUID, peer string) error {
	host, err := federation.Host(peer)
	if err != nil {
		return fmt.Errorf("peer %q is not an address: %w", peer, err)
	}

	/*
		Both, because they answer different questions.

		The host is who the peer is, compared the way the trust lists compare
		servers. The address is where to send, which needs the scheme and port
		the operator gave — assuming https on the default port is right in
		production and wrong everywhere a server runs behind another port.
	*/
	address := strings.TrimSpace(peer)
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}

	return s.store.AddRoomPeer(ctx, dbgen.AddRoomPeerParams{
		RoomID: roomID, Peer: host, Address: strings.TrimRight(address, "/"),
	})
}

// RemovePeer ends a peer's part in a room. What it already holds stays on its
// own server; this stops anything further being sent.
func (s *Service) RemovePeer(ctx context.Context, roomID uuid.UUID, peer string) error {
	host, err := federation.Host(peer)
	if err != nil {
		return fmt.Errorf("peer %q is not an address: %w", peer, err)
	}
	return s.store.RemoveRoomPeer(ctx, dbgen.RemoveRoomPeerParams{RoomID: roomID, Peer: host})
}

// Peers lists the servers participating in a room.
func (s *Service) Peers(ctx context.Context, roomID uuid.UUID) ([]dbgen.RoomPeer, error) {
	return s.store.RoomPeers(ctx, roomID)
}

/*
EventsForPeer returns a room's events after a peer's cursor.

The peer's participation is checked by the query itself rather than beforehand,
so there is no path through this code where a caller forgets: a peer with no
part in the room matches no rows and receives nothing, which is the same answer
as a room with no new events and deliberately so — telling the two apart would
say whether a room exists.
*/
func (s *Service) EventsForPeer(
	ctx context.Context,
	roomID uuid.UUID,
	peer string,
	since int64,
	limit int32,
) ([]events.Event, error) {
	host, err := federation.Host(peer)
	if err != nil {
		return nil, fmt.Errorf("peer %q is not an address: %w", peer, err)
	}

	rows, err := s.store.EventsForPeerSince(ctx, dbgen.EventsForPeerSinceParams{
		RoomID:   roomID,
		Peer:     host,
		Sequence: since,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("read events for %s: %w", host, err)
	}

	verifiable := make([]events.Event, 0, len(rows))
	for _, row := range rows {
		verifiable = append(verifiable, Verifiable(row))
	}
	return verifiable, nil
}

/*
AcceptForeignEvents verifies and stores events delivered by another server.

Returns how many were newly stored, which is not the number delivered: a
redelivery writes nothing. The caller reports that number rather than the
length of the batch, so a peer retrying a batch it already sent sees an honest
answer instead of one implying it did something.

Every event is verified before any is stored, and a single failure refuses the
whole batch. Storing the valid prefix of a batch containing a forgery would
leave the room half-updated from a delivery nobody should have trusted, and
would mean the peer's cursor advancing past events that were rejected.
*/
// AuthorKeys resolves the published keys of whichever server wrote an event.
//
// A room's events are not all written by the same server once remote members
// post, so verifying a batch means asking for keys by origin rather than
// assuming the deliverer's.
type AuthorKeys interface {
	Keys(ctx context.Context, server string) (*events.Keyring, error)
}

func (s *Service) AcceptForeignEvents(
	ctx context.Context,
	room dbgen.Room,
	origin string,
	keyring *events.Keyring,
	authors AuthorKeys,
	batch []events.Event,
) (int, error) {
	roomAddress := s.RoomAddress(room)

	if !room.Federated {
		return 0, ErrNotFederated
	}

	/*
		The room's home server decides its contents, and it is not this one.

		A server storing foreign events for a room it owns would be accepting
		somebody else's account of a conversation it is itself authoritative
		for — which is the point at which two servers begin to disagree.
	*/
	if strings.EqualFold(room.HomeServer, s.serverName) {
		return 0, ErrNotHome
	}

	/*
		Each event is verified against its own author's keys, not the
		deliverer's — a remote member's message is written by their server and
		passed on by the home server, so those differ.
	*/
	for _, event := range batch {
		verifier, err := s.keysFor(ctx, event.Origin, origin, keyring, authors)
		if err != nil {
			return 0, fmt.Errorf("event %s: %w", event.ID, err)
		}

		if err := federation.AcceptDelivered(
			event, verifier, roomAddress, room.HomeServer, origin,
		); err != nil {
			return 0, fmt.Errorf("event %s: %w", event.ID, err)
		}
	}

	stored := 0
	for _, event := range batch {
		rows, err := s.storeForeign(ctx, room, event)
		if err != nil {
			return stored, err
		}
		stored += int(rows)
	}
	return stored, nil
}

// storeForeign writes one verified event, keeping the sequence its home server
// assigned. Writing nothing when the event is already held is what makes
// redelivery a no-op rather than an error the caller has to interpret.
func (s *Service) storeForeign(ctx context.Context, room dbgen.Room, event events.Event) (int64, error) {
	/*
		The digest of the content as it was signed, recorded alongside it.

		Postgres normalises jsonb — keys reordered, spacing changed — so the
		bytes read back are not the bytes that were signed. Without the digest
		captured here, this event would stop verifying the moment it was read
		from the database, on the server that had just verified it.
	*/
	digest := events.HashContent(event.Content)

	rows, err := s.store.StoreForeignEvent(ctx, dbgen.StoreForeignEventParams{
		ID:            event.ID,
		RoomID:        room.ID,
		Sequence:      event.Sequence,
		SenderAddress: event.Sender,
		RoomAddress:   event.Room,
		OriginServer:  event.Origin,
		Type:          string(event.Type),
		Content:       event.Content,
		ContentHash:   pointerTo(digest),
		CreatedAt:     event.CreatedAt,
		Nonce:         event.Nonce,
		Signature:     event.Signature,
		KeyID:         event.KeyID,
	})
	if err != nil {
		return 0, fmt.Errorf("store event %s: %w", event.ID, err)
	}
	return rows, nil
}

// FederatedRoom finds a room by its slug, refusing one that is not federated
// so that a peer cannot learn which rooms exist by asking about each in turn.
func (s *Service) FederatedRoom(ctx context.Context, slug string) (dbgen.Room, error) {
	folded := strings.ToLower(slug)
	room, err := s.store.FederatedRoomByAddress(ctx, &folded)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Room{}, ErrNotFederated
	}
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("find federated room %q: %w", slug, err)
	}
	return room, nil
}

// AdvancePeer records how far a peer has been told, never backwards.
func (s *Service) AdvancePeer(ctx context.Context, roomID uuid.UUID, peer string, cursor int64) error {
	return s.store.AdvanceRoomPeerCursor(ctx, dbgen.AdvanceRoomPeerCursorParams{
		RoomID: roomID, Peer: peer, Cursor: cursor,
	})
}

// ByID reads a room without any membership check.
//
// For the federation paths, which decide access by a peer's participation in
// the room rather than by a person's membership of it — the two are different
// questions and conflating them would ask the wrong one.
func (s *Service) ByID(ctx context.Context, roomID uuid.UUID) (dbgen.Room, error) {
	room, err := s.store.GetRoomByID(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Room{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("read room %s: %w", roomID, err)
	}
	return room, nil
}

/*
Adopt creates the local replica of a room another server owns.

Distinct from creating a room, and deliberately so. A room created here names
this server as its home, which is right for a room this community started and
wrong for one it is only replicating — a replica created that way would then
correctly refuse every event its real home server sent it, for the stated
reason that this server is the home server, which is the most confusing
possible way to be told the room was set up wrongly.

The peer named here is where the room actually lives, so the replica knows
which server's signatures to expect on its events.
*/
func (s *Service) Adopt(
	ctx context.Context,
	slug, name, homeServer, visibility string,
) (dbgen.Room, error) {
	host, err := federation.Host(homeServer)
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("home server %q is not an address: %w", homeServer, err)
	}
	if strings.EqualFold(host, s.serverName) {
		return dbgen.Room{}, fmt.Errorf("%w: %s is this server", ErrNotHome, host)
	}

	if visibility == "" {
		visibility = "public"
	}

	folded := strings.ToLower(slug)
	room, err := s.store.AdoptForeignRoom(ctx, dbgen.AdoptForeignRoomParams{
		ID:         uuid.New(),
		Slug:       &slug,
		SlugFolded: &folded,
		Name:       name,
		Visibility: visibility,
		HomeServer: host,
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("adopt room %q from %s: %w", slug, host, err)
	}
	return room, nil
}

// HighestSequence is the furthest this server has stored for a room, which is
// what a catch-up request asks from.
func (s *Service) HighestSequence(ctx context.Context, roomID uuid.UUID) (int64, error) {
	return s.store.HighestSequenceInRoom(ctx, roomID)
}

// DeliveryFailed counts a failure against a peer and schedules the next try,
// abandoning it past the ceiling. Returns the peer's state afterwards, so a
// caller can tell an operator what happened without a second query.
func (s *Service) DeliveryFailed(
	ctx context.Context,
	roomID uuid.UUID,
	peer, reason string,
	attempts int,
) (dbgen.RoomPeer, error) {
	delay := federation.RetryDelay(attempts)

	return s.store.RecordDeliveryFailure(ctx, dbgen.RecordDeliveryFailureParams{
		RoomID:     roomID,
		Peer:       peer,
		LastError:  reason,
		RetryAfter: pgInterval(delay),
		Ceiling:    int32(federation.MaxDeliveryAttempts),
	})
}

// DeliverySucceeded clears a peer's failure state: consecutive failures are
// what matter, and a peer that answers has none.
func (s *Service) DeliverySucceeded(ctx context.Context, roomID uuid.UUID, peer string) error {
	return s.store.ClearDeliveryFailures(ctx, dbgen.ClearDeliveryFailuresParams{
		RoomID: roomID, Peer: peer,
	})
}

// ResumePeer is an operator deciding to try an abandoned peer again.
func (s *Service) ResumePeer(ctx context.Context, roomID uuid.UUID, peer string) error {
	return s.store.ResumePeer(ctx, dbgen.ResumePeerParams{RoomID: roomID, Peer: peer})
}

// PeersDue lists peers with events owed and nothing stopping delivery.
func (s *Service) PeersDue(ctx context.Context, limit int32) ([]dbgen.RoomPeer, error) {
	return s.store.PeersDueForDelivery(ctx, limit)
}

// pgInterval turns a duration into the interval the query expects.
func pgInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

/*
keysFor resolves the keys that verify one event's author.

Three cases, and the third is easy to miss. The author may be the peer that
delivered the batch, in which case its keys are already in hand. It may be
another server, which is what a remote member's message relayed by the home
server looks like — those keys are fetched.

Or it may be *this* server: a message one of our own users posted into a room
elsewhere, which the home server sequenced and has now sent back to us. Asking
discovery for our own keys would mean this server making an HTTP request to
itself, which fails for every deployment not reachable at its own canonical
address from inside — and would be absurd even where it worked, since the keys
are already here.
*/
func (s *Service) keysFor(
	ctx context.Context,
	author, deliveredBy string,
	delivererKeys *events.Keyring,
	authors AuthorKeys,
) (*events.Keyring, error) {
	if strings.EqualFold(author, s.serverName) {
		return s.keyring, nil
	}
	if strings.EqualFold(author, deliveredBy) {
		return delivererKeys, nil
	}

	keyring, err := authors.Keys(ctx, author)
	if err != nil {
		return nil, fmt.Errorf("cannot verify %s: %w", author, err)
	}
	return keyring, nil
}
