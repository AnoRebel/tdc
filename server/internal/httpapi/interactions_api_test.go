package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reactionHit struct {
	Emoji string   `json:"emoji"`
	Count int      `json:"count"`
	Users []string `json:"users"`
	Mine  bool     `json:"mine"`
}

func reactionsIn(t *testing.T, handler http.Handler, token, roomID string) map[string][]reactionHit {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/reactions", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading reactions returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var hits map[string][]reactionHit
	if err := json.Unmarshal(recorder.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decoding reactions: %v", err)
	}
	return hits
}

// uploadText puts a small text file in a room and returns its identifier.
func uploadText(t *testing.T, handler http.Handler, token, roomID, filename, content string) string {
	t.Helper()

	recorder := uploadFile(t, handler, token, roomID, filename, []byte(content))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var uploaded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decoding the upload: %v", err)
	}
	return uploaded.ID
}

// TestReactingAndTakingItBack covers the ordinary cycle.
func TestReactingAndTakingItBack(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Habari za asubuhi")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	added := call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
		map[string]any{"emoji": "👍"}, other)
	if added.Code != http.StatusNoContent {
		t.Fatalf("reacting returned %d: %s", added.Code, truncate(added.Body.String()))
	}

	hits := reactionsIn(t, handler, other, roomID)
	if len(hits[eventID]) != 1 || hits[eventID][0].Count != 1 {
		t.Fatalf("expected one reaction with a count of one, got %+v", hits[eventID])
	}
	if !hits[eventID][0].Mine {
		t.Error("the reactor's own reaction is not marked as theirs")
	}

	// The sender sees the same reaction, and it is not theirs.
	fromOwner := reactionsIn(t, handler, owner, roomID)
	if len(fromOwner[eventID]) != 1 {
		t.Fatalf("the sender sees %d reactions", len(fromOwner[eventID]))
	}
	if fromOwner[eventID][0].Mine {
		t.Error("somebody else's reaction is marked as the reader's own")
	}

	removed := call(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions?emoji=%F0%9F%91%8D", nil, other)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("removing a reaction returned %d", removed.Code)
	}

	if after := reactionsIn(t, handler, other, roomID); len(after[eventID]) != 0 {
		t.Errorf("%d reactions remain after removal", len(after[eventID]))
	}
}

/*
TestEventIdentifiersWorkEncodedOrNot is a regression test for a bug that was
invisible until a client behaved correctly.

Event identifiers begin with "$", which a correct client percent-encodes in a
path. Echo hands back the raw segment rather than the decoded one, so reading
the parameter directly yielded "%24abc" and every lookup missed. Clients that
sent the "$" unencoded worked, which is why it went unnoticed — so both
spellings are tested.
*/
func TestEventIdentifiersWorkEncodedOrNot(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Ujumbe wa kujaribu")

	// url.PathEscape leaves "$" alone, since it is a legal path character —
	// so it does not reproduce this at all. A browser's encodeURIComponent
	// does escape it, which is what a client actually sends.
	spellings := map[string]string{
		"raw":     eventID,
		"encoded": strings.Replace(eventID, "$", "%24", 1),
	}

	for name, spelling := range spellings {
		t.Run(name, func(t *testing.T) {
			recorder := callRawPath(t, handler, http.MethodGet,
				"/api/rooms/"+roomID+"/threads/"+spelling, nil, owner)

			if recorder.Code != http.StatusOK {
				t.Errorf("reading a thread by its %s identifier returned %d: %s",
					name, recorder.Code, truncate(recorder.Body.String()))
			}
		})
	}

	// And the same for a route that writes, since that is where it was found.
	recorder := callRawPath(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+strings.Replace(eventID, "$", "%24", 1)+"/reactions",
		map[string]any{"emoji": "👍"}, owner)
	if recorder.Code != http.StatusNoContent {
		t.Errorf("reacting by an encoded identifier returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}
}

/*
callRawPath issues a request with the path left exactly as written.

httptest.NewRequest parses the URL and fills in the decoded path, so a test
using it never sees what a real client sends: the router gets "$abc" where a
browser sent "%24abc". Setting RawPath is what makes an encoded identifier
reach the handler encoded, which is the condition the decoding exists for.
*/
func callRawPath(
	t *testing.T,
	handler http.Handler,
	method, path string,
	body any,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, reader)

	// Go only keeps RawPath when it differs from the decoded form, so this is
	// set explicitly rather than relying on the parse.
	request.URL.RawPath = path

	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// TestReactingTwiceCountsOnce checks the rule the unique constraint encodes.
//
// Two devices, or one impatient double click, must not make somebody appear to
// have agreed twice.
func TestReactingTwiceCountsOnce(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Tunaanza saa ngapi?")

	for range 3 {
		recorder := call(t, handler, http.MethodPut,
			"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
			map[string]any{"emoji": "🎉"}, owner)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("reacting returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
		}
	}

	hits := reactionsIn(t, handler, owner, roomID)
	if len(hits[eventID]) != 1 {
		t.Fatalf("expected one emoji, got %d", len(hits[eventID]))
	}
	if hits[eventID][0].Count != 1 {
		t.Errorf("reacting three times counted %d", hits[eventID][0].Count)
	}
}

// TestReactionsRefuseFreeText keeps the field from becoming a second message
// body, over the wire rather than only in the validator's own test.
func TestReactionsRefuseFreeText(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Ujumbe")

	for _, emoji := range []string{"this is a whole sentence", "lol", "", "a"} {
		recorder := call(t, handler, http.MethodPut,
			"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
			map[string]any{"emoji": emoji}, owner)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("the reaction %q returned %d, expected 400", emoji, recorder.Code)
		}
	}
}

/*
TestReactingRequiresMembership keeps a public room readable without being
participable.

A public channel can be read without joining. Reacting is participation: it
puts somebody's name in the room, so it takes membership rather than mere
visibility.
*/
func TestReactingRequiresMembership(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Karibu")

	// Signed in, has not joined.
	stranger := registerAndSignIn(t, handler, "amina")

	recorder := call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/reactions",
		map[string]any{"emoji": "👍"}, stranger)

	if recorder.Code == http.StatusNoContent {
		t.Error("somebody who has not joined was able to react")
	}
}

