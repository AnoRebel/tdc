package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

/*
A reply in a thread is a message, and these hold it to that: whatever a message
in the channel can carry, be refused for, or set off, a reply can too.
*/

// replyIn posts a reply and returns the recorder, whatever it said.
func replyIn(t *testing.T, handler http.Handler, token, roomID, rootID string, body map[string]any) map[string]any {
	t.Helper()
	return decode(t, mustCall(t, handler, http.MethodPost,
		"/api/rooms/"+roomID+"/threads/"+rootID+"/replies", body, token, http.StatusCreated))
}

// threadOf reads a thread.
func threadOf(t *testing.T, handler http.Handler, token, roomID, eventID string) map[string]any {
	t.Helper()
	return decode(t, mustCall(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/threads/"+eventID, nil, token, http.StatusOK))
}

// reasonsFor lists the reasons of somebody's notifications, ungrouped.
func reasonsFor(t *testing.T, handler http.Handler, token string) []string {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/me/notifications?grouped=false", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("listing notifications returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}
	var hits []struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decoding notifications: %v", err)
	}
	reasons := make([]string, 0, len(hits))
	for _, hit := range hits {
		reasons = append(reasons, hit.Reason)
	}
	return reasons
}

func count(values []string, want string) int {
	n := 0
	for _, value := range values {
		if value == want {
			n++
		}
	}
	return n
}

func TestAReplyCarriesFiles(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "share the screenshot?")

	uploaded := decode(t, uploadFile(t, handler, owner, roomID, "screenshot.png", testPNG))
	fileID, _ := uploaded["id"].(string)

	reply := replyIn(t, handler, owner, roomID, rootID,
		map[string]any{"body": "here", "attachmentIds": []string{fileID}})
	if files, _ := reply["attachments"].([]any); len(files) != 1 {
		t.Fatalf("the reply came back with %d files, want 1", len(files))
	}
	if reply["threadRoot"] != rootID {
		t.Errorf("the reply's threadRoot is %v, want %s", reply["threadRoot"], rootID)
	}

	replies, _ := threadOf(t, handler, owner, roomID, rootID)["replies"].([]any)
	first, _ := replies[0].(map[string]any)
	if files, _ := first["attachments"].([]any); len(files) != 1 {
		t.Errorf("the thread shows the reply with %d files, want 1", len(files))
	}

	// A file that is not there is refused before anything is posted.
	missing := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/threads/"+rootID+"/replies",
		map[string]any{"body": "and this", "attachmentIds": []string{uuid.NewString()}}, owner)
	if missing.Code == http.StatusCreated {
		t.Fatal("a reply naming a file that does not exist was posted")
	}
	if replies, _ := threadOf(t, handler, owner, roomID, rootID)["replies"].([]any); len(replies) != 1 {
		t.Errorf("the thread has %d replies after a refusal, want 1", len(replies))
	}

	// Nor can a reply carry somebody else's upload: it is refused, as a
	// message in the channel naming one is, rather than posted without it.
	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)
	theirs := decode(t, uploadFile(t, handler, other, roomID, "theirs.png", testPNG))
	theirID, _ := theirs["id"].(string)
	stolen := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/threads/"+rootID+"/replies",
		map[string]any{"body": "mine now", "attachmentIds": []string{theirID}}, owner)
	if stolen.Code == http.StatusCreated {
		t.Error("a reply carrying another person's upload was posted")
	}
}

func TestAReplyMayCarryButtonsAndIsRefusedForBadOnes(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "deploy?")

	refused := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/threads/"+rootID+"/replies",
		map[string]any{"body": "Run this", "actions": []map[string]any{
			{"name": "run", "label": "Run it", "capability": "execute"},
		}}, owner)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("a reply asking clients to execute something returned %d", refused.Code)
	}

	reply := replyIn(t, handler, owner, roomID, rootID, map[string]any{
		"body":    "Approve?",
		"actions": []map[string]any{{"name": "approve", "label": "Approve"}},
	})
	content, _ := reply["content"].(map[string]any)
	if offered, _ := content["actions"].([]any); len(offered) != 1 {
		t.Errorf("the reply carries %d buttons, want 1", len(offered))
	}
}

func TestABannedPersonCannotReply(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "question")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/bans",
		map[string]any{"user": userIDOf(t, handler, owner, "amina"), "reason": "Kuvuruga"},
		owner, http.StatusNoContent)

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/threads/"+rootID+"/replies",
		map[string]any{"body": "still here"}, member)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("a banned person's reply returned %d, want 403", recorder.Code)
	}
}

