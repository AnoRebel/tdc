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
