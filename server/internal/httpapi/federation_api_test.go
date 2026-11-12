package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

/*
Federation is off until an operator turns it on, and the two halves of that
behave differently on purpose.

A disabled server still publishes its identity — a peer reading
`"enabled": false` has learned something it can act on, where a refused
connection is indistinguishable from a server that is down. Every other
federated route refuses outright.
*/
func TestADisabledServerPublishesItsIdentityAndRefusesEverythingElse(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	_ = owner

	// The identity is served without federation being enabled, and without a
	// credential: this is the document a peer reads before it can authenticate.
	recorder := call(t, handler, http.MethodGet, "/.well-known/tdc-federation", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("a disabled server must still publish its identity, got %d", recorder.Code)
	}

	var document struct {
		CanonicalAddress string `json:"canonicalAddress"`
		Enabled          bool   `json:"enabled"`
		Versions         []string
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if document.Enabled {
		t.Error("federation must report disabled until an operator enables it")
	}
	if len(document.Versions) == 0 {
		t.Error("a disabled server still advertises what it would speak")
	}

	// Every other federated route refuses while federation is off.
	refused := call(t, handler, http.MethodPost, "/api/federation/version",
		map[string]any{"versions": []string{"tdc/1"}}, "")
	if refused.Code == http.StatusOK {
		t.Fatal("a federated route answered on a server with federation disabled")
	}
}

/*
Once enabled, the same route answers — which is what makes the previous test
mean something.

Without this, "refuses while disabled" would be satisfied by a route that is
broken in both states.
*/
func TestAnEnabledServerNegotiatesAVersion(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	set := call(t, handler, http.MethodPut, "/api/admin/settings/federation.enabled",
		map[string]any{"value": "true"}, owner)
	if set.Code != http.StatusNoContent {
		t.Fatalf("enabling federation returned %d", set.Code)
	}

	recorder := call(t, handler, http.MethodPost, "/api/federation/version",
		map[string]any{"versions": []string{"tdc/1"}}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("an enabled server must negotiate, got %d: %s", recorder.Code, recorder.Body)
	}

	var agreed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &agreed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if agreed.Version != "tdc/1" {
		t.Errorf("agreed version = %q, want tdc/1", agreed.Version)
	}

	// The document now says so too, so a peer can tell without trying.
	identity := call(t, handler, http.MethodGet, "/.well-known/tdc-federation", nil, "")
	var document struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(identity.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !document.Enabled {
		t.Error("an enabled server must publish that it federates")
	}
}

/*
A peer speaking nothing this server speaks is refused in words, not by a
failure somewhere deeper.

The requirement is not that two servers always agree — a new server and a very
old one genuinely cannot — but that the refusal names what each side speaks, so
an operator reading it knows why.
*/
func TestAPeerWithNoSharedVersionIsRefusedLegibly(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	set := call(t, handler, http.MethodPut, "/api/admin/settings/federation.enabled",
		map[string]any{"value": "true"}, owner)
	if set.Code != http.StatusNoContent {
		t.Fatalf("enabling federation returned %d", set.Code)
	}

	recorder := call(t, handler, http.MethodPost, "/api/federation/version",
		map[string]any{"versions": []string{"tdc/99"}}, "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	body := recorder.Body.String()
	for _, named := range []string{"tdc/99", "tdc/1"} {
		if !strings.Contains(body, named) {
			t.Errorf("the refusal must name %q so an operator can see the mismatch, got %s", named, body)
		}
	}
}

/*
The ping route is signed, and an unsigned call never reaches the handler.

Both halves matter. That it refuses proves the middleware runs; that the body
says nothing about pings proves the refusal happened before the handler rather
than inside it.
*/
func TestTheSignedPingRefusesAnUnsignedCaller(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	set := call(t, handler, http.MethodPut, "/api/admin/settings/federation.enabled",
		map[string]any{"value": "true"}, owner)
	if set.Code != http.StatusNoContent {
		t.Fatalf("enabling federation returned %d", set.Code)
	}

	recorder := call(t, handler, http.MethodPost, "/api/federation/ping",
		map[string]any{"ping": true}, "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("an unsigned ping returned %d, want 401", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "pong") {
		t.Error("the handler ran for an unsigned request")
	}
}

// And with federation off, the signed route is refused by the outer gate
// before signature verification is even attempted.
func TestTheSignedPingIsGatedByFederationBeingOff(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/federation/ping",
		map[string]any{"ping": true}, "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("with federation off the ping returned %d, want 404", recorder.Code)
	}
}

// enableFederation turns federation on and returns the owner's credential.
func enableFederation(t *testing.T, handler http.Handler) string {
	t.Helper()
	owner := completeSetup(t, handler)

	set := call(t, handler, http.MethodPut, "/api/admin/settings/federation.enabled",
		map[string]any{"value": "true"}, owner)
	if set.Code != http.StatusNoContent {
		t.Fatalf("enabling federation returned %d", set.Code)
	}
	return owner
}

/*
A blocked peer is refused on the way out, before anything is dialed.

The ping reports the refusal rather than a connection failure, which is what
distinguishes "this server will not talk to them" from "they were unreachable"
— two situations an operator must be able to tell apart.
*/
func TestABlockedPeerIsNotEvenDialed(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://this.example.org")
	owner := enableFederation(t, handler)

	setSetting(t, handler, owner, "federation.blocked_servers", "nuisance.example.org")

	recorder := call(t, handler, http.MethodPost, "/api/admin/federation/ping",
		map[string]any{"peer": "https://nuisance.example.org"}, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("the ping endpoint returned %d", recorder.Code)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `"reached":false`) {
		t.Fatalf("a blocked peer was reported as reached: %s", body)
	}
	if !strings.Contains(body, "blocked") {
		t.Errorf("the refusal should say the peer is blocked, got %s", body)
	}
	// A dial would have produced a connection error instead; this refusal
	// happened before the address was ever looked up.
	if strings.Contains(body, "dial") || strings.Contains(body, "no such host") {
		t.Errorf("the peer was dialed despite being blocked: %s", body)
	}
}

/*
Under an allowlist, an unlisted peer is refused and a listed one is not.

Both halves: a test that only checked the refusal would pass against a server
that refused everybody.
*/
func TestAnAllowlistRefusesTheUnlistedAndNotTheListed(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://this.example.org")
	owner := enableFederation(t, handler)

	setSetting(t, handler, owner, "federation.mode", "allowlist")
	setSetting(t, handler, owner, "federation.allowed_servers", "friend.example.org")

	unlisted := call(t, handler, http.MethodPost, "/api/admin/federation/ping",
		map[string]any{"peer": "https://stranger.example.org"}, owner)
	if !strings.Contains(unlisted.Body.String(), "allowlist") {
		t.Errorf("an unlisted peer should be refused by the allowlist, got %s", unlisted.Body)
	}

	// The listed peer does not exist, so it cannot be reached — but it must
	// fail by being unreachable rather than by being refused.
	listedPeer := call(t, handler, http.MethodPost, "/api/admin/federation/ping",
		map[string]any{"peer": "https://friend.example.org"}, owner)
	if strings.Contains(listedPeer.Body.String(), "allowlist") {
		t.Errorf("a listed peer was refused by the allowlist: %s", listedPeer.Body)
	}
}

/*
An operator's change takes effect on the next request, with no restart.

The requirement says "immediately", and the way that could quietly fail is a
value read once at startup — so this blocks a peer and pings it in the same
test, against a server that has been running throughout.
*/
func TestBlockingTakesEffectWithoutARestart(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://this.example.org")
	owner := enableFederation(t, handler)

	before := call(t, handler, http.MethodPost, "/api/admin/federation/ping",
		map[string]any{"peer": "https://peer.example.org"}, owner)
	if strings.Contains(before.Body.String(), "blocked") {
		t.Fatalf("the peer was blocked before anybody blocked it: %s", before.Body)
	}

	setSetting(t, handler, owner, "federation.blocked_servers", "peer.example.org")

	after := call(t, handler, http.MethodPost, "/api/admin/federation/ping",
		map[string]any{"peer": "https://peer.example.org"}, owner)
	if !strings.Contains(after.Body.String(), "blocked") {
		t.Fatalf("the block did not take effect on the same running server: %s", after.Body)
	}
}

/*
Each kind of federation change is its own audit entry.

Not a generic "setting changed" among hundreds: somebody asking later when a
relationship started, and who decided it, should find an entry that says so.
*/
func TestFederationChangesAreEachAudited(t *testing.T) {
	handler := newTestAPI(t)
	owner := enableFederation(t, handler)

	setSetting(t, handler, owner, "federation.mode", "allowlist")
	setSetting(t, handler, owner, "federation.allowed_servers", "friend.example.org")
	setSetting(t, handler, owner, "federation.blocked_servers", "nuisance.example.org")

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=50", nil, owner))

	found := map[string]bool{}
	for _, entry := range entries {
		found[entry.Action] = true
	}

	for _, wanted := range []string{
		"server.federation.enablement_changed",
		"server.federation.mode_changed",
		"server.federation.peers_changed",
	} {
		if !found[wanted] {
			t.Errorf("no audit entry for %s; saw %v", wanted, found)
		}
	}

	// And the entry records what the value became, so the log can be read
	// without going to look at the current settings.
	var enablement auditEntry
	for _, entry := range entries {
		if entry.Action == "server.federation.enablement_changed" {
			enablement = entry
		}
	}
	if !strings.Contains(enablement.Detail, "true") {
		t.Errorf("the enablement entry should record the new value, got %q", enablement.Detail)
	}
	if enablement.TargetID != "federation.enabled" {
		t.Errorf("the entry names %q, want federation.enabled", enablement.TargetID)
	}
}
