package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// markSensitive marks or unmarks an attachment.
func markSensitive(
	t *testing.T, handler http.Handler, token, attachmentID string, sensitive bool,
) *httptest.ResponseRecorder {
	t.Helper()

	return call(t, handler, http.MethodPut,
		"/api/attachments/"+attachmentID+"/sensitive",
		map[string]any{"sensitive": sensitive}, token)
}

// attachmentSensitivity reads how an attachment is marked.
func attachmentSensitivity(t *testing.T, recorder *httptest.ResponseRecorder) (bool, string) {
	t.Helper()

	if recorder.Code != http.StatusOK {
		t.Fatalf("marking returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var response struct {
		Sensitive       bool   `json:"sensitive"`
		SensitiveSource string `json:"sensitiveSource"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return response.Sensitive, response.SensitiveSource
}

/*
TestSomebodyCanMarkTheirOwnUpload is the scenario that must work with nothing
configured.

No classifier, no external service. The person who chose to send the file is
the person best placed to say it needs covering, and a server where that is the
only way to mark something still satisfies the requirement.
*/
func TestSomebodyCanMarkTheirOwnUpload(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	attachmentID := uploadText(t, handler, member, roomID, "picha.txt", "something")

	sensitive, source := attachmentSensitivity(t,
		markSensitive(t, handler, member, attachmentID, true))

	if !sensitive {
		t.Error("marking an own upload did not take effect")
	}
	if source != "uploader" {
		t.Errorf("marked by %q, expected uploader", source)
	}
}

// TestAMarkAppliesToEveryViewer covers the scenario's "for all viewers".
//
// A mark that only its author could see would be a private note, not a
// warning.
func TestAMarkAppliesToEveryViewer(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	attachmentID := uploadText(t, handler, member, roomID, "picha.txt", "something")
	markSensitive(t, handler, member, attachmentID, true)

	sent := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "angalia", "attachmentIds": []string{attachmentID}}, member)
	if sent.Code != http.StatusCreated {
		t.Fatalf("sending with the attachment returned %d: %s",
			sent.Code, truncate(sent.Body.String()))
	}

	// Somebody else reads the room and sees the mark.
	timeline := decodeTimeline(t, call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/timeline?limit=50", nil, owner))

	found := false
	for _, event := range timeline {
		attachments, _ := event["attachments"].([]any)
		for _, raw := range attachments {
			attachment, _ := raw.(map[string]any)
			if attachment["id"] != attachmentID {
				continue
			}
			found = true
			if sensitive, _ := attachment["sensitive"].(bool); !sensitive {
				t.Error("another viewer does not see the file as sensitive")
			}
		}
	}
	if !found {
		t.Fatal("the attachment was not in the timeline at all")
	}
}

/*
TestAModeratorsMarkCannotBeLiftedByTheUploader is what makes marking more than
advisory.

Otherwise the people most likely to post something unwelcome would be exactly
the people able to uncover it again.
*/
func TestAModeratorsMarkCannotBeLiftedByTheUploader(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	attachmentID := uploadText(t, handler, member, roomID, "picha.txt", "something")

	// The moderator marks it.
	_, source := attachmentSensitivity(t, markSensitive(t, handler, owner, attachmentID, true))
	if source != "moderator" {
		t.Fatalf("marked by %q, expected moderator", source)
	}

	// The uploader cannot lift it.
	lifted := markSensitive(t, handler, member, attachmentID, false)
	if lifted.Code == http.StatusOK {
		t.Error("the uploader lifted a moderator's mark")
	}

	// And a moderator can.
	sensitive, _ := attachmentSensitivity(t, markSensitive(t, handler, owner, attachmentID, false))
	if sensitive {
		t.Error("a moderator could not lift their own mark")
	}
}

// TestAnUploaderCanLiftTheirOwnMark covers changing your mind, which is the
// ordinary case and must not need a moderator.
func TestAnUploaderCanLiftTheirOwnMark(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	attachmentID := uploadText(t, handler, member, roomID, "picha.txt", "something")

	markSensitive(t, handler, member, attachmentID, true)

	sensitive, _ := attachmentSensitivity(t, markSensitive(t, handler, member, attachmentID, false))
	if sensitive {
		t.Error("somebody could not lift their own mark")
	}
}

/*
TestAModeratorsMarkOverridesAnUploaders is the direction that has to hold.

Marking your own file first, hoping that makes a moderator's decision a
duplicate they cannot strengthen, would be a way to keep control of it.
*/
func TestAModeratorsMarkOverridesAnUploaders(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	attachmentID := uploadText(t, handler, member, roomID, "picha.txt", "something")

	markSensitive(t, handler, member, attachmentID, true)
	_, source := attachmentSensitivity(t, markSensitive(t, handler, owner, attachmentID, true))

	if source != "moderator" {
		t.Errorf("after a moderator marked it the source is %q, expected moderator", source)
	}

	// And now the uploader cannot lift it.
	if lifted := markSensitive(t, handler, member, attachmentID, false); lifted.Code == http.StatusOK {
		t.Error("marking first let the uploader keep control of the mark")
	}
}

/*
TestAnUploaderCannotWeakenAModeratorsMark is the other order, and the one that
needs the guard.

When the moderator marks last, an unconditional write reaches the same answer —
so the test above passes even with the rule removed. This is the case that
fails without it: the uploader re-marks afterwards, and a write that did not
check would quietly downgrade the source to "uploader" and hand them back the
ability to lift it.
*/
func TestAnUploaderCannotWeakenAModeratorsMark(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	attachmentID := uploadText(t, handler, member, roomID, "picha.txt", "something")

	// The moderator marks first.
	markSensitive(t, handler, owner, attachmentID, true)

	// The uploader marks it again, which would be theirs if the write did not
	// check what is already there.
	_, source := attachmentSensitivity(t, markSensitive(t, handler, member, attachmentID, true))
	if source != "moderator" {
		t.Errorf("after the uploader re-marked it the source is %q, expected "+
			"the moderator's mark to stand", source)
	}

	// Which means they still cannot lift it.
	if lifted := markSensitive(t, handler, member, attachmentID, false); lifted.Code == http.StatusOK {
		t.Error("re-marking let the uploader take back control of a moderator's mark")
	}
}

// TestAStrangerCannotMarkSomebodyElsesUpload keeps this from being a way to
// deface a conversation.
func TestAStrangerCannotMarkSomebodyElsesUpload(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	uploader := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, uploader, roomID)

	stranger := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, stranger, roomID)

	attachmentID := uploadText(t, handler, uploader, roomID, "picha.txt", "something")

	if marked := markSensitive(t, handler, stranger, attachmentID, true); marked.Code == http.StatusOK {
		t.Error("somebody with no claim on the file marked it")
	}
}

/*
TestObscuringIsOnByDefault covers the operator policy.

Somebody who marked their upload meant it to be covered. A server that ignored
that until an operator found a setting would be one where the marking did
nothing.
*/
func TestObscuringIsOnByDefault(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading server info returned %d", recorder.Code)
	}

	var info struct {
		ObscureSensitiveMedia bool `json:"obscureSensitiveMedia"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if !info.ObscureSensitiveMedia {
		t.Error("a fresh server does not obscure media marked sensitive")
	}
}

// TestAnOperatorCanStopObscuring covers the server where everybody has agreed
// otherwise.
func TestAnOperatorCanStopObscuring(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "moderation.obscure_sensitive_media", "false")

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")
	var info struct {
		ObscureSensitiveMedia bool `json:"obscureSensitiveMedia"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if info.ObscureSensitiveMedia {
		t.Error("the policy did not change")
	}
}

/*
TestMarkingIsNotAccessControl states the limit of the feature plainly.

Obscuring is about not showing somebody something they did not ask to see. The
file stays fetchable by anybody entitled to read the room, and a test asserting
otherwise would be asserting security theatre over a URL the viewer already
has.
*/
func TestMarkingIsNotAccessControl(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	attachmentID := uploadText(t, handler, member, roomID, "picha.txt", "something")
	markSensitive(t, handler, member, attachmentID, true)

	fetched := call(t, handler, http.MethodGet, "/api/attachments/"+attachmentID, nil, owner)
	if fetched.Code == http.StatusForbidden || fetched.Code == http.StatusNotFound {
		t.Errorf("a marked file became unfetchable (%d); marking covers media, "+
			"it does not restrict who may read the room", fetched.Code)
	}
}
