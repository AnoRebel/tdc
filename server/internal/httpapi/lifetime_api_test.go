package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

// setLifetime gives a message an expiry.
func setLifetime(
	t *testing.T, handler http.Handler, token, roomID, eventID, kind string, seconds int,
) *httptest.ResponseRecorder {
	t.Helper()

	body := map[string]any{"kind": kind}
	if seconds > 0 {
		body["seconds"] = seconds
	}

	return call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/lifetime", body, token)
}

// lifetimeOfMessage reads what a timeline says about when a message expires.
func lifetimeOfMessage(
	t *testing.T, handler http.Handler, token, roomID, eventID string,
) map[string]any {
	t.Helper()

	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=100", nil, token))

	for _, event := range timeline {
		if id, _ := event["id"].(string); id != eventID {
			continue
		}
		lifetime, _ := event["lifetime"].(map[string]any)
		return lifetime
	}
	return nil
}

/*
TestAMessageExpiresOnItsOwnTimer is the first of the three kinds.

A duration from sending, whether or not anybody read it.
*/
func TestAMessageExpiresOnItsOwnTimer(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := sendMessage(t, handler, owner, roomID, "Siri ya muda")

	if recorder := setLifetime(t, handler, owner, roomID, eventID, "after_sending", 3600); recorder.Code != http.StatusOK {
		t.Fatalf("setting a lifetime returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	// Not yet due.
	if _, err := services.Moderation.ExpireLifetimes(context.Background(), 100, nil); err != nil {
		t.Fatalf("ExpireLifetimes: %v", err)
	}
	if !messageReadable(t, handler, owner, roomID, "Siri ya muda") {
		t.Fatal("a message expired before its lifetime elapsed")
	}

	// Past it.
	ageMessages(t, roomID, "2 hours")
	ageLifetimes(t, roomID, "2 hours")

	removed, err := services.Moderation.ExpireLifetimes(context.Background(), 100, nil)
	if err != nil {
		t.Fatalf("ExpireLifetimes: %v", err)
	}
	if removed == 0 {
		t.Fatal("nothing expired although the lifetime had elapsed")
	}

	if messageReadable(t, handler, owner, roomID, "Siri ya muda") {
		t.Error("an expired message is still readable")
	}
}

/*
TestAMessageExpiresOnceEveryoneHasSeenIt is the second kind.

"Everyone" excludes the sender, who has necessarily seen what they wrote —
waiting for them would mean a message that never expires in a conversation they
have left open.
*/
func TestAMessageExpiresOnceEveryoneHasSeenIt(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	eventID := sendMessage(t, handler, owner, roomID, "Soma kisha itapotea")
	setLifetime(t, handler, owner, roomID, eventID, "once_seen", 0)

	// Nobody else has read it, so it stays.
	if _, err := services.Moderation.ExpireLifetimes(context.Background(), 100, nil); err != nil {
		t.Fatalf("ExpireLifetimes: %v", err)
	}
	if !messageReadable(t, handler, owner, roomID, "Soma kisha itapotea") {
		t.Fatal("a message expired before the other participant had read it")
	}

	// The other participant reads the conversation, which marks it read.
	markRead(t, handler, member, roomID)

	removed, err := services.Moderation.ExpireLifetimes(context.Background(), 100, nil)
	if err != nil {
		t.Fatalf("ExpireLifetimes: %v", err)
	}
	if removed == 0 {
		t.Fatal("a message did not expire although everybody had seen it")
	}

	if messageReadable(t, handler, owner, roomID, "Soma kisha itapotea") {
		t.Error("a message everybody had read is still readable")
	}
}

/*
TestExpiryIsNotServedToAClientOfflineThroughout is the property the requirement
singles out.

Hiding an expired message in a client is not expiry: a client that was not
running never got the instruction. What the server holds has to be gone.
*/
func TestExpiryIsNotServedToAClientOfflineThroughout(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := sendMessage(t, handler, owner, roomID, "Siri kabisa")
	setLifetime(t, handler, owner, roomID, eventID, "after_sending", 60)

	// The lifetime passes with nobody connected.
	ageMessages(t, roomID, "2 hours")
	ageLifetimes(t, roomID, "2 hours")

	if _, err := services.Moderation.ExpireLifetimes(context.Background(), 100, nil); err != nil {
		t.Fatalf("ExpireLifetimes: %v", err)
	}

	// A client asks for everything, as one returning after a week does.
	response := call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=100", nil, owner)
	if strings.Contains(response.Body.String(), "Siri kabisa") {
		t.Errorf("expired content was served to a client that was offline "+
			"throughout: %s", truncate(response.Body.String()))
	}
}

/*
TestAnExpiredMessageLeavesAPlaceholder covers what is left behind.

The conversation must not appear never to have contained it, and the
placeholder must not say what it was.
*/
func TestAnExpiredMessageLeavesAPlaceholder(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := sendMessage(t, handler, owner, roomID, "Siri kabisa")
	setLifetime(t, handler, owner, roomID, eventID, "after_sending", 60)

	ageMessages(t, roomID, "2 hours")
	ageLifetimes(t, roomID, "2 hours")

	if _, err := services.Moderation.ExpireLifetimes(context.Background(), 100, nil); err != nil {
		t.Fatalf("ExpireLifetimes: %v", err)
	}

	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=100", nil, owner))

	found := false
	for _, event := range timeline {
		if id, _ := event["id"].(string); id != eventID {
			continue
		}
		found = true

		if redacted, _ := event["redacted"].(bool); !redacted {
			t.Error("the expired message is not marked as gone")
		}
		if body := bodyOfEvent(event); body != "" {
			t.Errorf("the placeholder discloses what was removed: %q", body)
		}
	}

	if !found {
		t.Error("the expired message vanished entirely, leaving a hole in the " +
			"sequence rather than a placeholder")
	}
}

/*
TestALifetimeIsDisclosedWhileTheMessageIsReadable is what stops somebody being
surprised by a removal.

Somebody who would rather not answer a message on a timer should be able to see
that it is on one.
*/
func TestALifetimeIsDisclosedWhileTheMessageIsReadable(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	eventID := sendMessage(t, handler, owner, roomID, "Itapotea baadaye")
	setLifetime(t, handler, owner, roomID, eventID, "after_sending", 3600)

	// Read by the recipient, not only the sender who set it.
	lifetime := lifetimeOfMessage(t, handler, member, roomID, eventID)
	if lifetime == nil {
		t.Fatal("the recipient cannot see that the message will expire")
	}

	if kind, _ := lifetime["kind"].(string); kind != "after_sending" {
		t.Errorf("the lifetime reports kind %q", kind)
	}
	if _, ok := lifetime["expiresAt"]; !ok {
		t.Error("a lifetime with a known expiry does not disclose when")
	}
}

/*
TestALifetimeCannotBeExtendedThroughTheApi is the rule reaching the endpoint.

The comparison itself is covered in the moderation package; this checks the
refusal a client actually receives.
*/
func TestALifetimeCannotBeExtendedThroughTheApi(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	eventID := sendMessage(t, handler, owner, roomID, "Ujumbe")
	setLifetime(t, handler, owner, roomID, eventID, "after_sending", 600)

	longer := setLifetime(t, handler, owner, roomID, eventID, "after_sending", 3600)
	if longer.Code == http.StatusOK {
		t.Error("a lifetime was extended after sending")
	}

	shorter := setLifetime(t, handler, owner, roomID, eventID, "after_sending", 60)
	if shorter.Code != http.StatusOK {
		t.Errorf("shortening a lifetime was refused: %d: %s",
			shorter.Code, truncate(shorter.Body.String()))
	}
}

/*
TestOnlyTheSenderOrAModeratorMaySetALifetime keeps this from being deletion by
another name.

Anybody else could otherwise put a minute on somebody's message and watch it
go, and the rule against extending would not help — there was nothing to
extend.
*/
func TestOnlyTheSenderOrAModeratorMaySetALifetime(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	author := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, author, roomID)

	stranger := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, stranger, roomID)

	eventID := sendMessage(t, handler, author, roomID, "Ujumbe wangu")

	if refused := setLifetime(t, handler, stranger, roomID, eventID, "after_sending", 60); refused.Code == http.StatusOK {
		t.Error("somebody with no claim on the message set when it expires")
	}

	// Its author may.
	if mine := setLifetime(t, handler, author, roomID, eventID, "after_sending", 60); mine.Code != http.StatusOK {
		t.Errorf("the author could not set a lifetime on their own message: %d", mine.Code)
	}

	// And a moderator may, on anybody's.
	if asModerator := setLifetime(t, handler, owner, roomID, eventID, "after_sending", 30); asModerator.Code != http.StatusOK {
		t.Errorf("a moderator could not set a lifetime: %d", asModerator.Code)
	}
}

