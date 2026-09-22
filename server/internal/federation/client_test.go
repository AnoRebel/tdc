package federation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
)

// peer stands up a server publishing a discovery document, counting fetches so
// a test can tell a cache hit from a network call.
type peer struct {
	server  *httptest.Server
	fetches atomic.Int64
	keyID   atomic.Value // string
}

func newPeer(t *testing.T) *peer {
	t.Helper()
	p := &peer{}
	p.keyID.Store("key-1")

	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != WellKnownPath {
			http.NotFound(w, r)
			return
		}
		p.fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Document{
			// Claims the address it is actually served from, so the domain
			// check passes and the test is about caching rather than identity.
			CanonicalAddress: p.server.URL,
			Keys:             []events.PublishedKey{{ID: p.keyID.Load().(string), PublicKey: "AAAA"}},
			Versions:         []string{Version1},
			Enabled:          true,
		})
	}))
	t.Cleanup(p.server.Close)
	return p
}

// A client reaching a test server on loopback has to be allowed to, which is
// exactly the private-address case an operator opts into.
func loopbackClient() *Client { return NewClient(true) }

/*
A cached document is reused, so verifying a signature is not a network call.

The count is what proves it: an assertion that the second call returned a
document would pass whether or not it went back to the peer.
*/
func TestFreshDocumentIsReusedWithoutRefetching(t *testing.T) {
	p := newPeer(t)
	client := loopbackClient()

	for range 3 {
		if _, err := client.Discover(context.Background(), p.server.URL); err != nil {
			t.Fatalf("Discover: %v", err)
		}
	}

	if got := p.fetches.Load(); got != 1 {
		t.Fatalf("peer was fetched %d times, want 1 — the cache is not being used", got)
	}
}

/*
A rotation becomes visible without waiting out the TTL.

This is the case that makes the cache safe to have: a peer that rotates a key
must become verifiable again as soon as we notice, not an hour later.
*/
func TestRefreshPicksUpARotatedKey(t *testing.T) {
	p := newPeer(t)
	client := loopbackClient()

	first, err := client.Discover(context.Background(), p.server.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if first.Keys[0].ID != "key-1" {
		t.Fatalf("first key = %q, want key-1", first.Keys[0].ID)
	}

	p.keyID.Store("key-2")

	// Still cached: the peer rotated but nothing has told us to look.
	cached, err := client.Discover(context.Background(), p.server.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if cached.Keys[0].ID != "key-1" {
		t.Fatalf("within the TTL the cached document must still be returned, got %q", cached.Keys[0].ID)
	}

	// Refresh is what a caller does on meeting an unrecognised key ID.
	refreshed, err := client.Refresh(context.Background(), p.server.URL)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.Keys[0].ID != "key-2" {
		t.Fatalf("Refresh must return the rotated key, got %q", refreshed.Keys[0].ID)
	}
}

// An expired entry is refetched rather than served stale forever.
func TestExpiredDocumentIsRefetched(t *testing.T) {
	p := newPeer(t)
	client := loopbackClient()

	if _, err := client.Discover(context.Background(), p.server.URL); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// Move the clock past the TTL rather than sleeping for an hour.
	client.now = func() time.Time { return time.Now().Add(2 * DefaultTTL) }

	if _, err := client.Discover(context.Background(), p.server.URL); err != nil {
		t.Fatalf("Discover after expiry: %v", err)
	}
	if got := p.fetches.Load(); got != 2 {
		t.Fatalf("peer was fetched %d times, want 2 — an expired entry was served stale", got)
	}
}

/*
A peer claiming somebody else's identity is refused, and not cached.

The second assertion matters as much as the first: caching a document that
failed verification would mean the refusal happened once and the lie was
believed thereafter.
*/
func TestPeerClaimingAnotherDomainIsRefusedAndNotCached(t *testing.T) {
	impostor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Document{
			CanonicalAddress: "https://someone-else.example.org",
			Keys:             []events.PublishedKey{{ID: "k", PublicKey: "AAAA"}},
			Versions:         []string{Version1},
		})
	}))
	defer impostor.Close()

	client := loopbackClient()

	_, err := client.Discover(context.Background(), impostor.URL)
	if !errors.Is(err, ErrDomainMismatch) {
		t.Fatalf("Discover = %v, want ErrDomainMismatch", err)
	}

	client.mu.RLock()
	cachedCount := len(client.documents)
	client.mu.RUnlock()
	if cachedCount != 0 {
		t.Fatal("a document that failed the domain check must not be cached")
	}
}

// A peer that answers with an error is a failure to report, not an empty
// document to proceed with.
func TestUnreachablePeerIsAnError(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer down.Close()

	if _, err := loopbackClient().Discover(context.Background(), down.URL); !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("Discover = %v, want ErrFetchFailed", err)
	}
}