// TestPinningShowsForEverybody covers a pin being the room speaking.
func TestPinningShowsForEverybody(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	eventID := sendMessage(t, handler, owner, roomID, "Ratiba ya wiki hii")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	pinned := call(t, handler, http.MethodPut,
		"/api/rooms/"+roomID+"/messages/"+eventID+"/pin", nil, owner)
	if pinned.Code != http.StatusNoContent {
		t.Fatalf("pinning returned %d: %s", pinned.Code, truncate(pinned.Body.String()))
	}

	// Somebody else sees it, which is what makes a pin different from a
	// bookmark.
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/pins", nil, other)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading pins returned %d", recorder.Code)
	}

	var pins []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &pins); err != nil {
		t.Fatalf("decoding pins: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("expected one pinned message, got %d", len(pins))
	}
}

/*
TestForwardingCopiesRatherThanLinks checks the decision that keeps a forwarded
message readable.

A link would mean the forwarded message vanishes when the original is deleted,
and is only readable by people who can see where it came from. A copy carries
its own text, with attribution.
*/
func TestForwardingCopiesRatherThanLinks(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	source := makeRoom(t, handler, owner, "source", "public")
	destination := makeRoom(t, handler, owner, "destination", "public")

	eventID := sendMessage(t, handler, owner, source, "Tarehe imebadilika")

	recorder := call(t, handler, http.MethodPost,
		"/api/rooms/"+source+"/messages/"+eventID+"/forward",
		map[string]any{"to": destination, "comment": "Angalia hii"}, owner)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("forwarding returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "Tarehe imebadilika") {
		t.Errorf("the forwarded message does not carry the original text: %s", truncate(body))
	}
	if !strings.Contains(body, "forwardedFrom") {
		t.Errorf("the forwarded message does not say where it came from: %s", truncate(body))
	}
}

/*
TestForwardingOutOfAPrivateRoomIsRefused stops forwarding being an extraction
tool.

Without a check at the source, anybody holding an event identifier could
forward a message out of a channel they cannot read into one they can — which
would make private conversations readable by guessing.
*/
func TestForwardingOutOfAPrivateRoomIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	private := makeRoom(t, handler, owner, "bodi", "private")
	eventID := sendMessage(t, handler, owner, private, "Siri ya bodi")

	// A public room the stranger is in, so the only thing standing between
	// them and the private message is the check being tested.
	destination := makeRoom(t, handler, owner, "wazi", "public")

	stranger := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, stranger, destination)

	recorder := call(t, handler, http.MethodPost,
		"/api/rooms/"+private+"/messages/"+eventID+"/forward",
		map[string]any{"to": destination}, stranger)

	if recorder.Code == http.StatusCreated {
		t.Error("a message was forwarded out of a private room by somebody not in it")
	}
	if strings.Contains(recorder.Body.String(), "Siri ya bodi") {
		t.Error("the refusal disclosed the message it refused to forward")
	}
}

