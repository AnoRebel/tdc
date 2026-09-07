package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

/*
TestGifSearchIsUnavailableByDefault is the constraint this endpoint answers to.

A server must work with no account anywhere. With nothing configured — which is
every default — searching has to be unavailable rather than failing some other
way, so that a client can tell the difference between "not offered here" and
"broken".

404 rather than the 503 this expected before feature toggles existed. The
feature defaults off, so on a fresh server this endpoint is not offered at all,
and that is what /api/server says too: the refusal and the advertisement agree.
503 would mean the capability exists here and is temporarily unwell, which
would be a different and untrue thing to tell a client.
*/
func TestGifSearchIsUnavailableByDefault(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/gifs?q=cat", nil, token)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("searching gifs returned %d, expected 404 with nothing configured",
			recorder.Code)
	}

	// And the server does not claim to offer it.
	if lists(advertised(t, handler), "gif-search") {
		t.Error("gif search is advertised on a server with nothing configured")
	}
}

/*
TestGifSearchStaysUnavailableWhenSwitchedOnWithoutAProvider separates the two
reasons it can be off.

An operator who turns the toggle on has said what they want; without a provider
the server still cannot do it. The endpoint must refuse either way rather than
reaching for a provider that is not there.
*/
func TestGifSearchStaysUnavailableWhenSwitchedOnWithoutAProvider(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setFeature(t, handler, owner, "gif-search", true)

	recorder := call(t, handler, http.MethodGet, "/api/gifs?q=cat", nil, owner)
	if recorder.Code == http.StatusOK {
		t.Errorf("gif search succeeded with no provider configured: %s",
			truncate(recorder.Body.String()))
	}
}

// TestGifSearchNeedsAuthentication keeps this from being an open proxy.
//
// The request is made on the operator's key. Anybody able to call it without
// an account could spend that key's quota, or use this server to search on
// their behalf.
func TestGifSearchNeedsAuthentication(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/gifs?q=cat", nil, "")

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous gif search returned %d, expected 401", recorder.Code)
	}
}

/*
TestCapabilitiesReflectWhatIsConfigured is how a client knows what to offer.

A control for a feature this server does not have is worse than no control: it
looks broken rather than absent. The capability list is what lets a client hide
it instead.
*/
func TestCapabilitiesReflectWhatIsConfigured(t *testing.T) {
	handler := newTestAPI(t)

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading server info returned %d", recorder.Code)
	}

	var info struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding server info: %v", err)
	}

	// Nothing optional is configured here, so nothing optional is claimed.
	for _, capability := range info.Capabilities {
		if capability == "gif-search" {
			t.Error("the server advertises gif search with no provider configured")
		}
	}

	// Never null. A client iterating the list must not have to guard against
	// its absence, which is the difference between an empty array and null in
	// the JSON.
	if info.Capabilities == nil {
		t.Error("capabilities came back null rather than an empty list")
	}
}
