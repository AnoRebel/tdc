package federation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

/*
A whole server, reduced to the two things federation needs from one: a
discovery document and a signed endpoint.

Built here rather than using the real httpapi server because this is the
transport's own test — what is being proved is that a signature made by one
process is verified by another over a real network connection, with no shared
memory between them. A test that signed and verified in one place would prove
the arithmetic and not the transport.
*/
type peerServer struct {
	http    *httptest.Server
	keyring *events.Keyring
	seen    *federation.ReplayCache
	// refusals records why requests were turned away, standing in for the log
	// an operator would read.
	refusals []string
}

func newPeerServer(t *testing.T, id string) *peerServer {
	t.Helper()

	key, err := events.NewKeyPair(id)
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}

	peer := &peerServer{keyring: events.NewKeyring(key), seen: federation.NewReplayCache()}
	mux := http.NewServeMux()

	mux.HandleFunc(federation.WellKnownPath, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(federation.Document{
			CanonicalAddress: peer.http.URL,
			Keys:             peer.keyring.Published(),
			Versions:         federation.SupportedVersions(),
			Enabled:          true,
		})
	})

	mux.HandleFunc("/api/federation/ping", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		origin := r.Header.Get("TDC-Origin-Server")
		document, err := federation.NewClient(true).Discover(r.Context(), origin)
		if err != nil {
			peer.refuse(w, "could not verify the origin server", err)
			return
		}
		keyring, err := document.Keyring()
		if err != nil {
			peer.refuse(w, "the origin publishes no usable keys", err)
			return
		}

		// The destination this server knows itself to be, from its own
		// configuration — never from the request.
		authority, _ := federation.Authority(peer.http.URL)

		signature, err := federation.Verify(r, body, keyring, authority, time.Now())
		if err != nil {
			peer.refuse(w, "signature was not accepted", err)
			return
		}
		if !peer.seen.Remember(signature.Raw, signature.Expires) {
			peer.refuse(w, "request has already been delivered", errors.New("replayed signature"))
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"pong": true, "origin": document.CanonicalAddress})
	})

	peer.http = httptest.NewServer(mux)
	t.Cleanup(peer.http.Close)
	return peer
}

func (p *peerServer) refuse(w http.ResponseWriter, reason string, err error) {
	p.refusals = append(p.refusals, reason+": "+err.Error())
	http.Error(w, reason, http.StatusUnauthorized)
}

// ping signs a request as this server and sends it to another.
func (p *peerServer) ping(t *testing.T, target *peerServer, at time.Time) *http.Response {
	t.Helper()

	body := []byte(`{"ping":true}`)
	request, err := http.NewRequest(http.MethodPost, target.http.URL+"/api/federation/ping", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("TDC-Origin-Server", p.http.URL)

	if err := federation.Sign(request, body, p.keyring, at); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send the ping: %v", err)
	}
	return response
}

/*
Two servers exchange a signed ping over a real connection.

Neither holds the other's private key; B verifies A purely from what A
published at its well-known path.
*/
func TestASignedPingCrossesBetweenTwoServers(t *testing.T) {
	a := newPeerServer(t, "a-key")
	b := newPeerServer(t, "b-key")

	response := a.ping(t, b, time.Now())
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 200: %s (refusals: %v)", response.StatusCode, body, b.refusals)
	}

	var answer struct {
		Pong   bool   `json:"pong"`
		Origin string `json:"origin"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !answer.Pong {
		t.Error("the ping was not answered")
	}
	if answer.Origin != a.http.URL {
		t.Errorf("origin = %q, want the sending server %q", answer.Origin, a.http.URL)
	}
}

/*
A captured ping replayed after its window is refused, with a stated reason.

Signed at an instant far enough in the past that the window has lapsed by the
time it is delivered — which is how a captured request behaves in practice.
*/
func TestAPingReplayedAfterItsWindowIsRefused(t *testing.T) {
	a := newPeerServer(t, "a-key")
	b := newPeerServer(t, "b-key")

	stale := time.Now().Add(-2 * federation.SignatureLifetime)
	response := a.ping(t, b, stale)
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.StatusCode)
	}
	if len(b.refusals) == 0 {
		t.Fatal("the refusal was not recorded anywhere an operator could read it")
	}
}

/*
A ping signed for one server and delivered to another is refused.

The request is signed naming B as its destination, then sent to C. Every byte
is valid; only the destination is wrong, and the signature covers it.
*/
func TestAPingSignedForOneServerIsRefusedByAnother(t *testing.T) {
	a := newPeerServer(t, "a-key")
	b := newPeerServer(t, "b-key")
	c := newPeerServer(t, "c-key")

	body := []byte(`{"ping":true}`)
	// Signed for B...
	request, err := http.NewRequest(http.MethodPost, b.http.URL+"/api/federation/ping", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("TDC-Origin-Server", a.http.URL)
	if err := federation.Sign(request, body, a.keyring, time.Now()); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// ...and delivered to C, carrying the signature that names B.
	redirected, err := http.NewRequest(http.MethodPost, c.http.URL+"/api/federation/ping", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for name, values := range request.Header {
		for _, value := range values {
			redirected.Header.Add(name, value)
		}
	}

	response, err := http.DefaultClient.Do(redirected)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — C accepted a request signed for B", response.StatusCode)
	}
	if len(c.refusals) == 0 {
		t.Fatal("C refused without recording why")
	}
}

/*
The same valid ping, sent twice inside its window, is refused the second time.

Nothing about the second delivery is detectably different from the first except
that it has happened before — which is exactly what the replay cache is for.
*/
func TestTheSamePingTwiceIsRefusedTheSecondTime(t *testing.T) {
	a := newPeerServer(t, "a-key")
	b := newPeerServer(t, "b-key")

	body := []byte(`{"ping":true}`)
	request, err := http.NewRequest(http.MethodPost, b.http.URL+"/api/federation/ping", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("TDC-Origin-Server", a.http.URL)
	if err := federation.Sign(request, body, a.keyring, time.Now()); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	send := func() int {
		replay, err := http.NewRequest(http.MethodPost, b.http.URL+"/api/federation/ping", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		for name, values := range request.Header {
			for _, value := range values {
				replay.Header.Add(name, value)
			}
		}
		response, err := http.DefaultClient.Do(replay)
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}

	if first := send(); first != http.StatusOK {
		t.Fatalf("the first delivery returned %d, want 200", first)
	}
	if second := send(); second != http.StatusUnauthorized {
		t.Fatalf("the second delivery returned %d, want 401 — the replay was accepted", second)
	}
}

// An unsigned ping is refused before any handler work happens.
func TestAnUnsignedPingIsRefused(t *testing.T) {
	a := newPeerServer(t, "a-key")
	b := newPeerServer(t, "b-key")

	request, err := http.NewRequest(http.MethodPost, b.http.URL+"/api/federation/ping",
		bytes.NewReader([]byte(`{"ping":true}`)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("TDC-Origin-Server", a.http.URL)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.StatusCode)
	}
}