// TestSchedulingRefusesThePast checks the bounds.
func TestSchedulingRefusesThePast(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	for _, when := range []time.Time{
		time.Now().Add(-time.Hour),
		time.Now(),
		time.Now().Add(2 * 365 * 24 * time.Hour),
	} {
		recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
			map[string]any{"body": "Baadaye", "sendAt": when.Format(time.RFC3339)}, owner)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("scheduling for %s returned %d, expected 400", when, recorder.Code)
		}
	}
}

/*
TestScheduledMessageIsNotInTheTimelineYet is the point of holding it outside the
event log.

A scheduled message that appeared in the timeline immediately would either show
everybody a message before it was sent, or require every reader to know to
filter it out.
*/
func TestScheduledMessageIsNotInTheTimelineYet(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	scheduled := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
		map[string]any{
			"body":   "Kesho asubuhi",
			"sendAt": time.Now().Add(time.Hour).Format(time.RFC3339),
		}, owner)
	if scheduled.Code != http.StatusCreated {
		t.Fatalf("scheduling returned %d: %s", scheduled.Code, truncate(scheduled.Body.String()))
	}

	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, owner)
	if strings.Contains(timeline.Body.String(), "Kesho asubuhi") {
		t.Error("a scheduled message is already in the timeline")
	}

	// It is listed as the sender's own outstanding message.
	mine := call(t, handler, http.MethodGet, "/api/me/scheduled", nil, owner)
	if !strings.Contains(mine.Body.String(), "Kesho asubuhi") {
		t.Errorf("the scheduled message is not listed: %s", truncate(mine.Body.String()))
	}
}

// TestCancellingSomebodyElsesScheduledMessageIsRefused keeps a plan its
// author's own.
func TestCancellingSomebodyElsesScheduledMessageIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	scheduled := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
		map[string]any{
			"body":   "Yangu",
			"sendAt": time.Now().Add(time.Hour).Format(time.RFC3339),
		}, owner)

	var created map[string]any
	if err := json.Unmarshal(scheduled.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	id, _ := created["id"].(string)

	stranger := registerAndSignIn(t, handler, "amina")

	recorder := call(t, handler, http.MethodDelete, "/api/me/scheduled/"+id, nil, stranger)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("cancelling somebody else's scheduled message returned %d", recorder.Code)
	}

	// And it is still there.
	mine := call(t, handler, http.MethodGet, "/api/me/scheduled", nil, owner)
	if !strings.Contains(mine.Body.String(), "Yangu") {
		t.Error("somebody else cancelled a scheduled message")
	}
}

/*
TestReportSurvivesTheMessageBeingDeleted is why the reported text is copied.

Moderation happens after the fact, usually after the author has noticed and
deleted what they wrote. A report saying only "message $abc, now missing"
cannot be acted on.
*/
func TestReportSurvivesTheMessageBeingDeleted(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	offender := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, offender, roomID)
	eventID := sendMessage(t, handler, offender, roomID, "Maneno mabaya sana")

	filed := call(t, handler, http.MethodPost, "/api/reports", map[string]any{
		"room":    roomID,
		"eventId": eventID,
		"reason":  "harassment",
		"detail":  "Ameniudhi",
	}, owner)
	if filed.Code != http.StatusCreated {
		t.Fatalf("filing returned %d: %s", filed.Code, truncate(filed.Body.String()))
	}

	// The author deletes it.
	deleted := call(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/messages/"+eventID, nil, offender)
	// A deletion is an event appended to the log rather than a row removed, so
	// it is created rather than merely accepted.
	if deleted.Code != http.StatusCreated {
		t.Fatalf("deleting returned %d: %s", deleted.Code, truncate(deleted.Body.String()))
	}

	// The report still says what was said. The owner completed setup, so they
	// hold the moderation permission.
	queue := call(t, handler, http.MethodGet, "/api/reports", nil, owner)
	if queue.Code != http.StatusOK {
		t.Fatalf("reading the queue returned %d: %s", queue.Code, truncate(queue.Body.String()))
	}
	if !strings.Contains(queue.Body.String(), "Maneno mabaya sana") {
		t.Errorf("the report lost what it was about: %s", truncate(queue.Body.String()))
	}
}

