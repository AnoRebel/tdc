package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

/*
callPeer is another server as far as calls need one: a published key, signed
requests to this server, and a record of the call requests this server sends
it.
*/
type callPeer struct {
	*fakePeer
	mu       sync.Mutex
	received []map[string]any // each call request, with "action" added
	answers  map[string]any   // what to answer an action with, beyond 202
}

func newCallPeer(t *testing.T, id string, named ...string) *callPeer {
	t.Helper()
	key, err := events.NewKeyPair(id)
	if err != nil {
		t.Fatal(err)
	}
	peer := &callPeer{fakePeer: &fakePeer{keyring: events.NewKeyring(key), files: map[string][]byte{}}, answers: map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc(federation.WellKnownPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(federation.Document{
			CanonicalAddress: peer.address, Keys: peer.keyring.Published(),
			Versions: federation.SupportedVersions(), Enabled: true,
		})
	})
	mux.HandleFunc("/api/federation/calls/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		_ = json.Unmarshal(body, &request)
		if request == nil {
			request = map[string]any{}
		}
		request["action"] = strings.TrimPrefix(r.URL.Path, "/api/federation/calls/")
		request["signed"] = r.Header.Get("Signature") != ""
		peer.mu.Lock()
		peer.received = append(peer.received, request)
		answer, answers := peer.answers[request["action"].(string)]
		peer.mu.Unlock()
		if answers {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(answer)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	peer.http = httptest.NewServer(mux)
	t.Cleanup(peer.http.Close)
	peer.address = peer.http.URL
	if len(named) > 0 {
		peer.address = strings.Replace(peer.http.URL, "127.0.0.1", named[0], 1)
	}
	return peer
}

// awaitRequest waits for this server to send the peer a call request of a kind.
func (p *callPeer) awaitRequest(t *testing.T, action string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		for _, each := range p.received {
			if each["action"] == action && (match == nil || match(each)) {
				p.mu.Unlock()
				return each
			}
		}
		p.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("this server never sent the peer a %q", action)
	return nil
}

// federatedCalls is this server with calls crossing servers, and a peer it
// shares a room with — which is how it knows where the peer is.
func federatedCalls(t *testing.T, peers ...*callPeer) (http.Handler, string) {
	t.Helper()
	handler := newTestAPIWithAddress(t, thisServer)
	fakes := []*fakePeer{}
	for _, peer := range peers {
		fakes = append(fakes, peer.fakePeer)
	}
	_, founder := sharedRoom(t, handler, fakes...)
	setFeature(t, handler, founder, "calls", true)
	setSetting(t, handler, founder, "calls.federated", "true")
	return handler, founder
}

// noFrame says no frame of a type arrives within a moment.
func firstFrame(t *testing.T, socket *websocket.Conn, frameType string, within time.Duration) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	frame, _ := nextCallFrame(t, ctx, socket, frameType)
	return frame
}

/*
A ring from another server reaches the person only when everybody it must pass
agrees: this server takes calls from other servers, trusts the one ringing, the
person accepts calls from other servers, and the caller is somebody that server
may speak for. Each refusal answers the peer the same as success where telling
them apart would disclose a setting, and none reaches the person.
*/
func TestARingFromAnotherServerReachesOnlyWhomItMay(t *testing.T) {
	home := newCallPeer(t, "home-key")
	handler, founder := federatedCalls(t, home)
	baraka := registerAndSignIn(t, handler, "baraka")

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	live := liveConnection(t, ctx, server, baraka)

	ring := func(from string) (string, int) {
		callID := uuid.NewString()
		answer := home.signed(t, handler, http.MethodPost, "/api/federation/calls/ring", map[string]any{
			"call": callID, "roomId": uuid.NewString(), "to": "baraka@tdc.test",
			"from": map[string]string{"address": from, "displayName": "Amina"},
		})
		return callID, answer.Code
	}
	amina := "amina@" + home.host(t)

	// Every refusal first; then one that is allowed. The first ring to arrive
	// must be that last one, or a refused one got through.
	setSetting(t, handler, founder, "calls.federated", "false")
	if _, code := ring(amina); code != http.StatusNotFound {
		t.Errorf("a ring with calls not federated: %d, want 404", code)
	}
	setSetting(t, handler, founder, "calls.federated", "true")

	setSetting(t, handler, founder, "federation.mode", "blocklist")
	setSetting(t, handler, founder, "federation.blocked_servers", home.host(t))
	if _, code := ring(amina); code != http.StatusUnauthorized {
		t.Errorf("a ring from a blocked server: %d, want 401", code)
	}
	setSetting(t, handler, founder, "federation.blocked_servers", "")

	mustCall(t, handler, http.MethodPut, "/api/me/privacy", map[string]any{"callsFromOtherServers": false}, baraka, http.StatusNoContent)
	if _, code := ring(amina); code != http.StatusAccepted {
		t.Errorf("a ring the person's setting refuses is answered %d, not as any other", code)
	}
	mustCall(t, handler, http.MethodPut, "/api/me/privacy", map[string]any{"callsFromOtherServers": true}, baraka, http.StatusNoContent)

	if _, code := ring("amina@elsewhere.example"); code != http.StatusAccepted {
		t.Errorf("a ring claiming somebody of another server: %d", code)
	}

	allowed, code := ring(amina)
	if code != http.StatusAccepted {
		t.Fatalf("an allowed ring: %d", code)
	}
	frame := firstFrame(t, live, "call.ring", 10*time.Second)
	if frame == nil {
		t.Fatal("an allowed ring never reached the person")
	}
	if got := frame["payload"].(map[string]any)["call"]; got != allowed {
		t.Errorf("the first ring to reach the person was %v, one that should have been refused", got)
	}
}

/*
Only the server that rang somebody into a call may send them anything about it
afterwards: another server, trusted and signing perfectly, is refused.
*/
func TestOnlyACallsHomeDeliversToThePeopleInIt(t *testing.T) {
	home := newCallPeer(t, "home-key")
	stranger := newCallPeer(t, "stranger-key", "localhost")
	handler, _ := federatedCalls(t, home)
	baraka := registerAndSignIn(t, handler, "baraka")
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	live := liveConnection(t, ctx, server, baraka)

	callID := uuid.NewString()
	if code := home.signed(t, handler, http.MethodPost, "/api/federation/calls/ring", map[string]any{
		"call": callID, "roomId": uuid.NewString(), "to": "baraka@tdc.test",
		"from": map[string]string{"address": "amina@" + home.host(t)},
	}).Code; code != http.StatusAccepted {
		t.Fatalf("ring: %d", code)
	}
	deliver := func(peer *callPeer, body string) int {
		return peer.signed(t, handler, http.MethodPost, "/api/federation/calls/deliver", map[string]any{
			"call": callID, "to": "baraka@tdc.test", "type": "call.signal",
			"payload": map[string]any{"call": callID, "from": uuid.NewString(), "signal": map[string]string{"sdp": body}},
		}).Code
	}
	if code := deliver(stranger, "from a stranger"); code != http.StatusNotFound {
		t.Errorf("a delivery from a server that is not the call's home: %d, want 404", code)
	}
	if code := deliver(home, "from home"); code != http.StatusAccepted {
		t.Errorf("a delivery from the call's home: %d", code)
	}
	frame := firstFrame(t, live, "call.signal", 10*time.Second)
	if frame == nil {
		t.Fatal("the home's delivery never reached the person")
	}
	if payload, _ := json.Marshal(frame["payload"]); strings.Contains(string(payload), "stranger") {
		t.Error("a stranger's delivery reached the person")
	}
}

/*
Somebody on another server, rung into a call here, joins it through their
server, shows in it by their own identifier and address, and negotiates with
the people here: what they send is passed to the participant named, and what is
sent to them goes back to their server, signed.
*/
func TestSomebodyFromAnotherServerTakesPartInACallHere(t *testing.T) {
	peer := newCallPeer(t, "peer-key")
	handler, founder := federatedCalls(t, peer)
	room := makeRoom(t, handler, founder, "standup", "public")
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	founderLive := liveConnection(t, ctx, server, founder)

	started := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, founder, http.StatusOK))
	callID := started["id"].(string)
	baraka := "baraka@" + peer.host(t)
	barakaID := uuid.NewString()
	join := func(member, id string) *httptest.ResponseRecorder {
		return peer.signed(t, handler, http.MethodPost, "/api/federation/calls/join", map[string]any{
			"call": callID, "member": member, "id": id, "displayName": "Baraka", "audio": true,
		})
	}

	if code := join(baraka, barakaID).Code; code != http.StatusNotFound {
		t.Errorf("joining a call one was never rung into: %d, want 404", code)
	}

	rung := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call/ring",
		map[string]any{"addresses": []string{baraka}}, founder, http.StatusOK))
	if len(rung["rung"].([]any)) != 1 {
		t.Fatalf("ringing somebody on another server: %v", rung)
	}
	ring := peer.awaitRequest(t, "ring", nil)
	if ring["to"] != baraka || ring["call"] != callID || ring["signed"] != true {
		t.Errorf("the ring sent: %v", ring)
	}

	if code := join("chausiku@elsewhere.example", uuid.NewString()).Code; code != http.StatusForbidden {
		t.Errorf("a server joining somebody not its own: %d, want 403", code)
	}
	if code := join(baraka, accountID(t, handler, founder)).Code; code != http.StatusConflict {
		t.Errorf("joining as somebody here's identifier: %d, want 409", code)
	}

	joined := join(baraka, barakaID)
	if joined.Code != http.StatusOK {
		t.Fatalf("joining: %d %s", joined.Code, joined.Body)
	}
	state := decode(t, joined)
	found := false
	for _, raw := range state["participants"].([]any) {
		participant := raw.(map[string]any)
		if participant["userId"] == barakaID {
			found = true
			if participant["address"] != baraka || participant["guest"] == true {
				t.Errorf("somebody from another server is shown as %v", participant)
			}
		}
	}
	if !found {
		t.Fatalf("the call does not list them: %v", state)
	}

	// From them, to somebody here.
	if code := peer.signed(t, handler, http.MethodPost, "/api/federation/calls/signal", map[string]any{
		"call": callID, "member": baraka, "to": accountID(t, handler, founder), "signal": map[string]string{"sdp": "their offer"},
	}).Code; code != http.StatusAccepted {
		t.Errorf("their signal: %d", code)
	}
	frame := firstFrame(t, founderLive, "call.signal", 10*time.Second)
	if payload, _ := json.Marshal(frame["payload"]); !strings.Contains(string(payload), "their offer") || !strings.Contains(string(payload), barakaID) {
		t.Errorf("their signal arrived as %s", payload)
	}

	// From somebody here, to them: back to their server.
	message, _ := json.Marshal(map[string]any{
		"action": "signal", "call": callID, "to": barakaID, "signal": map[string]string{"sdp": "our answer"},
	})
	if err := founderLive.Write(ctx, websocket.MessageText, message); err != nil {
		t.Fatal(err)
	}
	delivered := peer.awaitRequest(t, "deliver", func(request map[string]any) bool { return request["type"] == "call.signal" })
	if payload, _ := json.Marshal(delivered["payload"]); delivered["to"] != baraka || !strings.Contains(string(payload), "our answer") || delivered["signed"] != true {
		t.Errorf("what reached their server: %v", delivered)
	}

	// Leaving through their server takes them out.
	if code := peer.signed(t, handler, http.MethodPost, "/api/federation/calls/leave", map[string]any{"call": callID, "member": baraka}).Code; code != http.StatusNoContent {
		t.Errorf("leaving: %d", code)
	}
	after := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, founder, http.StatusOK))
	if len(after["participants"].([]any)) != 1 {
		t.Errorf("after they left the call holds %v", after["participants"])
	}
}

