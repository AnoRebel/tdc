package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
The peer list is reachable by a signed-in person, not gated behind federation.

It shares a path prefix with the federated routes, which are refused outright
while federation is off — so this is checking that a route registered outside
that group did not get swallowed by it.
*/
func TestTheConnectedServerListIsNotBehindTheFederationGate(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// Federation is off, and this must still answer.
	recorder := call(t, handler, http.MethodGet, "/api/federation/servers", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("the connected-server list returned %d with federation off, want 200", recorder.Code)
	}
	// A bare array, which is what the clients were written to read.
	if !strings.HasPrefix(strings.TrimSpace(recorder.Body.String()), "[") {
		t.Errorf("the connected-server list is not an array: %s", recorder.Body)
	}
}

// And it needs a session: the shape of the network is for this community, not
// for anybody who can reach the server.
func TestTheConnectedServerListNeedsASession(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/federation/servers", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated peer list returned %d, want 401", recorder.Code)
	}
}

/*
The federated room listing refuses an unsigned caller.

It is the endpoint a peer browses this server through, so an unauthenticated
caller reaching it would make every public room listable by anybody — which is
the same information a signed-in person can see, but disclosed to a stranger
who has not identified their server.
*/
func TestTheFederatedRoomListRefusesAnUnsignedCaller(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	set := call(t, handler, http.MethodPut, "/api/admin/settings/federation.enabled",
		map[string]any{"value": "true"}, owner)
	if set.Code != http.StatusNoContent {
		t.Fatalf("enabling federation returned %d", set.Code)
	}

	recorder := call(t, handler, http.MethodGet, "/api/federation/rooms", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("an unsigned room list returned %d, want 401", recorder.Code)
	}
}

/*
A private room is not among what this server publishes to a peer.

The real test of the filtering, against a real database: the mock-peer version
of this proved only that a list with no private room in it has no private room
in it. Here the room genuinely exists, is genuinely private, and must be
genuinely absent.
*/
func TestPrivateRoomsAreNotPublishedToPeers(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://a.example.org")
	owner := enableFederation(t, handler)

	makeRoom(t, handler, owner, "general", "public")
	makeRoom(t, handler, owner, "secrets", "private")

	server := serverBehind(t, handler)
	published, err := server.PublishedRoomsForTest(t.Context())
	if err != nil {
		t.Fatalf("published rooms: %v", err)
	}

	var names []string
	for _, room := range published {
		names = append(names, room.Slug)
		if room.Slug == "secrets" {
			t.Error("a private room is published to peers")
		}
	}

	if !slices.Contains(names, "general") {
		t.Errorf("the public room is missing from %v", names)
	}
}

/*
A limited server answers a peer it has listed, and only that peer.

The two halves matter equally. An earlier demonstration showed the refusal
working and the admission not, which is a server that refuses everybody — and
would have passed a test that only checked the refusal.
*/
func TestALimitedServerAnswersAListedPeerAndNobodyElse(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://b.example.org")
	owner := enableFederation(t, handler)

	makeRoom(t, handler, owner, "market", "public")

	setSetting(t, handler, owner, "server.visibility", "limited")
	setSetting(t, handler, owner, "server.visible_to", "a.example.org")

	server := serverBehind(t, handler)

	listed := server.MayDiscoverForTest(t.Context(), "https://a.example.org:8443")
	if !listed {
		t.Error("a listed peer was refused by a limited server")
	}

	unlisted := server.MayDiscoverForTest(t.Context(), "https://stranger.example.org")
	if unlisted {
		t.Error("an unlisted peer was admitted by a limited server")
	}

	// And a caller with no verified peer identity at all is refused, which is
	// what an unsigned request looks like here.
	if server.MayDiscoverForTest(t.Context(), "") {
		t.Error("a caller with no verified identity was admitted")
	}
}

/*
On a signed route, the header cannot buy admission the signature did not.

This is the whole of what 6.1 changes. Before federation existed, the only
source of a peer identity was a header anybody could set; now a federated
request carries one the sender proved. A stranger setting the header on a
signed route must gain nothing by it — the route refuses them for having no
signature long before visibility is considered.
*/
func TestTheForgeableHeaderBuysNothingOnASignedRoute(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://b.example.org")
	owner := enableFederation(t, handler)

	makeRoom(t, handler, owner, "market", "public")
	setSetting(t, handler, owner, "server.visibility", "limited")
	setSetting(t, handler, owner, "server.visible_to", "a.example.org")

	// Claiming to be the one listed peer, with no signature to show for it.
	request := httptest.NewRequest(http.MethodGet, "/api/federation/rooms", nil)
	request.Header.Set("TDC-Peer-Server", "a.example.org")
	request.Header.Set("TDC-Origin-Server", "https://a.example.org")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("a header-only caller got %d from a signed route, want 401", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "market") {
		t.Error("the header disclosed a room on a signed route")
	}
}