/*
TestModerationQueueNeedsPermission keeps reports from being readable by whoever
asks.

A queue lists what people reported, who reported it, and the text — a
compilation of exactly the material that should not be browsable.
*/
func TestModerationQueueNeedsPermission(t *testing.T) {
	handler := newTestAPI(t)

	completeSetup(t, handler)
	ordinary := registerAndSignIn(t, handler, "amina")

	for _, path := range []string{"/api/reports", "/api/reports/count"} {
		recorder := call(t, handler, http.MethodGet, path, nil, ordinary)
		if recorder.Code == http.StatusOK {
			t.Errorf("%s was readable without the moderation permission", path)
		}
	}
}

// TestUnknownReportReasonIsRefused keeps the queue sortable.
func TestUnknownReportReasonIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/reports", map[string]any{
		"subject": "amina@localhost",
		"reason":  "i just do not like them",
	}, owner)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown reason returned %d, expected 400", recorder.Code)
	}
}

/*
TestPinnedRoomsArePrivateToThePerson checks the difference between the two kinds
of pin.

Pinning a message is the room speaking; pinning a conversation is a bookmark.
Somebody else's bookmarks are not theirs to see.
*/
func TestPinnedRoomsArePrivateToThePerson(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	pinned := call(t, handler, http.MethodPut, "/api/me/pins/"+roomID, nil, owner)
	if pinned.Code != http.StatusNoContent {
		t.Fatalf("pinning a room returned %d: %s", pinned.Code, truncate(pinned.Body.String()))
	}

	mine := call(t, handler, http.MethodGet, "/api/me/pins", nil, owner)
	if !strings.Contains(mine.Body.String(), roomID) {
		t.Errorf("the pinned room is not listed: %s", truncate(mine.Body.String()))
	}

	// Somebody else's list does not have it.
	other := registerAndSignIn(t, handler, "amina")
	theirs := call(t, handler, http.MethodGet, "/api/me/pins", nil, other)
	if strings.Contains(theirs.Body.String(), roomID) {
		t.Error("one person's pinned conversation appeared in another's list")
	}
}

// TestCaptioningSomebodyElsesUploadIsRefused keeps a caption the uploader's.
func TestCaptioningSomebodyElsesUploadIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	attachmentID := uploadText(t, handler, owner, roomID, "picha.txt", "habari")

	stranger := registerAndSignIn(t, handler, "amina")

	recorder := call(t, handler, http.MethodPut,
		"/api/attachments/"+attachmentID+"/caption",
		map[string]any{"caption": "si yangu"}, stranger)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("captioning somebody else's upload returned %d, expected 404", recorder.Code)
	}
}

/*
TestCaptionsArePerAttachment is the reason a caption is not part of the message
body.

Sending several pictures with a line about each is the ordinary case, and one
body cannot say which line describes which picture.
*/
func TestCaptionsArePerAttachment(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	first := uploadText(t, handler, owner, roomID, "moja.txt", "ya kwanza")
	second := uploadText(t, handler, owner, roomID, "mbili.txt", "ya pili")

	for id, caption := range map[string]string{
		first:  "Picha ya kwanza",
		second: "Picha ya pili",
	} {
		recorder := call(t, handler, http.MethodPut,
			"/api/attachments/"+id+"/caption",
			map[string]any{"caption": caption}, owner)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("captioning returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
		}
	}

	sent := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "Picha mbili", "attachmentIds": []string{first, second}}, owner)
	if sent.Code != http.StatusCreated {
		t.Fatalf("sending with attachments returned %d: %s", sent.Code, truncate(sent.Body.String()))
	}

	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, owner)
	body := timeline.Body.String()

	for _, caption := range []string{"Picha ya kwanza", "Picha ya pili"} {
		if !strings.Contains(body, caption) {
			t.Errorf("the caption %q is not carried with its attachment: %s", caption, truncate(body))
		}
	}
}