func TestARepliesAreFoundBySearch(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "who has the slides?")
	reply := replyIn(t, handler, owner, roomID, rootID, map[string]any{"body": "Mwanaisha ana slaidi"})

	hits := searchFor(t, handler, owner, "slaidi")
	if len(hits) != 1 || hits[0].EventID != reply["id"] {
		t.Fatalf("searching found %v, want the reply", hits)
	}

	// And the thread opens from it: asking for the reply names its thread.
	thread := threadOf(t, handler, owner, roomID, hits[0].EventID)
	root, _ := thread["root"].(map[string]any)
	if root["id"] != rootID {
		t.Errorf("opening a thread by its reply gave root %v, want %s", root["id"], rootID)
	}
}

// Followers hear about replies; somebody who only asked to hear everything in
// the channel does not, and somebody who stopped following stops hearing.
func TestRepliesReachFollowersAndTheMentionedOnly(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "Nani anaweza kusaidia?")

	helper := registerAndSignIn(t, handler, "amina")
	everything := registerAndSignIn(t, handler, "juma")
	named := registerAndSignIn(t, handler, "bakari")
	for _, token := range []string{helper, everything, named} {
		joinRoom(t, handler, token, roomID)
	}
	mustCall(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "all", "muted": false, "room": roomID}, everything, http.StatusNoContent)

	replyIn(t, handler, helper, roomID, rootID, map[string]any{"body": "Mimi ninaweza"})
	if got := count(reasonsFor(t, handler, owner), "reply"); got != 1 {
		t.Fatalf("the thread's author has %d reply notifications, want 1", got)
	}

	replyIn(t, handler, owner, roomID, rootID, map[string]any{"body": "Asante, na @bakari pia"})
	if got := count(reasonsFor(t, handler, helper), "reply"); got != 1 {
		t.Errorf("a follower has %d reply notifications, want 1", got)
	}
	if got := count(reasonsFor(t, handler, named), "mention"); got != 1 {
		t.Errorf("the person named has %d mentions, want 1", got)
	}
	if got := reasonsFor(t, handler, everything); len(got) != 0 {
		t.Errorf("somebody who never took part was notified: %v", got)
	}

	// Stopping following stops it, and replying again does not undo that.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+roomID+"/threads/"+rootID+"/follow",
		nil, helper, http.StatusNoContent)
	if thread := threadOf(t, handler, helper, roomID, rootID); thread["following"] != false {
		t.Errorf("following = %v after unfollowing", thread["following"])
	}
	replyIn(t, handler, helper, roomID, rootID, map[string]any{"body": "one more thing"})
	replyIn(t, handler, owner, roomID, rootID, map[string]any{"body": "noted"})
	if got := count(reasonsFor(t, handler, helper), "reply"); got != 1 {
		t.Errorf("an unfollower has %d reply notifications, want still 1", got)
	}

	// Following again is theirs to choose.
	mustCall(t, handler, http.MethodPut, "/api/rooms/"+roomID+"/threads/"+rootID+"/follow",
		nil, helper, http.StatusNoContent)
	if thread := threadOf(t, handler, helper, roomID, rootID); thread["following"] != true {
		t.Errorf("following = %v after following", thread["following"])
	}
}

// An edit or deletion of a reply belongs to its thread, not the channel.
func TestChangesToAReplyStayInItsThread(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "question")
	reply := replyIn(t, handler, owner, roomID, rootID, map[string]any{"body": "answr"})
	replyID, _ := reply["id"].(string)

	mustCall(t, handler, http.MethodPatch, "/api/rooms/"+roomID+"/messages/"+replyID,
		map[string]any{"body": "answer"}, owner, http.StatusCreated)

	types := timelineTypes(t, handler, owner, roomID)
	if hasType(types, "message.edited") {
		t.Error("an edit of a reply appeared in the channel timeline")
	}

	replies, _ := threadOf(t, handler, owner, roomID, rootID)["replies"].([]any)
	edited := false
	for _, raw := range replies {
		event, _ := raw.(map[string]any)
		if event["type"] == "message.edited" {
			edited = true
		}
	}
	if !edited {
		t.Error("the thread does not carry the edit of its reply")
	}

	// And the edit is not counted as a reply.
	summaries := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/threads",
		nil, owner, http.StatusOK))
	summary, _ := summaries[rootID].(map[string]any)
	if summary["replyCount"] != float64(1) {
		t.Errorf("replyCount = %v with one reply and one edit, want 1", summary["replyCount"])
	}
}

