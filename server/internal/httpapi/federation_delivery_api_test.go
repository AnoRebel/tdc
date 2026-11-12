package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/events"
)

// receivingPeer is another server that accepts every delivery and remembers
// what it was sent.
type receivingPeer struct {
	http *httptest.Server
	mu   sync.Mutex
	got  map[string][]events.Event // room address → events, in arrival order
}

func newReceivingPeer(t *testing.T) *receivingPeer {
	t.Helper()
	peer := &receivingPeer{got: map[string][]events.Event{}}
	peer.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var delivery struct {
			Events []events.Event `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&delivery); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		peer.mu.Lock()
		for _, event := range delivery.Events {
			peer.got[event.Room] = append(peer.got[event.Room], event)
		}
		peer.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]int{"stored": len(delivery.Events)})
	}))
	t.Cleanup(peer.http.Close)
	return peer
}

func (p *receivingPeer) received(room string) []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]events.Event(nil), p.got[room]...)
}

/*
A federated room's events reach its peers without anybody pushing them: once,
in order, and only while the room is federated. A replica's peer — the server
it receives from — is never sent that server's own events back.
*/
func TestAFederatedRoomDeliversByItself(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://tdc.test")
	owner := enableFederation(t, handler)
	setSetting(t, handler, owner, "federation.allow_private_addresses", "true")
	peer := newReceivingPeer(t)
	server := serverBehind(t, handler)

	room := makeRoom(t, handler, owner, "shared", "public")
	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+room+"/federation",
		map[string]any{"federated": true, "peers": []string{peer.http.URL}}, owner, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "Habari"}, owner, http.StatusCreated)

	server.DeliverDueForTest(t.Context())
	delivered := peer.received("shared@tdc.test")
	if len(delivered) == 0 {
		t.Fatal("nothing was delivered without being asked")
	}
	for i := 1; i < len(delivered); i++ {
		if delivered[i].Sequence <= delivered[i-1].Sequence {
			t.Fatalf("delivered out of order: %d after %d", delivered[i].Sequence, delivered[i-1].Sequence)
		}
	}
	last := delivered[len(delivered)-1]
	if last.Type != events.TypeMessageSent {
		t.Errorf("the last event delivered is %s, not the message", last.Type)
	}

	server.DeliverDueForTest(t.Context())
	if again := peer.received("shared@tdc.test"); len(again) != len(delivered) {
		t.Errorf("a second pass sent %d more events, though nothing new was written", len(again)-len(delivered))
	}

	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+room+"/federation",
		map[string]any{"federated": false}, owner, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "Kwa heri"}, owner, http.StatusCreated)
	server.DeliverDueForTest(t.Context())
	if closed := peer.received("shared@tdc.test"); len(closed) != len(delivered) {
		t.Error("a room closed to federation was still delivered")
	}

	// The same room as though another server were its home: this server
	// holds its events as a replica, and its peer is where they came from.
	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+room+"/federation",
		map[string]any{"federated": true}, owner, http.StatusOK)
	if _, err := poolForTest.Exec(t.Context(), "UPDATE rooms SET home_server = 'b.example.org' WHERE id = $1", room); err != nil {
		t.Fatal(err)
	}
	if _, err := poolForTest.Exec(t.Context(), "UPDATE room_peers SET cursor = 0 WHERE room_id = $1", room); err != nil {
		t.Fatal(err)
	}
	server.DeliverDueForTest(t.Context())
	if replica := peer.received("shared@tdc.test"); len(replica) != len(delivered) {
		t.Error("a replica pushed its home server's events back to it")
	}
}
