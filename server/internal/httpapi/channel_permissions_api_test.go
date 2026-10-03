package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// withdraw denies a permission to the Member role in one channel.
func withdraw(t *testing.T, handler http.Handler, admin, permission, roomID string) {
	t.Helper()
	member := roleNamed(t, handler, admin, "Member")
	recorder := call(t, handler, http.MethodPut, "/api/admin/roles/"+member+"/permissions",
		map[string]any{"permission": permission, "channel": roomID, "granted": false}, admin)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("withdrawing %s in %s: status %d, body %s", permission, roomID, recorder.Code, recorder.Body)
	}
}

/*
An announcement channel: withdrawing channel.post from the Member role in one
channel stops members posting there, and nowhere else.

channel.post was in the catalogue and never checked for people, so before this
a withdrawal changed nothing at all — which is the case this would have caught.
*/
func TestWithdrawingPostInOneChannelMakesItReadOnly(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	announcements := makeRoom(t, handler, admin, "announcements", "public")
	general := makeRoom(t, handler, admin, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	for _, room := range []string{announcements, general} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, member, http.StatusOK)
	}

	withdraw(t, handler, admin, "channel.post", announcements)

	refused := call(t, handler, http.MethodPost, "/api/rooms/"+announcements+"/messages",
		map[string]any{"body": "may I?"}, member)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("posting where channel.post was withdrawn: status %d, want 403", refused.Code)
	}

	// Every other way of putting words into the channel is closed too.
	for _, path := range []string{"/scheduled"} {
		recorder := call(t, handler, http.MethodPost, "/api/rooms/"+announcements+path,
			map[string]any{"body": "later", "sendAt": "2099-01-01T00:00:00Z"}, member)
		if recorder.Code != http.StatusForbidden && recorder.Code != http.StatusNotFound {
			t.Errorf("POST %s where channel.post was withdrawn: status %d", path, recorder.Code)
		}
	}

	// The withdrawal is confined to that channel.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "fine here"}, member, http.StatusCreated)

	// And reading the announcements is untouched — read-only, not hidden.
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+announcements+"/timeline", nil, member, http.StatusOK)

	// An administrator still posts announcements.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+announcements+"/messages",
		map[string]any{"body": "an announcement"}, admin, http.StatusCreated)
}

/*
The few who announce keep the channel the rest may only read.

Everybody holds Member, including whoever announces, so a denial on Member that
beat every grant would silence them too and leave administrators as the only
voice. A role granting channel.post in that channel lets them back in; so does
being made an administrator after registering, which also leaves Member held.
*/
func TestAnnouncersPostWhereMembersMayOnlyRead(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	announcements := makeRoom(t, handler, admin, "announcements", "public")

	member := registerAndSignIn(t, handler, "amina")
	announcer := registerAndSignIn(t, handler, "baraka")
	promoted := registerAndSignIn(t, handler, "chausiku")
	for _, person := range []string{member, announcer, promoted} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+announcements+"/join", nil, person, http.StatusOK)
	}

	withdraw(t, handler, admin, "channel.post", announcements)

	created := decode(t, mustCall(t, handler, http.MethodPost, "/api/admin/roles",
		map[string]string{"name": "Announcer"}, admin, http.StatusCreated))
	announcerRole, _ := created["id"].(string)
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+announcerRole+"/permissions",
		map[string]any{"permission": "channel.post", "channel": announcements, "granted": true},
		admin, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+announcerRole+"/members",
		map[string]string{"user": accountID(t, handler, announcer)}, admin, http.StatusNoContent)

	mustCall(t, handler, http.MethodPost,
		"/api/admin/roles/"+roleNamed(t, handler, admin, "Administrator")+"/members",
		map[string]string{"user": accountID(t, handler, promoted)}, admin, http.StatusNoContent)

	post := func(token, body string) int {
		return call(t, handler, http.MethodPost, "/api/rooms/"+announcements+"/messages",
			map[string]any{"body": body}, token).Code
	}
	if code := post(member, "may I?"); code != http.StatusForbidden {
		t.Errorf("a member posted in the announcements: status %d, want 403", code)
	}
	if code := post(announcer, "the meetup moves to Saturday"); code != http.StatusCreated {
		t.Errorf("the announcer could not post: status %d, want 201", code)
	}
	if code := post(promoted, "welcome, everyone"); code != http.StatusCreated {
		t.Errorf("an administrator who also holds Member could not post: status %d, want 201", code)
	}
}