/*
Somebody here answering a ring from another server joins that call through
this server, which asks the call's home on their behalf — as themselves, by
their own identifier — passes their negotiation there in order, and keeps
saying they are still connected.
*/
func TestSomebodyHereJoinsACallAnotherServerHolds(t *testing.T) {
	home := newCallPeer(t, "home-key")
	handler, _ := federatedCalls(t, home)
	baraka := registerAndSignIn(t, handler, "baraka")
	barakaID := accountID(t, handler, baraka)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	live := liveConnection(t, ctx, server, baraka)

	callID, roomID, aminaID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	mustCall(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, baraka, http.StatusNotFound)

	if code := home.signed(t, handler, http.MethodPost, "/api/federation/calls/ring", map[string]any{
		"call": callID, "roomId": roomID, "to": "baraka@tdc.test",
		"from": map[string]string{"address": "amina@" + home.host(t)},
	}).Code; code != http.StatusAccepted {
		t.Fatalf("ring: %d", code)
	}
	home.mu.Lock()
	home.answers["join"] = map[string]any{
		"id": callID, "roomId": roomID, "mode": "mesh", "startedAt": time.Now().UTC().Format(time.RFC3339),
		"participants": []map[string]any{{"userId": aminaID}, {"userId": barakaID}},
	}
	home.mu.Unlock()

	joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/calls/"+callID, map[string]any{"audio": true}, baraka, http.StatusOK))
	if joined["id"] != callID {
		t.Errorf("joining answered %v", joined)
	}
	asked := home.awaitRequest(t, "join", nil)
	if asked["member"] != "baraka@tdc.test" || asked["id"] != barakaID || asked["call"] != callID || asked["signed"] != true {
		t.Errorf("the join sent to the call's home: %v", asked)
	}

	for _, body := range []string{"first", "second"} {
		message, _ := json.Marshal(map[string]any{
			"action": "signal", "call": callID, "to": aminaID, "signal": map[string]string{"sdp": body},
		})
		if err := live.Write(ctx, websocket.MessageText, message); err != nil {
			t.Fatal(err)
		}
	}
	home.awaitRequest(t, "signal", func(request map[string]any) bool {
		payload, _ := json.Marshal(request["signal"])
		return strings.Contains(string(payload), "second")
	})
	home.mu.Lock()
	order := []string{}
	for _, each := range home.received {
		if each["action"] == "signal" {
			payload, _ := json.Marshal(each["signal"])
			order = append(order, string(payload))
		}
	}
	home.mu.Unlock()
	if len(order) != 2 || !strings.Contains(order[0], "first") {
		t.Errorf("signals reached the home as %v, want first then second", order)
	}

	serverOf(handler).SweepCalls(ctx)
	home.awaitRequest(t, "present", func(request map[string]any) bool { return request["member"] == "baraka@tdc.test" })

	mustCall(t, handler, http.MethodDelete, "/api/calls/"+callID, nil, baraka, http.StatusNoContent)
	home.awaitRequest(t, "leave", nil)
}