/*
TestTheReportedPartyNeverLearnsWhoReportedThem is the rule 7.9 turns on.

Somebody who learns who reported them retaliates against that person, which
teaches everybody else not to report. The reporter's identity appears only in
the moderation queue, behind a permission — and nowhere the reported party can
reach.
*/
func TestTheReportedPartyNeverLearnsWhoReportedThem(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	reporter := registerAndSignIn(t, handler, "mtoa-taarifa")
	joinRoom(t, handler, reporter, roomID)

	reported := registerAndSignIn(t, handler, "mshtakiwa")
	joinRoom(t, handler, reported, roomID)

	eventID := sendMessage(t, handler, reported, roomID, "Maneno yasiyofaa")

	filed := call(t, handler, http.MethodPost, "/api/reports", map[string]any{
		"room":    roomID,
		"eventId": eventID,
		"reason":  "harassment",
		"detail":  "Ameniudhi mara kwa mara",
	}, reporter)
	if filed.Code != http.StatusCreated {
		t.Fatalf("filing returned %d: %s", filed.Code, truncate(filed.Body.String()))
	}

	// The reported party cannot read the queue at all.
	queue := call(t, handler, http.MethodGet, "/api/reports", nil, reported)
	if queue.Code == http.StatusOK {
		t.Fatal("the reported party could read the moderation queue")
	}

	// Nor can the reporter, who has no more authority than anybody else —
	// which is what stops the queue being a way to see who else has reported.
	reporterQueue := call(t, handler, http.MethodGet, "/api/reports", nil, reporter)
	if reporterQueue.Code == http.StatusOK {
		t.Error("the reporter could read the moderation queue")
	}

	// And the filing response itself names nobody: it is returned to the
	// reporter, who knows who they are, but it must not carry their identity
	// into anything that might be shown elsewhere.
	if strings.Contains(filed.Body.String(), "mtoa-taarifa") {
		t.Errorf("the filing response carried the reporter's identity: %s",
			truncate(filed.Body.String()))
	}

	// A moderator does see it, which is the whole point of recording it.
	moderatorView := call(t, handler, http.MethodGet, "/api/reports", nil, owner)
	if moderatorView.Code != http.StatusOK {
		t.Fatalf("a moderator could not read the queue: %d", moderatorView.Code)
	}
	if !strings.Contains(moderatorView.Body.String(), "mtoa-taarifa") {
		t.Error("a moderator cannot see who reported, which is what makes a " +
			"report actionable")
	}
}

/*
TestReportingIsRateLimited stops one person burying the queue.

Reporting has to be easy, so the allowance is deliberately generous — far above
what anybody encountering real problems files, and low enough that a script
cannot make the queue unusable.
*/
func TestReportingIsRateLimited(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	reporter := registerAndSignIn(t, handler, "mkorofi")
	joinRoom(t, handler, reporter, roomID)

	refused := false
	for range 30 {
		recorder := call(t, handler, http.MethodPost, "/api/reports", map[string]any{
			"room":    roomID,
			"subject": "founder@tdc.test",
			"reason":  "spam",
		}, reporter)

		if recorder.Code == http.StatusTooManyRequests {
			refused = true
			break
		}
	}

	if !refused {
		t.Error("thirty reports from one person were all accepted; the queue " +
			"can be buried")
	}
}

/*
TestResolvingRecordsTheOutcome is what makes a queue a queue rather than a
list.

A report nobody can mark as handled is a report every moderator looks at again.
Resolving records what was decided and takes it out of the queue.
*/
func TestResolvingRecordsTheOutcome(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	reporter := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, reporter, roomID)

	call(t, handler, http.MethodPost, "/api/reports", map[string]any{
		"room":    roomID,
		"subject": "founder@tdc.test",
		"reason":  "spam",
	}, reporter)

	open := decodeReports(t, call(t, handler, http.MethodGet, "/api/reports", nil, owner))
	if len(open) != 1 {
		t.Fatalf("expected one open report, got %d", len(open))
	}

	resolved := call(t, handler, http.MethodPost,
		"/api/reports/"+open[0].ID+"/resolve",
		map[string]any{"resolution": "Nimezungumza naye"}, owner)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolving returned %d: %s", resolved.Code, truncate(resolved.Body.String()))
	}

	// Out of the queue.
	remaining := decodeReports(t, call(t, handler, http.MethodGet, "/api/reports", nil, owner))
	if len(remaining) != 0 {
		t.Errorf("%d reports remain in the queue after resolving", len(remaining))
	}

	// And resolving it again is refused, so two moderators acting at once do
	// not both record an outcome.
	twice := call(t, handler, http.MethodPost,
		"/api/reports/"+open[0].ID+"/resolve",
		map[string]any{"resolution": "Na mimi pia"}, owner)
	if twice.Code == http.StatusOK {
		t.Error("a report was resolved twice; two moderators could both record " +
			"an outcome for the same thing")
	}
}

type queuedReport struct {
	ID           string `json:"id"`
	Reason       string `json:"reason"`
	Reporter     string `json:"reporter"`
	Subject      string `json:"subject"`
	ReportedBody string `json:"reportedBody"`
}

func decodeReports(t *testing.T, recorder *httptest.ResponseRecorder) []queuedReport {
	t.Helper()

	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the queue returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	var reports []queuedReport
	if err := json.Unmarshal(recorder.Body.Bytes(), &reports); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return reports
}
