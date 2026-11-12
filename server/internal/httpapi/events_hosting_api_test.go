package httpapi_test

import (
	"net/http"
	"testing"
)

/*
An event's call is its conversation's call: a host starts it, people going
join it, and somebody going cannot start it themselves. An event not hosted
online has no call at all. Sabotage-verified: letting anybody start it fails
the first refusal.
*/
func TestAHostStartsAnEventsCallAndAttendeesJoin(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	setFeature(t, handler, admin, "calls", true)
	host := registerAndSignIn(t, handler, "host")
	amina := registerAndSignIn(t, handler, "amina")
	outsider := registerAndSignIn(t, handler, "outsider")

	id := makeEvent(t, handler, host, map[string]any{"online": true, "kind": "talk"})["id"].(string)
	answer(t, handler, amina, id, "going")

	if code := call(t, handler, http.MethodPost, "/api/rooms/"+id+"/call", map[string]any{"audio": true}, amina).Code; code != http.StatusForbidden {
		t.Errorf("an attendee started the event's call: status %d", code)
	}
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+id+"/call", map[string]any{"audio": true}, host, http.StatusOK)
	joined := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+id+"/call", map[string]any{"audio": true}, amina, http.StatusOK))
	if len(joined["participants"].([]any)) != 2 {
		t.Errorf("the call holds %v", joined["participants"])
	}
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+id+"/call", map[string]any{"audio": true}, outsider).Code; code == http.StatusOK {
		t.Error("somebody not going joined the event's call")
	}

	offline := makeEvent(t, handler, host, nil)["id"].(string)
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+offline+"/call", map[string]any{"audio": true}, host).Code; code != http.StatusConflict {
		t.Errorf("a call started in an event not hosted online: status %d", code)
	}
}

/*
A cover is uploaded to the event's conversation and shown to anybody who can
see the event, and to nobody else.
*/
func TestACoverIsSeenByWhoeverSeesTheEvent(t *testing.T) {
	handler := newTestAPI(t)
	_ = completeSetup(t, handler)
	host := registerAndSignIn(t, handler, "host")
	amina := registerAndSignIn(t, handler, "amina")

	public := makeEvent(t, handler, host, nil)["id"].(string)
	closed := makeEvent(t, handler, host, map[string]any{"visibility": "invite"})["id"].(string)
	for _, id := range []string{public, closed} {
		image := uploadImage(t, handler, host, id, "cover.png")
		mustCall(t, handler, http.MethodPut, "/api/events/"+id+"/cover", map[string]any{"attachmentId": image}, host, http.StatusOK)
		want := http.StatusOK
		if id == closed {
			want = http.StatusNotFound
		}
		if code := call(t, handler, http.MethodGet, "/api/attachments/"+image, nil, amina).Code; code != want {
			t.Errorf("amina fetching the cover of %s: status %d, want %d", id, code, want)
		}
	}
}