/*
A call in a room shared with another server is held by the room's home: a
member of the room on the other server may start it and join it, and somebody
from that server who is not a member may not.
*/
func TestACallInASharedRoomIsHeldByItsHome(t *testing.T) {
	peer := newCallPeer(t, "peer-key")
	handler, founder := federatedCalls(t, peer)
	join := func(member string) *httptest.ResponseRecorder {
		return peer.signed(t, handler, http.MethodPost, "/api/federation/calls/join", map[string]any{
			"room": "shared", "member": member, "id": uuid.NewString(), "audio": true,
		})
	}
	if code := join("zuri@" + peer.host(t)).Code; code != http.StatusNotFound {
		t.Errorf("somebody not in the shared room joining its call: %d, want 404", code)
	}
	started := join("amina@" + peer.host(t))
	if started.Code != http.StatusOK {
		t.Fatalf("a member on the other server starting the room's call: %d %s", started.Code, started.Body)
	}
	state := decode(t, started)
	room := state["roomId"].(string)
	here := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/call", nil, founder, http.StatusOK))
	if here["id"] != state["id"] {
		t.Errorf("the room's call here is %v, the one started from elsewhere %v", here["id"], state["id"])
	}
	if !slices.Contains(timelineTypes(t, handler, founder, room), "call.started") {
		t.Error("a call started from another server is not in the room's history")
	}
}