/*
TestAttachmentsExpireWithTheirMessage covers the scenario that makes expiry
mean anything for a photograph.

Text that vanished while its picture stayed would have expired nothing that
mattered.
*/
func TestAttachmentsExpireWithTheirMessage(t *testing.T) {
	handler, services := newTestAPIWithServices(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	attachmentID := uploadText(t, handler, owner, roomID, "siri.txt", "something private")

	sent := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "angalia", "attachmentIds": []string{attachmentID}}, owner)
	if sent.Code != http.StatusCreated {
		t.Fatalf("sending with an attachment returned %d", sent.Code)
	}

	var message struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(sent.Body.Bytes(), &message); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	setLifetime(t, handler, owner, roomID, message.ID, "after_sending", 60)

	ageMessages(t, roomID, "2 hours")
	ageLifetimes(t, roomID, "2 hours")

	removed := func(ctx context.Context, ids []uuid.UUID) error {
		for _, id := range ids {
			if err := services.Attachments.Delete(ctx, id); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := services.Moderation.ExpireLifetimes(context.Background(), 100, removed); err != nil {
		t.Fatalf("ExpireLifetimes: %v", err)
	}

	// The file is gone, not merely unreferenced.
	fetched := call(t, handler, http.MethodGet, "/api/attachments/"+attachmentID, nil, owner)
	if fetched.Code == http.StatusOK {
		t.Error("an attachment survived the message that carried it expiring")
	}
}

// TestAnUnknownLifetimeKindIsRefused keeps a typo from meaning something.
func TestAnUnknownLifetimeKindIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Ujumbe")

	if recorder := setLifetime(t, handler, owner, roomID, eventID, "whenever", 60); recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown lifetime kind returned %d, expected 400", recorder.Code)
	}
}

