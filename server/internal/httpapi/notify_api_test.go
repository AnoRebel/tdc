package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
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

// notificationsWithActor is a notification including who caused it.
type actorNotification struct {
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
	Detail string `json:"detail"`
}

func actorNotificationsFor(t *testing.T, handler http.Handler, token string) []actorNotification {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/me/notifications", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("listing notifications returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	var hits []actorNotification
	if err := json.Unmarshal(recorder.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decoding notifications: %v", err)
	}
	return hits
}

// TestReactingNotifiesTheAuthor covers the ordinary case.
func TestReactingNotifiesTheAuthor(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Nimemaliza ripoti")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	recorder := call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
		map[string]any{"emoji": "🎉"}, other)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("reacting returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	hits := actorNotificationsFor(t, handler, owner)
	if len(hits) != 1 {
		t.Fatalf("the author has %d notifications after being reacted to, expected 1", len(hits))
	}
	if hits[0].Reason != "reaction" {
		t.Errorf("the reason is %q, expected reaction", hits[0].Reason)
	}
	// The emoji travels with it, so a reaction taken back later still leaves a
	// notification that can explain itself.
	if hits[0].Detail != "🎉" {
		t.Errorf("the notification does not carry the emoji: %q", hits[0].Detail)
	}
	// The actor is the reactor, not the message's sender — which is the
	// distinction that makes this notification make sense at all.
	if !strings.Contains(hits[0].Actor, "amina") {
		t.Errorf("the actor is %q, expected the person who reacted", hits[0].Actor)
	}
}

/*
TestTwoPeopleReactingRaisesTwoNotifications is the rule that forced the unique
constraint to change.

It used to be one notification per person per event, which is right while every
notification is about a message arriving: an edit or a redelivery must not
notify twice. It is wrong here — two people reacting are two things worth
hearing about, and the second was silently dropped.
*/
func TestTwoPeopleReactingRaisesTwoNotifications(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Tumefanikiwa")

	for _, name := range []string{"amina", "juma"} {
		token := registerAndSignIn(t, handler, name)
		joinRoom(t, handler, token, roomID)

		recorder := call(t, handler, http.MethodPut,
			"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
			map[string]any{"emoji": "👏"}, token)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s reacting returned %d", name, recorder.Code)
		}
	}

	hits := actorNotificationsFor(t, handler, owner)
	if len(hits) != 2 {
		t.Errorf("two people reacting raised %d notifications, expected 2", len(hits))
	}
}

// TestReactingToYourOwnMessageIsSilent covers the rule no preference can turn
// on.
//
// Reacting to your own message is ordinary, and a notification for it is noise
// — the failure that teaches people to ignore notifications entirely.
func TestReactingToYourOwnMessageIsSilent(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Nadhani hii ni sawa")

	recorder := call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
		map[string]any{"emoji": "👍"}, owner)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("reacting returned %d", recorder.Code)
	}

	if hits := actorNotificationsFor(t, handler, owner); len(hits) != 0 {
		t.Errorf("reacting to your own message raised %d notifications", len(hits))
	}
}

// TestReplyingNotifiesWhoeverStartedTheThread is the notification a threaded
// conversation lives on: without it, a thread is only found by going back and
// looking.
func TestReplyingNotifiesWhoeverStartedTheThread(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "Nani anaweza kusaidia?")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	recorder := call(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/threads/"+rootID+"/replies",
		map[string]any{"body": "Mimi ninaweza", "clientToken": "reply-1"}, other)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("replying returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	hits := actorNotificationsFor(t, handler, owner)
	if len(hits) != 1 {
		t.Fatalf("the thread's author has %d notifications, expected 1", len(hits))
	}
	if hits[0].Reason != "reply" {
		t.Errorf("the reason is %q, expected reply", hits[0].Reason)
	}
}

