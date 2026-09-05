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
*/
func TestGifSearchIsUnavailableByDefault(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/gifs?q=cat", nil, token)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("searching gifs returned %d, expected 503 with nothing configured",
			recorder.Code)
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
