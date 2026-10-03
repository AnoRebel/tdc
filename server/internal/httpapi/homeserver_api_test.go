package httpapi_test

import (
	"net/http"
	"testing"
)

/*
A server with no canonical address owns its own rooms.

Rooms made here are recorded under the server's name, which falls back to a
local default when no canonical address is configured. They were compared
with the canonical address's host instead — empty on such a server — so every
room was reported as somebody else's, and the client sent every message
through federation, which refused it. Nothing could be said anywhere.
*/
func TestRoomsMadeHereAreNotReportedAsElsewhere(t *testing.T) {
	for _, address := range []string{"", "https://tdc.test"} {
		t.Run("canonical address "+address, func(t *testing.T) {
			handler := newTestAPIWithAddress(t, address)
			token := completeSetup(t, handler)
			room := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms", map[string]any{
				"slug": "general", "name": "General",
			}, token, http.StatusCreated))
			if home, present := room["homeServer"]; present {
				t.Errorf("a room made here names a home server: %v", home)
			}
			for _, listed := range decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/rooms", nil, token, http.StatusOK).Body.Bytes()) {
				if home, present := listed["homeServer"]; present {
					t.Errorf("%v is listed with a home server: %v", listed["slug"], home)
				}
			}
		})
	}
}
