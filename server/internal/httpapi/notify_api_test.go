package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type notificationHit struct {
	ID      string `json:"id"`
	RoomID  string `json:"roomId"`
	EventID string `json:"eventId"`
	Reason  string `json:"reason"`
	Sender  string `json:"sender"`
	Preview string `json:"preview"`
}

func notificationsFor(t *testing.T, handler http.Handler, token string) []notificationHit {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/me/notifications", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("listing notifications returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	var hits []notificationHit
	if err := json.Unmarshal(recorder.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decoding notifications: %v", err)
	}
	return hits
}

// TestMentionRaisesANotification covers the ordinary case.
func TestMentionRaisesANotification(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	sendMessage(t, handler, owner, roomID, "Habari @amina, unaweza kusaidia?")

	hits := notificationsFor(t, handler, other)
	if len(hits) != 1 {
		t.Fatalf("amina has %d notifications after being mentioned, expected 1", len(hits))
	}
	if hits[0].Reason != "mention" {
		t.Errorf("the reason is %q, expected mention", hits[0].Reason)
	}
	if hits[0].Preview == "" {
		t.Error("the notification carries no preview of the message")
	}
}

// TestNobodyIsNotifiedOfTheirOwnMessage checks the rule no preference can turn
// off.
//
// Somebody who has just typed something knows about it. A notification for it
// is pure noise, and noise is what teaches people to ignore notifications.
func TestNobodyIsNotifiedOfTheirOwnMessage(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	// Naming yourself is the case a careless implementation gets wrong.
	sendMessage(t, handler, owner, roomID, "Kumbuka @admin kufanya hivi")

	if hits := notificationsFor(t, handler, owner); len(hits) != 0 {
		t.Errorf("the sender has %d notifications about their own message", len(hits))
	}
}

// TestAnUnmentionedMemberIsNotNotified checks the default is mentions-only.
//
// A channel that notified everybody on every message would be unusable, and
// the setting most people never change has to be the one that works.
func TestAnUnmentionedMemberIsNotNotified(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	sendMessage(t, handler, owner, roomID, "Ujumbe wa kawaida, hakuna anayeitwa")

	if hits := notificationsFor(t, handler, other); len(hits) != 0 {
		t.Errorf("an unmentioned member got %d notifications by default", len(hits))
	}
}

// TestMutingSilencesButStillCounts is the milestone's own wording, and the
// distinction that matters.
//
// Muting says "do not interrupt me", not "tell me nothing happened". Somebody
// who mutes a busy channel still wants to see that it has moved on when they
// look at it — so the notification is suppressed and the unread count is not.
func TestMutingSilencesButStillCounts(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	// Amina mutes the channel.
	muted := call(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "all", "muted": true, "room": roomID}, other)
	if muted.Code != http.StatusNoContent {
		t.Fatalf("muting returned %d: %s", muted.Code, truncate(muted.Body.String()))
	}

	// A message that would otherwise have notified her, since it names her.
	sendMessage(t, handler, owner, roomID, "Habari @amina")

	if hits := notificationsFor(t, handler, other); len(hits) != 0 {
		t.Errorf("a muted channel raised %d notifications", len(hits))
	}

	// But the conversation still reads as having moved on.
	unread := decode(t, call(t, handler, http.MethodGet, "/api/me/unread", nil, other))
	if count, _ := unread[roomID].(float64); count == 0 {
		t.Error("muting cleared the unread count; it should only silence the notification")
	}
}

// TestNotificationsClearWhenTheRoomIsRead checks that reading somewhere
// withdraws what another device is showing.
//
// Without this a notification would have to be dismissed once per device,
// which nobody does — so the badge would only ever grow.
func TestNotificationsClearWhenTheRoomIsRead(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	sendMessage(t, handler, owner, roomID, "Tafadhali angalia hii @amina")

	if hits := notificationsFor(t, handler, other); len(hits) != 1 {
		t.Fatalf("expected one notification, got %d", len(hits))
	}

	// Dismissing it directly is the per-notification path.
	hits := notificationsFor(t, handler, other)
	dismissed := call(t, handler, http.MethodPost,
		"/api/me/notifications/"+hits[0].ID+"/read", nil, other)
	if dismissed.Code != http.StatusNoContent {
		t.Fatalf("dismissing returned %d", dismissed.Code)
	}

	if remaining := notificationsFor(t, handler, other); len(remaining) != 0 {
		t.Errorf("%d notifications remain after being read", len(remaining))
	}
}

// TestNotificationCountIsSeparateFromTheList checks the badge endpoint, which
// a client polls far more often than the list.
func TestNotificationCountIsSeparateFromTheList(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	sendMessage(t, handler, owner, roomID, "Ya kwanza @amina")
	sendMessage(t, handler, owner, roomID, "Ya pili @amina")

	counted := decode(t, call(t, handler, http.MethodGet, "/api/me/notifications/count", nil, other))
	if count, _ := counted["unread"].(float64); count != 2 {
		t.Errorf("the count is %v, expected 2", counted["unread"])
	}
}

// TestInvalidPreferenceIsRefused checks that a level outside the three
// permitted values does not become one.
func TestInvalidPreferenceIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	for _, level := range []string{"", "loud", "ALL", "everything"} {
		recorder := call(t, handler, http.MethodPut, "/api/me/preferences/notifications",
			map[string]any{"level": level}, token)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("the level %q returned %d, expected 400", level, recorder.Code)
		}
	}
}

// joinRoom puts somebody in a room, so a test can have a second member.
func joinRoom(t *testing.T, handler http.Handler, token, roomID string) {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("joining returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}
}
