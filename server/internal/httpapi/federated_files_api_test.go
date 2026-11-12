package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

const thisServer = "https://this.example.org"

/*
fakePeer is another server, reduced to what federated files need from one: a
published key, signed requests, and the bytes of a file when asked.
*/
type fakePeer struct {
	http    *httptest.Server
	keyring *events.Keyring
	// address is what the peer calls itself. Two peers on one machine are
	// one server by identity, since ports are ignored, so a second peer
	// names itself differently.
	address string

	mu    sync.Mutex
	files map[string][]byte // digest → bytes served for it
	asked int
	// stream, when set, is a room's events served to a catch-up, a page of
	// at most 200 from the sequence asked for.
	stream []events.Event
}

func newFakePeer(t *testing.T, id string, named ...string) *fakePeer {
	t.Helper()
	key, err := events.NewKeyPair(id)
	if err != nil {
		t.Fatal(err)
	}
	peer := &fakePeer{keyring: events.NewKeyring(key), files: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc(federation.WellKnownPath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(federation.Document{
			CanonicalAddress: peer.address,
			Keys:             peer.keyring.Published(),
			Versions:         federation.SupportedVersions(),
			Enabled:          true,
		})
	})
	mux.HandleFunc("/api/federation/rooms/", func(w http.ResponseWriter, r *http.Request) {
		digest := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		peer.mu.Lock()
		if digest == "events" && peer.stream != nil {
			since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
			page := []events.Event{}
			for _, event := range peer.stream {
				if event.Sequence > since && len(page) < 200 {
					page = append(page, event)
				}
			}
			peer.asked++
			peer.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"events": page, "more": len(page) == 200})
			return
		}
		data, ok := peer.files[digest]
		peer.asked++
		peer.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	peer.http = httptest.NewServer(mux)
	t.Cleanup(peer.http.Close)
	peer.address = peer.http.URL
	if len(named) > 0 {
		peer.address = strings.Replace(peer.http.URL, "127.0.0.1", named[0], 1)
	}
	return peer
}

func (p *fakePeer) host(t *testing.T) string {
	t.Helper()
	host, err := federation.Host(p.address)
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func (p *fakePeer) serve(digest string, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files[digest] = data
}

// signed sends a request to this server, signed as the peer.
func (p *fakePeer) signed(t *testing.T, handler http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	request := httptest.NewRequest(method, thisServer+path, bytes.NewReader(body))
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("TDC-Origin-Server", p.address)
	if err := federation.Sign(request, body, p.keyring, time.Now()); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sharedRoom makes a federated room the peer takes part in, with a member of
// the peer's in it, and returns the room and the founder's token.
func sharedRoom(t *testing.T, handler http.Handler, peers ...*fakePeer) (string, string) {
	t.Helper()
	founder := enableFederation(t, handler)
	setSetting(t, handler, founder, "federation.allow_private_addresses", "true")
	room := makeRoom(t, handler, founder, "shared", "public")
	mustCall(t, handler, http.MethodPut, "/api/admin/rooms/"+room+"/federation",
		map[string]any{"federated": true}, founder, http.StatusOK)
	for _, peer := range peers {
		joined := peer.signed(t, handler, http.MethodPost, "/api/federation/rooms/shared/join",
			map[string]any{"member": "amina@" + peer.host(t)})
		if joined.Code != http.StatusOK {
			t.Fatalf("the peer's member could not join: %d %s", joined.Code, joined.Body)
		}
	}
	return room, founder
}

/*
A peer taking part in a room gets the bytes of a file a message there carries —
and nothing else: not a file that was never sent, and nothing at all for a peer
with no part in the room. Both refusals look like absence, so asking for
digests in turn learns nothing.
*/
func TestAPeerFetchesOnlyFilesSentInItsRooms(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	member := newFakePeer(t, "member-key")
	stranger := newFakePeer(t, "stranger-key", "localhost")
	room, founder := sharedRoom(t, handler, member)

	data := fileOf(t, 5000)
	sentID := uploadedIDOf(t, handler, founder, room, "sent.png", data)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"attachmentIds": []string{sentID}}, founder, http.StatusCreated)
	unsent := fileOf(t, 4000)
	uploadedIDOf(t, handler, founder, room, "unsent.png", unsent)

	got := member.signed(t, handler, http.MethodGet, "/api/federation/rooms/shared/files/"+digestOf(data), nil)
	if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), data) {
		t.Fatalf("the peer in the room got %d and %d bytes, want the file", got.Code, got.Body.Len())
	}
	if got := member.signed(t, handler, http.MethodGet, "/api/federation/rooms/shared/files/"+digestOf(unsent), nil); got.Code != http.StatusNotFound {
		t.Errorf("a file never sent was served to a peer: %d", got.Code)
	}
	if got := stranger.signed(t, handler, http.MethodGet, "/api/federation/rooms/shared/files/"+digestOf(data), nil); got.Code != http.StatusNotFound {
		t.Errorf("a peer with no part in the room got %d, want 404", got.Code)
	}
}