/*
Forwarding into a channel is posting in it.

The route's own channel is the source, so without a check on the destination
forwarding would be a way to post in a channel somebody may only read.
*/
func TestForwardingIntoAReadOnlyChannelIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	announcements := makeRoom(t, handler, admin, "announcements", "public")
	general := makeRoom(t, handler, admin, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	for _, room := range []string{announcements, general} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, member, http.StatusOK)
	}

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "worth sharing"}, member, http.StatusCreated))
	eventID, _ := sent["id"].(string)

	withdraw(t, handler, admin, "channel.post", announcements)

	recorder := call(t, handler, http.MethodPost,
		"/api/rooms/"+general+"/messages/"+eventID+"/forward",
		map[string]any{"to": announcements}, member)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("forwarding into a read-only channel: status %d, want 403; body %s", recorder.Code, recorder.Body)
	}
}

// Withdrawing channel.read in one channel closes its timeline, threads, pins
// and member list to the role, whether or not somebody is a member.
func TestWithdrawingReadInOneChannelClosesIt(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	staff := makeRoom(t, handler, admin, "staff", "public")
	member := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+staff+"/join", nil, member, http.StatusOK)

	withdraw(t, handler, admin, "channel.read", staff)

	for _, path := range []string{"/timeline", "/threads", "/pins", "/members", "/reactions", "/presence"} {
		if recorder := call(t, handler, http.MethodGet, "/api/rooms/"+staff+path, nil, member); recorder.Code != http.StatusForbidden {
			t.Errorf("GET %s where channel.read was withdrawn: status %d, want 403", path, recorder.Code)
		}
	}
}

// A fresh server's Member role holds both permissions from the start, so
// enforcing them refuses nobody who could post before.
func TestTheMemberRoleReadsAndPostsByDefault(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	room := makeRoom(t, handler, admin, "general", "public")
	member := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, member, http.StatusOK)

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "hello"}, member, http.StatusCreated)
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/timeline", nil, member, http.StatusOK)
}

/*
The live connection honours channel.read as the timeline does.

Otherwise withdrawing it would close the timeline and leave the same messages
streaming to anybody who subscribed — membership alone was what the live
connection checked. The member here is in the channel; only the permission is
gone, and the subscription must be refused.
*/
func TestALiveSubscriptionNeedsChannelRead(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	staff := makeRoom(t, handler, admin, "staff", "public")
	member := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+staff+"/join", nil, member, http.StatusOK)

	withdraw(t, handler, admin, "channel.read", staff)

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + member}}})
	if err != nil {
		t.Fatalf("opening the live connection: %v", err)
	}
	defer func() { _ = socket.Close(websocket.StatusNormalClosure, "") }()

	for {
		_, data, err := socket.Read(ctx)
		if err != nil {
			t.Fatalf("the connection ended before answering the subscription: %v", err)
		}
		var frame map[string]any
		if json.Unmarshal(data, &frame) != nil {
			continue
		}

		switch frame["type"] {
		case "ready":
			request, _ := json.Marshal(map[string]any{"action": "subscribe", "rooms": []string{staff}})
			if err := socket.Write(ctx, websocket.MessageText, request); err != nil {
				t.Fatalf("subscribing: %v", err)
			}
		case "subscribed":
			if rooms, _ := frame["rooms"].([]any); len(rooms) > 0 {
				t.Fatalf("subscribed to a channel whose channel.read was withdrawn: %v", frame)
			}
		case "error":
			payload, _ := frame["payload"].(map[string]any)
			message, _ := payload["message"].(string)
			if strings.Contains(message, "cannot subscribe") {
				return
			}
			t.Fatalf("an unexpected error frame: %v", frame)
		}
	}
}

// A webhook is a way of reading, so a channel closed to somebody cannot be
// subscribed to by them either.
func TestAWebhookNeedsChannelRead(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	staff := makeRoom(t, handler, admin, "staff", "public")
	member := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+staff+"/join", nil, member, http.StatusOK)

	withdraw(t, handler, admin, "channel.read", staff)

	recorder := call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"roomId": staff, "label": "listener", "url": "https://example.org/hook",
		"eventTypes": []string{"message.sent"},
	}, member)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("subscribing to a channel whose channel.read was withdrawn: status %d, want 404", recorder.Code)
	}
}
