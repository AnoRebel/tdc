package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/events"
)

func fetchDeliveryKeys(t *testing.T, server *Server) (*httptest.ResponseRecorder, deliveryKeys) {
	t.Helper()
	recorder := httptest.NewRecorder()
	// No credential of any kind: a receiver is somebody else's server.
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, DeliveryKeysPath, nil))

	var document deliveryKeys
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
			t.Fatalf("decoding the published keys: %v", err)
		}
	}
	return recorder, document
}

/*
The keys are published to anybody, the current one and every retired one.

Rotated first, so the retired key is really there to be found: a test against a
keyring that had never rotated would pass whether retired keys were published
or dropped.
*/
func TestDeliveryKeysArePublishedWithoutACredential(t *testing.T) {
	server := federationServer(t, "https://tdc.example.org")

	next, err := events.NewKeyPair("test-key-2")
	if err != nil {
		t.Fatal(err)
	}
	server.rooms.Keyring().Rotate(next)

	recorder, document := fetchDeliveryKeys(t, server)
	if recorder.Code != http.StatusOK {
		t.Fatalf("fetching the keys without a credential returned %d", recorder.Code)
	}

	retired := map[string]bool{}
	for _, key := range document.Keys {
		retired[key.ID] = key.Retired
	}
	if current, found := retired["test-key-2"]; !found || current {
		t.Errorf("the current key is missing or marked retired: %v", document.Keys)
	}
	if old, found := retired["test-key-1"]; !found || !old {
		t.Errorf("the retired key is missing or not marked retired: %v", document.Keys)
	}
	if document.Algorithm != "ed25519" || document.ReplayWindowSeconds != 300 {
		t.Errorf("the document states %q and %ds", document.Algorithm, document.ReplayWindowSeconds)
	}
}

/*
A delivery this server actually sent verifies from the keys it publishes and
the delivery's own content — and nothing else.

No database anywhere: the server is built without one, and the receiver holds
only what it fetched from the public path and what arrived in the request. The
same independence the federation-readiness test proves for events, proved for
deliveries.
*/
func TestASentDeliveryVerifiesFromThePublishedKeysAlone(t *testing.T) {
	server := federationServer(t, "https://tdc.example.org")

	// The receiver here is on loopback, which deliveries refuse unless an
	// operator allows it; this is that operator.
	server.integrations.once.Do(func() {
		server.integrations.signer = delivery.NewSigner(server.rooms.Keyring())
		server.integrations.client = delivery.NewClient(5*time.Second, func() bool { return true })
	})

	type received struct {
		headers http.Header
		body    []byte
	}
	arrived := make(chan received, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		arrived <- received{headers: r.Header.Clone(), body: body}
	}))
	defer receiver.Close()

	body := []byte(`{"version":1,"type":"delivery.test"}`)
	outcome, err := server.sendSigned(context.Background(), receiver.URL, "delivery-1", "delivery.test",
		body, []byte("shared"))
	if err != nil || !outcome.Delivered {
		t.Fatalf("sending: %v, %+v", err, outcome)
	}
	delivered := <-arrived

	// Everything the receiver knows about this server, fetched as it would.
	_, document := fetchDeliveryKeys(t, server)
	keys, err := events.KeyringFromPublished(document.Keys)
	if err != nil {
		t.Fatalf("a keyring from the published keys: %v", err)
	}

	if err := delivery.Verify(delivered.headers, delivered.body, keys, time.Now()); err != nil {
		t.Fatalf("a real delivery did not verify from the published keys: %v", err)
	}
	if err := delivery.VerifyHMAC(delivered.headers, delivered.body, []byte("shared"), time.Now()); err != nil {
		t.Errorf("a real delivery's HMAC did not verify: %v", err)
	}
	if delivered.headers.Get(delivery.HeaderDelivery) != "delivery-1" ||
		delivered.headers.Get(delivery.HeaderEvent) != "delivery.test" {
		t.Errorf("the delivery arrived with headers %v", delivered.headers)
	}
}