/*
A peer with no participant in a room receives no mention of it.

Scoped by actual participation, never by content visibility alone. A private
room is excluded from browsing because it is private; a *public* room the peer
has no part in must also be absent from its event deliveries, or an allowlist
that permits discovery would permit reading everything.
*/
func TestAPeerReceivesNothingForARoomItDoesNotParticipateIn(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://a.example.org")
	owner := enableFederation(t, handler)

	shared := makeRoom(t, handler, owner, "shared", "public")
	unshared := makeRoom(t, handler, owner, "unshared", "public")

	/*
		Both rooms are federated and both have a peer — a different one for
		the unshared room.

		An earlier version left the second room with no peers at all, so the
		join in the query matched nothing whatever the scoping did, and the
		test passed with participation entirely disabled. A room with a
		*different* participant is the case that distinguishes scoping by
		participation from scoping by visibility.
	*/
	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+shared+"/federation",
		map[string]any{"federated": true, "peers": []string{"b.example.org"}}, owner, http.StatusOK)
	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+unshared+"/federation",
		map[string]any{"federated": true, "peers": []string{"c.example.org"}}, owner, http.StatusOK)

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+shared+"/messages",
		map[string]any{"body": "in the shared room"}, owner, http.StatusCreated)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+unshared+"/messages",
		map[string]any{"body": "in the unshared room"}, owner, http.StatusCreated)

	server := serverBehind(t, handler)
	ctx := t.Context()

	// The room it participates in: events reach it.
	inShared, err := server.EventsForPeerTest(ctx, shared, "b.example.org")
	if err != nil {
		t.Fatalf("events for the shared room: %v", err)
	}
	if len(inShared) == 0 {
		t.Fatal("a participating peer received nothing from the room it participates in")
	}

	// The room it does not: nothing at all, not a refusal.
	inUnshared, err := server.EventsForPeerTest(ctx, unshared, "b.example.org")
	if err != nil {
		t.Fatalf("events for the unshared room: %v", err)
	}
	if len(inUnshared) != 0 {
		t.Fatalf("a peer with no part in a room received %d of its events", len(inUnshared))
	}
}

/*
A federated event delivery carries nothing beyond the room it is about.

The concern is not what the room contains but what travels alongside it: no
local user directory, no presence, no membership of other rooms. Asserted by
reading every field of what would actually be sent, rather than by trusting
that nothing was added.
*/
func TestFederatedEventsCarryNothingBeyondTheirRoom(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://a.example.org")
	owner := enableFederation(t, handler)

	shared := makeRoom(t, handler, owner, "shared", "public")
	makeRoom(t, handler, owner, "elsewhere", "private")

	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+shared+"/federation",
		map[string]any{"federated": true, "peers": []string{"b.example.org"}}, owner, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+shared+"/messages",
		map[string]any{"body": "hello"}, owner, http.StatusCreated)

	server := serverBehind(t, handler)
	batch, err := server.EventsForPeerTest(t.Context(), shared, "b.example.org")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(batch) == 0 {
		t.Fatal("nothing to inspect")
	}

	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	payload := string(encoded)

	// Nothing about another room, and nothing about anybody's account beyond
	// the addresses the events themselves carry.
	for _, forbidden := range []string{"elsewhere", "passwordHash", "email", "presence", "lastSeen"} {
		if strings.Contains(payload, forbidden) {
			t.Errorf("a federated delivery mentions %q: %s", forbidden, payload)
		}
	}

	// And every event named is in the room being delivered.
	for _, event := range batch {
		if !strings.HasPrefix(event.Room, "shared@") {
			t.Errorf("an event for room %q appeared in the shared room's delivery", event.Room)
		}
	}
}

/*
This server does not store content its own policy forbids, whoever sent it.

The spec is explicit about this, and a filter that ran only on locally composed
messages would leave federation as the way around it — post the forbidden thing
from a server without the filter and have it replicated in.

Refusal is the only action available on arrival. Masking rewrites a body the
signature covers, producing an event that verifies nowhere.
*/
func TestAFederatedEventIsRefusedByTheLocalContentPolicy(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://b.example.org")
	owner := enableFederation(t, handler)

	setSetting(t, handler, owner, "filter.words", "forbidden")
	setSetting(t, handler, owner, "filter.action", "block")

	server := serverBehind(t, handler)
	ctx := t.Context()

	clean := []events.Event{{ID: "$clean", Content: json.RawMessage(`{"body":"ordinary words"}`)}}
	if refused := server.RefusedByPolicyForTest(ctx, clean); refused != "" {
		t.Errorf("an ordinary event was refused: %s", refused)
	}

	forbidden := []events.Event{
		{ID: "$clean", Content: json.RawMessage(`{"body":"ordinary words"}`)},
		{ID: "$dirty", Content: json.RawMessage(`{"body":"a forbidden thing"}`)},
	}
	refused := server.RefusedByPolicyForTest(ctx, forbidden)
	if refused != "$dirty" {
		t.Fatalf("the policy refused %q, want $dirty", refused)
	}
}

/*
And under any action but blocking, nothing arriving is refused.

Flagging and masking are for events this server stores; an arriving event that
was masked would no longer verify, so the gentler actions deliberately do
nothing here rather than doing something harmful.
*/
func TestFlaggingDoesNotRefuseFederatedEvents(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://b.example.org")
	owner := enableFederation(t, handler)

	setSetting(t, handler, owner, "filter.words", "forbidden")
	setSetting(t, handler, owner, "filter.action", "flag")

	server := serverBehind(t, handler)
	batch := []events.Event{{ID: "$dirty", Content: json.RawMessage(`{"body":"a forbidden thing"}`)}}

	if refused := server.RefusedByPolicyForTest(t.Context(), batch); refused != "" {
		t.Errorf("flagging refused an arriving event (%s); it should only refuse under block", refused)
	}
}
