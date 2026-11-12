package httpapi_test

import (
	"net/http"
	"testing"
)

/*
The address a person is told is theirs is the one their messages carry.

A client decides which messages are somebody's own by comparing the two, so a
difference of a scheme or a port means nobody can edit or delete anything they
wrote, and is offered to block themselves. It was exactly that on any server
configured with a scheme in its canonical address.
*/
func TestAPersonsAddressIsTheOneTheirMessagesCarry(t *testing.T) {
	handler := newTestAPIWithAddress(t, "https://chat.example.org:8443")
	founder := completeSetup(t, handler)

	me := decode(t, mustCall(t, handler, http.MethodGet, "/api/me", nil, founder, http.StatusOK))
	room := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms",
		map[string]any{"slug": "mine"}, founder, http.StatusCreated))["id"].(string)
	posted := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "is this mine?"}, founder, http.StatusCreated))

	if me["address"] != posted["sender"] {
		t.Errorf("the account is %v but its message is from %v", me["address"], posted["sender"])
	}
}