// messageReadable reports whether a message's text is still being served.
func messageReadable(t *testing.T, handler http.Handler, token, roomID, body string) bool {
	t.Helper()

	response := call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=100", nil, token)
	return strings.Contains(response.Body.String(), body)
}

/*
markRead marks everything in a room as read by this person.

A large sequence rather than the exact one: the read mark is a high-water
number and the query takes the greater of the two, so anything past the end of
the conversation means "all of it".
*/
func markRead(t *testing.T, handler http.Handler, token, roomID string) {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/read",
		map[string]any{"sequence": 1_000_000}, token)
	if recorder.Code != http.StatusOK && recorder.Code != http.StatusNoContent {
		t.Fatalf("marking read returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}
}

/*
ageLifetimes backdates the expiry stored against a room's messages.

Separate from ageMessages, which moves the events. A lifetime's expiry is
computed when it is set, so moving the events alone leaves an expiry still in
the future and the message would not be due — which is a test that passes for
the wrong reason.
*/
func ageLifetimes(t *testing.T, roomID, interval string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Open(ctx, db.Config{URL: dbtest.URL(t, "httpapi")}, logger)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
		UPDATE message_lifetimes
		SET expires_at = expires_at - $2::interval
		WHERE room_id = $1 AND expires_at IS NOT NULL`, roomID, interval); err != nil {
		t.Fatalf("ageing lifetimes: %v", err)
	}
}