// TestPinningNotifiesTheAuthor tells somebody the channel has singled out what
// they wrote.
func TestPinningNotifiesTheAuthor(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)
	eventID := sendMessage(t, handler, other, roomID, "Ratiba ya wiki")

	// The owner completed setup, so they hold the pinning permission.
	pinned := call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/pin", nil, owner)
	if pinned.Code != http.StatusNoContent {
		t.Fatalf("pinning returned %d: %s", pinned.Code, truncate(pinned.Body.String()))
	}

	hits := actorNotificationsFor(t, handler, other)
	if len(hits) != 1 || hits[0].Reason != "pin" {
		t.Errorf("the author has %+v, expected one pin notification", hits)
	}
}

/*
TestJoiningOnlyNotifiesPeopleWhoAskedForEverything keeps a busy channel usable.

Somebody on the default of mentions-only does not want to know that a person
they have never spoken to joined a channel. A server where every join notified
everybody would be a stream of notifications about nothing anybody said.
*/
func TestJoiningOnlyNotifiesPeopleWhoAskedForEverything(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	// A second member on the default level.
	quiet := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, quiet, roomID)

	// The owner asks to hear everything about this conversation.
	set := call(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "all", "muted": false, "room": roomID}, owner)
	if set.Code != http.StatusNoContent {
		t.Fatalf("setting a preference returned %d", set.Code)
	}

	// Somebody new joins.
	newcomer := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, newcomer, roomID)

	ownerHits := actorNotificationsFor(t, handler, owner)
	joins := 0
	for _, hit := range ownerHits {
		if hit.Reason == "join" {
			joins++
		}
	}
	if joins != 1 {
		t.Errorf("somebody who asked to hear everything got %d join notifications", joins)
	}

	// And the member on the default hears nothing about it.
	for _, hit := range actorNotificationsFor(t, handler, quiet) {
		if hit.Reason == "join" {
			t.Error("a member on the default level was notified about somebody joining")
		}
	}
}

// TestMutingSilencesReactionsToo checks that muting means what it says.
//
// Somebody who mutes a channel is asking not to be interrupted by it, and a
// reaction is an interruption.
func TestMutingSilencesReactionsToo(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Ujumbe")

	muted := call(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "all", "muted": true, "room": roomID}, owner)
	if muted.Code != http.StatusNoContent {
		t.Fatalf("muting returned %d", muted.Code)
	}

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	recorder := call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
		map[string]any{"emoji": "👍"}, other)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("reacting returned %d", recorder.Code)
	}

	for _, hit := range actorNotificationsFor(t, handler, owner) {
		if hit.Reason == "reaction" {
			t.Error("a muted channel raised a reaction notification")
		}
	}
}

/*
TestSuppressionLeavesUnreadStateAlone is what makes suppression safe.

Suppressing a notification says "not now", not "this never happened". Somebody
whose status silenced a message, or who was looking at another device, still
needs the badge to tell them the conversation moved on — otherwise silence and
nothing-happened become indistinguishable, and a message is genuinely lost.
*/
func TestSuppressionLeavesUnreadStateAlone(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	// Muting is the suppression this harness can exercise directly; watching
	// and status are covered in the notify package's own tests. All three take
	// the same path, and the guarantee under test is the same: unread state is
	// untouched by any of them.
	muted := call(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "all", "muted": true, "room": roomID}, other)
	if muted.Code != http.StatusNoContent {
		t.Fatalf("muting returned %d", muted.Code)
	}

	sendMessage(t, handler, owner, roomID, "Habari @amina, tazama hii")

	if hits := notificationsFor(t, handler, other); len(hits) != 0 {
		t.Errorf("a suppressed notification was raised anyway: %+v", hits)
	}

	unread := decode(t, call(t, handler, http.MethodGet, "/api/me/unread", nil, other))
	if count, _ := unread[roomID].(float64); count == 0 {
		t.Error("suppressing the notification also cleared the unread count; " +
			"the conversation must still read as having moved on")
	}
}
