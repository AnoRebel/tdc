package rooms_test

import (
	"context"
	"slices"
	"testing"
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

	if err := service.Archive(ctx, retired.ID, owner.ID); err != nil {
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