// A followed thread counts what its reader has not read; reading clears it,
// and a thread the reader does not follow counts nothing.
func TestThreadUnreadFollowsTheReadPosition(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "question")

	other := registerAndSignIn(t, handler, "amina")
	bystander := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, other, roomID)
	joinRoom(t, handler, bystander, roomID)

	var latest float64
	for range 2 {
		reply := replyIn(t, handler, other, roomID, rootID, map[string]any{"body": "an answer"})
		latest, _ = reply["sequence"].(float64)
	}

	unreadFor := func(token string) any {
		summaries := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/threads",
			nil, token, http.StatusOK))
		summary, _ := summaries[rootID].(map[string]any)
		return summary["unread"]
	}

	if got := unreadFor(owner); got != float64(2) {
		t.Fatalf("the thread's author has %v unread, want 2", got)
	}
	if got := unreadFor(other); got != float64(0) {
		t.Errorf("the replier has %v unread of their own replies, want 0", got)
	}
	if got := unreadFor(bystander); got != float64(0) {
		t.Errorf("somebody not following has %v unread, want 0", got)
	}

	// Replies are not the channel's unread.
	unread := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/unread", nil, owner, http.StatusOK))
	if count, _ := unread[roomID].(float64); count != 0 {
		t.Errorf("the channel counts %v unread with only replies after it, want 0", count)
	}

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/threads/"+rootID+"/read",
		map[string]any{"sequence": latest}, owner, http.StatusNoContent)
	if got := unreadFor(owner); got != float64(0) {
		t.Errorf("%v unread after reading, want 0", got)
	}
	if thread := threadOf(t, handler, owner, roomID, rootID); thread["readSequence"] != latest {
		t.Errorf("readSequence = %v, want %v", thread["readSequence"], latest)
	}

	// Never backwards.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/threads/"+rootID+"/read",
		map[string]any{"sequence": 1}, owner, http.StatusNoContent)
	if got := unreadFor(owner); got != float64(0) {
		t.Errorf("an older position resurfaced %v unread", got)
	}

	// Outsiders cannot mark or follow.
	outsider := registerAndSignIn(t, handler, "nje")
	if recorder := call(t, handler, http.MethodPut, "/api/rooms/"+roomID+"/threads/"+rootID+"/follow",
		nil, outsider); recorder.Code == http.StatusNoContent {
		t.Error("somebody outside the channel followed one of its threads")
	}
}

func TestReactionsOnAThread(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")
	rootID := sendMessage(t, handler, owner, roomID, "question")
	reply := replyIn(t, handler, owner, roomID, rootID, map[string]any{"body": "answer"})
	replyID, _ := reply["id"].(string)

	mustCall(t, handler, http.MethodPut, "/api/rooms/"+roomID+"/messages/"+replyID+"/reactions",
		map[string]any{"emoji": "👍"}, owner, http.StatusNoContent)

	channel := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/reactions",
		nil, owner, http.StatusOK))
	if _, ok := channel[replyID]; ok {
		t.Error("a reply's reactions came with the channel's")
	}

	thread := decode(t, mustCall(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/reactions?thread="+rootID, nil, owner, http.StatusOK))
	if reactions, _ := thread[replyID].([]any); len(reactions) != 1 {
		t.Errorf("the thread's reactions for the reply are %v, want one", thread[replyID])
	}
}

func TestTypingInAThreadIsSaidToBeThere(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	other := registerAndSignIn(t, handler, "amina")
	roomID := makeRoom(t, handler, owner, "general", "public")
	joinRoom(t, handler, other, roomID)
	rootID := sendMessage(t, handler, owner, roomID, "question")

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/typing",
		map[string]any{"threadRoot": rootID}, owner, http.StatusNoContent)

	var typing []map[string]any
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/typing", nil, other)
	if err := json.Unmarshal(recorder.Body.Bytes(), &typing); err != nil {
		t.Fatalf("decode typing: %v", err)
	}
	if len(typing) != 1 || typing[0]["threadRoot"] != rootID {
		t.Fatalf("typing = %v, want the owner in the thread", typing)
	}

	// Somewhere that is not a message here is refused.
	if refused := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/typing",
		map[string]any{"threadRoot": "$nothing"}, owner); refused.Code == http.StatusNoContent {
		t.Error("typing in a thread that does not exist was accepted")
	}
}
