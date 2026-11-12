package rooms_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
An archived room is not published to peers.

A room nobody can post in is not one to invite a stranger's server into, and
listing it would mean a peer browsing sees rooms that cannot be joined.

Tested here rather than through the API because archiving has no HTTP route —
an earlier version of this went through a path that does not exist and
therefore proved nothing.
*/
func TestArchivedRoomsAreNotPublished(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	live := makeRoom(t, service, owner.ID, "general")
	retired := makeRoom(t, service, owner.ID, "retired")

	if _, err := service.Archive(ctx, retired.ID, owner.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	published, err := service.Published(ctx, 100)
	if err != nil {
		t.Fatalf("Published: %v", err)
	}

	var slugs []string
	for _, room := range published {
		slugs = append(slugs, room.Slug)
	}

	if slices.Contains(slugs, "retired") {
		t.Errorf("an archived room is published to peers: %v", slugs)
	}
	if !slices.Contains(slugs, "general") {
		t.Errorf("the live room is missing from %v", slugs)
	}
	_ = live
}

/*
A peer that has never been delivered to is still listed.

This is the common case, not an edge: a peer is listed the moment it is added,
before anything has been sent. An earlier version aggregated the last delivery
time with a cast that made it non-nullable, so the scan failed for exactly
every peer somebody had just added — the list was empty precisely when it
mattered most.
*/
func TestAPeerWithNothingDeliveredIsStillListed(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	room := makeRoom(t, service, owner.ID, "general")

	if err := service.AddPeer(ctx, room.ID, "http://b.localhost:8112"); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	peers, err := service.KnownPeers(ctx)
	if err != nil {
		t.Fatalf("KnownPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("KnownPeers returned %d peers, want 1", len(peers))
	}

	peer := peers[0]
	if peer.Server != "b.localhost" {
		t.Errorf("Server = %q, want b.localhost", peer.Server)
	}
	// The address is kept alongside the host, because one is identity and the
	// other is where to actually send.
	if peer.Address != "http://b.localhost:8112" {
		t.Errorf("Address = %q, want the address it was added with", peer.Address)
	}
	if !peer.Reachable {
		t.Error("a peer nothing has been tried against is not unreachable")
	}
}

/*
A peer shared across several rooms is one peer, counted.

Peers are recorded per room, so a server sharing four rooms has four rows.
"Who are we connected to" asks about servers, not rows.
*/
func TestAPeerInSeveralRoomsIsListedOnce(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	for _, slug := range []string{"general", "market", "harbour"} {
		room := makeRoom(t, service, owner.ID, slug)
		if err := service.AddPeer(ctx, room.ID, "http://b.localhost:8112"); err != nil {
			t.Fatalf("AddPeer: %v", err)
		}
	}

	peers, err := service.KnownPeers(ctx)
	if err != nil {
		t.Fatalf("KnownPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("one peer in three rooms was listed %d times", len(peers))
	}
	if peers[0].Rooms != 3 {
		t.Errorf("Rooms = %d, want 3", peers[0].Rooms)
	}
}

/*
A replica never numbers events in a room it does not own.

This is the bug that matters most in this phase, and it was invisible until
somebody opened a replicated room: joining writes a member.joined event, which
took sequence 1 — the position the home server was also about to use. Its own
event is then rejected by the unique index on (room, sequence), and replication
into that room is broken for good.

So a local join on a replica records the membership and writes no event, and
every other path that would allocate a position refuses outright.
*/
func TestAReplicaAssignsNoSequences(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	replica, err := service.Adopt(ctx, "townsquare", "Town square", "a.example.org", "public")
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	// Joining is the ordinary thing a person does, and must cost no position.
	if err := service.Join(ctx, replica.ID, owner.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	highest, err := service.HighestSequence(ctx, replica.ID)
	if err != nil {
		t.Fatalf("HighestSequence: %v", err)
	}
	if highest != 0 {
		t.Fatalf("a local join took sequence %d in a replicated room", highest)
	}

	// And posting locally is refused rather than silently taking one.
	if _, err := service.SendMessage(ctx, replica.ID, owner.ID, "local write", ""); !errors.Is(err, rooms.ErrNotHome) {
		t.Fatalf("posting into a replica = %v, want ErrNotHome", err)
	}
}

/*
The home server's events fit, because nothing local took their positions.

The other half: a test that only checked "no local sequence" would pass against
a replica that refused everything. What must work is the home server's own
event arriving at the position it assigned.
*/
func TestAHomeServersEventFitsIntoAReplicaAfterALocalJoin(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	replica, err := service.Adopt(ctx, "townsquare", "Town square", "a.example.org", "public")
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := service.Join(ctx, replica.ID, owner.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	homeKey, err := events.NewKeyPair("home-key")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	homeRing := events.NewKeyring(homeKey)
	verifying, err := events.KeyringFromPublished(homeRing.Published())
	if err != nil {
		t.Fatalf("KeyringFromPublished: %v", err)
	}

	nonce, err := events.NewNonce()
	if err != nil {
		t.Fatalf("NewNonce: %v", err)
	}
	incoming := events.Event{
		Room:      "townsquare@a.example.org",
		Sender:    "someone@a.example.org",
		Origin:    "a.example.org",
		Type:      events.TypeMessageSent,
		Sequence:  1,
		CreatedAt: events.Now(),
		Nonce:     nonce,
		Content:   json.RawMessage(`{"body":"the home server's first event"}`),
	}
	if err := homeRing.Sign(&incoming); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	stored, err := service.AcceptForeignEvents(ctx, replica, "a.example.org", verifying,
		keysOnlyFromDeliverer{}, []events.Event{incoming})
	if err != nil {
		t.Fatalf("the home server's sequence 1 was refused: %v", err)
	}
	if len(stored) != 1 || stored[0].ID != incoming.ID {
		t.Fatalf("stored %d events, want the one delivered", len(stored))
	}
}

// keysOnlyFromDeliverer stands in for discovery in tests where every event was
// written by the server that delivered it, so no lookup should happen.
type keysOnlyFromDeliverer struct{}

func (keysOnlyFromDeliverer) Keys(context.Context, string) (*events.Keyring, error) {
	return nil, errors.New("no author keys should be needed here")
}
