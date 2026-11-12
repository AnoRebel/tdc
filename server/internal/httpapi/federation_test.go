package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// federationServer is a server that can sign, so the discovery document it
// serves has real keys in it rather than an empty list that would make every
// assertion about keys vacuous.
func federationServer(t *testing.T, address string) *Server {
	t.Helper()

	signing, err := events.NewKeyPair("test-key-1")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return New(Config{
		Address:          "127.0.0.1:0",
		ReadTimeout:      5 * time.Second,
		WriteTimeout:     5 * time.Second,
		IdleTimeout:      10 * time.Second,
		ShutdownTimeout:  2 * time.Second,
		ServerName:       "Test Server",
		CanonicalAddress: address,
		DefaultLanguage:  "en",
	}, nil, Services{
		Rooms: rooms.NewService(nil, events.NewKeyring(signing), address, logger),
	}, logger)
}

func fetchWellKnown(t *testing.T, server *Server) (*httptest.ResponseRecorder, federation.Document) {
	t.Helper()
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, federation.WellKnownPath, nil))

	var doc federation.Document
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("decode discovery document: %v", err)
		}
	}
	return rec, doc
}

/*
The document a peer reads describes this server truthfully.

Every field is checked against the running server rather than against a
constant: a document that published a hard-coded address or an empty key list
would still be valid JSON, and a test that only checked it parsed would pass.
*/
func TestWellKnownDescribesThisServer(t *testing.T) {
	server := federationServer(t, "https://tdc.example.org")

	rec, doc := fetchWellKnown(t, server)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if doc.CanonicalAddress != "https://tdc.example.org" {
		t.Errorf("address = %q, want the operator's canonical address", doc.CanonicalAddress)
	}

	// The keys must be this server's actual keyring, not a placeholder.
	published := server.rooms.Keyring().Published()
	if len(doc.Keys) != len(published) || len(doc.Keys) == 0 {
		t.Fatalf("document publishes %d keys, the keyring holds %d", len(doc.Keys), len(published))
	}
	if doc.Keys[0].ID != published[0].ID {
		t.Errorf("published key = %q, want the keyring's own %q", doc.Keys[0].ID, published[0].ID)
	}
	if doc.Keys[0].PublicKey == "" {
		t.Error("a published key with no public half verifies nothing")
	}

	if err := doc.Valid(); err != nil {
		t.Errorf("this server publishes a document it would itself reject: %v", err)
	}
}

/*
A retired key stays published.

A peer that dropped one would lose the ability to verify everything that key
ever signed — a rotation making history unverifiable, which is the failure the
event log's Published() was written to avoid. Serving it is what carries that
guarantee across the network.
*/
func TestWellKnownPublishesRetiredKeys(t *testing.T) {
	server := federationServer(t, "https://tdc.example.org")

	// Rotate, so the original key becomes retired and a second one signs.
	replacement, err := events.NewKeyPair("test-key-2")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	server.rooms.Keyring().Rotate(replacement)

	_, doc := fetchWellKnown(t, server)

	var retired, current int
	for _, key := range doc.Keys {
		if key.Retired {
			retired++
		} else {
			current++
		}
	}
	if retired == 0 {
		t.Error("a rotated-away key must still be published, or its history stops verifying")
	}
	if current == 0 {
		t.Error("the current signing key must be published")
	}
}

/*
The discovery document answers whether or not federation is enabled.

A server with federation off still has an identity, and saying "not
participating" is an answer a peer's operator can read. A connection refused
is not: it looks identical to a server that is down.
*/
func TestWellKnownAnswersWithFederationDisabled(t *testing.T) {
	server := federationServer(t, "https://tdc.example.org")

	rec, doc := fetchWellKnown(t, server)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the document to be served regardless", rec.Code)
	}
	// No settings service configured means federation is off, which is also
	// the default an operator gets.
	if doc.Enabled {
		t.Error("federation must report disabled until an operator enables it")
	}
	if doc.CanonicalAddress == "" || len(doc.Keys) == 0 {
		t.Error("a disabled server still has an identity and must publish it")
	}
}

// The document a peer fetches from this server must pass the very check that
// peer will apply to it — otherwise every correctly configured server would be
// refused as an impostor.
func TestThisServersDocumentPassesTheDomainCheck(t *testing.T) {
	server := federationServer(t, "https://tdc.example.org")
	_, doc := fetchWellKnown(t, server)

	if err := doc.DomainMatches("tdc.example.org"); err != nil {
		t.Fatalf("a peer fetching this document from the right domain must accept it: %v", err)
	}
}
