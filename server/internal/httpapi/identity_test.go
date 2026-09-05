package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
TestHostHeaderCannotChangeIdentity is the rule 7.1 exists for.

A Host header is supplied by whoever is asking. A server that derived its own
name from one would let anybody make it introduce itself as their domain — and
since addresses are how federation attributes messages, that is a server
claiming somebody else's identity on request.

Every place the server names itself is checked, because getting it right in one
and wrong in another is the shape this bug actually takes.
*/
func TestHostHeaderCannotChangeIdentity(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	// What the server says when asked honestly.
	honest := serverIdentity(t, handler, "")

	spoofed := []string{
		"evil.example",
		"attacker.test:8080",
		"localhost:9999",
	}

	for _, host := range spoofed {
		t.Run(host, func(t *testing.T) {
			claimed := serverIdentity(t, handler, host)

			if claimed != honest {
				t.Errorf("a Host header of %q changed the reported identity "+
					"from %q to %q", host, honest, claimed)
			}
			if strings.Contains(claimed, "evil") || strings.Contains(claimed, "attacker") {
				t.Errorf("the server introduced itself as %q", claimed)
			}
		})
	}

	// And the address on a person, which is what federation attributes
	// messages by.
	for _, host := range spoofed {
		request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		request.Host = host
		request.Header.Set("Authorization", "Bearer "+token)

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		var me struct {
			Address string `json:"address"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &me); err != nil {
			t.Fatalf("decoding: %v", err)
		}

		if strings.Contains(me.Address, host) {
			t.Errorf("a Host header of %q put itself into a person's address: %q",
				host, me.Address)
		}
	}
}

// serverIdentity asks the server what it calls itself, optionally lying about
// the host in the request.
func serverIdentity(t *testing.T, handler http.Handler, host string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/server", nil)
	if host != "" {
		request.Host = host
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("reading server info returned %d", recorder.Code)
	}

	var info struct {
		CanonicalAddress string `json:"canonicalAddress"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding server info: %v", err)
	}
	return info.CanonicalAddress
}

/*
TestSharedLinksUseTheConfiguredAddress covers the other place a Host header
would reach.

A link preview's canonical URL is built by the server and read by whoever the
link was shared with. Built from a header, it would let somebody generate a
preview pointing at their own domain while appearing to come from this server.
*/
func TestSharedLinksUseTheConfiguredAddress(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wazi", "public")

	request := httptest.NewRequest(http.MethodGet, "/rooms/"+roomID, nil)
	request.Host = "evil.example"

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if strings.Contains(recorder.Body.String(), "evil.example") {
		t.Errorf("a Host header reached a shared link's canonical URL:\n%s",
			truncate(recorder.Body.String()))
	}
}