/*
A file a member elsewhere sent reaches people here: the message describes it,
the bytes are copied from that server the first time somebody opens it, and
what is served is exactly what was signed — altered bytes are refused, and a
file over this server's copy limit says so.
*/
func TestAFileSentElsewhereIsCopiedOnFirstView(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	peer := newFakePeer(t, "peer-key")
	room, founder := sharedRoom(t, handler, peer)

	// What the peer's member sends.
	data := fileOf(t, 9000)
	post := func(name string, content []byte) string {
		t.Helper()
		nonce, _ := events.NewNonce()
		event := events.Event{
			Room:      "shared@tdc.test",
			Sender:    "amina@" + peer.host(t),
			Origin:    peer.host(t),
			Type:      events.TypeMessageSent,
			CreatedAt: events.Now(),
			Nonce:     nonce,
		}
		event.Content, _ = json.Marshal(map[string]any{"body": "", "attachments": []map[string]any{{
			"id": "0b6f6f3e-6a4e-4a39-9d87-58c1c0a2a5a1", "filename": name, "contentType": "image/png",
			"size": len(content), "sha256": digestOf(content),
		}}})
		if err := peer.keyring.Sign(&event); err != nil {
			t.Fatal(err)
		}
		posted := peer.signed(t, handler, http.MethodPost, "/api/federation/rooms/shared/post",
			map[string]any{"event": event})
		if posted.Code != http.StatusCreated {
			t.Fatalf("posting from the peer returned %d: %s", posted.Code, posted.Body)
		}
		var timeline []struct {
			Attachments []struct {
				URL    string `json:"url"`
				Origin string `json:"origin"`
				Held   bool   `json:"held"`
			} `json:"attachments"`
		}
		_ = json.Unmarshal(mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/timeline", nil, founder, http.StatusOK).Body.Bytes(), &timeline)
		last := timeline[len(timeline)-1]
		if len(last.Attachments) != 1 || last.Attachments[0].Held || last.Attachments[0].Origin == "" {
			t.Fatalf("the message from elsewhere shows %+v, want one file not yet copied", last.Attachments)
		}
		return last.Attachments[0].URL
	}

	t.Run("altered bytes are refused", func(t *testing.T) {
		url := post("altered.png", data)
		tampered := bytes.Clone(data)
		tampered[len(tampered)-1] ^= 0xff
		peer.serve(digestOf(data), tampered)
		if got := call(t, handler, http.MethodGet, url, nil, founder); got.Code != http.StatusBadGateway {
			t.Fatalf("altered bytes returned %d, want 502", got.Code)
		}
	})

	t.Run("the real bytes are copied, once", func(t *testing.T) {
		peer.serve(digestOf(data), data)
		url := post("photo.png", data)
		peer.mu.Lock()
		peer.asked = 0
		peer.mu.Unlock()
		for range 2 {
			got := mustCall(t, handler, http.MethodGet, url, nil, founder, http.StatusOK)
			if !bytes.Equal(got.Body.Bytes(), data) {
				t.Fatal("the file served is not the file sent")
			}
		}
		if peer.asked != 1 {
			t.Errorf("the peer was asked %d times, want once", peer.asked)
		}
	})

	t.Run("a file over the copy limit says so", func(t *testing.T) {
		setSetting(t, handler, founder, "files.federation_copy_max_bytes", "1000")
		big := fileOf(t, 3000)
		peer.serve(digestOf(big), big)
		url := post("big.png", big)
		if got := call(t, handler, http.MethodGet, url, nil, founder); got.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("a file over the copy limit returned %d, want 413", got.Code)
		}
	})
}

// uploadedIDOf uploads given bytes and returns the attachment's identifier.
func uploadedIDOf(t *testing.T, handler http.Handler, token, roomID, name string, data []byte) string {
	t.Helper()
	recorder := uploadFile(t, handler, token, roomID, name, data)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, recorder.Body)
	}
	id, _ := decode(t, recorder)["id"].(string)
	return id
}
