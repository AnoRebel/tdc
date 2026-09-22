package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
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
