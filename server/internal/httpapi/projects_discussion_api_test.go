package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// unreadNotificationsOf lists what somebody has been told about, one per message.
func unreadNotificationsOf(t *testing.T, handler http.Handler, token string) []map[string]any {
	t.Helper()
	return decodeRows(t, mustCall(t, handler, http.MethodGet,
		"/api/me/notifications?grouped=false", nil, token, http.StatusOK).Body.Bytes())
}

// toldAbout reports whether somebody was notified about a message, and why.
func toldAbout(t *testing.T, handler http.Handler, token, eventID string) (string, bool) {
	t.Helper()
	for _, notification := range unreadNotificationsOf(t, handler, token) {
		if notification["eventId"] == eventID {
			return notification["reason"].(string), true
		}
	}
	return "", false
}

// comment posts in an issue's discussion, joining it first as a person using
// the page would.
func comment(t *testing.T, handler http.Handler, token, issueID, body string) string {
	t.Helper()
	call(t, handler, http.MethodPost, "/api/rooms/"+issueID+"/join", nil, token)
	posted := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+issueID+"/messages",
		map[string]any{"body": body}, token, http.StatusCreated))
	return posted["id"].(string)
}

/*
A comment is a channel message in every mechanical sense: joined, posted,
edited and deleted through the room endpoints, held to the same length limit
and the same attachment rules — and unreachable to anybody who may not see the
issue, through every one of those endpoints.
*/
func TestAnIssueCommentIsAChannelMessage(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")
	outsider := registerMember(t, handler, "outsider")

	startProject(t, handler, founder, "PUB", "public")
	public := fileIssue(t, handler, founder, "PUB", "Talk about me")
	room := public["id"].(string)

	// Anybody who may see a public project's issue may join its discussion,
	// as anybody may join a public channel.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, amina, http.StatusOK)
	posted := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "I can reproduce this"}, amina, http.StatusCreated))
	id := posted["id"].(string)

	mustCall(t, handler, http.MethodPatch, "/api/rooms/"+room+"/messages/"+id,
		map[string]any{"body": "I can reproduce this on Firefox"}, amina, http.StatusCreated)
	// Only its author may edit it, as in a channel.
	mustCall(t, handler, http.MethodPatch, "/api/rooms/"+room+"/messages/"+id,
		map[string]any{"body": "hijacked"}, founder, http.StatusForbidden)

	tooLong := strings.Repeat("x", 8001)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": tooLong}, amina, http.StatusBadRequest)

	if recorder := uploadFile(t, handler, amina, room, "notes.txt", []byte("steps to reproduce")); recorder.Code != http.StatusCreated {
		t.Errorf("a participant could not attach a file: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := uploadFile(t, handler, outsider, room, "notes.txt", []byte("x")); recorder.Code == http.StatusCreated {
		t.Error("somebody who never joined the discussion attached a file to it")
	}

	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/messages/"+id, nil, amina, http.StatusCreated)
	timeline := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+room+"/timeline",
		nil, founder, http.StatusOK).Body.Bytes())
	types := []string{}
	for _, event := range timeline {
		types = append(types, event["type"].(string))
	}
	if joined := strings.Join(types, ","); !strings.Contains(joined, "message.edited") ||
		!strings.Contains(joined, "message.deleted") {
		t.Errorf("the discussion's timeline does not record the edit and removal: %s", joined)
	}

	// A private project's discussion: nothing through any of these paths.
	startProject(t, handler, founder, "HUSH", "private")
	private := fileIssue(t, handler, founder, "HUSH", "Not for everybody")
	hidden := private["id"].(string)
	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/rooms/" + hidden},
		{http.MethodGet, "/api/rooms/" + hidden + "/timeline"},
		{http.MethodGet, "/api/rooms/" + hidden + "/members"},
		{http.MethodPost, "/api/rooms/" + hidden + "/join"},
		{http.MethodPost, "/api/rooms/" + hidden + "/messages"},
	} {
		if recorder := call(t, handler, probe.method, probe.path, map[string]any{"body": "hello"}, amina); recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s answered an outsider %d, want 404", probe.method, probe.path, recorder.Code)
		}
	}
}

/*
A mention in a comment reaches whoever it names, if they may see the issue,
according to their own preferences — the same as a mention in a channel. It
reaches nobody outside a private project, and does not tell them they were
named.
*/
func TestAMentionInACommentNotifiesByPreference(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	bola := registerMember(t, handler, "bola")
	quiet := registerMember(t, handler, "quiet")

	mustCall(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "none"}, quiet, http.StatusNoContent)

	startProject(t, handler, founder, "MENT", "public")
	issue := fileIssue(t, handler, founder, "MENT", "Who should look at this?")
	room := issue["id"].(string)

	event := comment(t, handler, founder, room, "@bola and @quiet, could you look?")

	if reason, told := toldAbout(t, handler, bola, event); !told || reason != "mention" {
		t.Errorf("bola, named and able to see the issue, was told %v (%q), want a mention", told, reason)
	}
	if _, told := toldAbout(t, handler, quiet, event); told {
		t.Error("somebody who asked to hear nothing was told about a mention")
	}

	// Outside a private project, a mention is nothing at all.
	startProject(t, handler, founder, "PRIV", "private")
	private := fileIssue(t, handler, founder, "PRIV", "Private matter")
	secret := comment(t, handler, founder, private["id"].(string), "@bola should not hear of this")
	if _, told := toldAbout(t, handler, bola, secret); told {
		t.Fatal("somebody outside a private project was told about a mention in it")
	}
	members := decodeRows(t, mustCall(t, handler, http.MethodGet,
		"/api/rooms/"+private["id"].(string)+"/members", nil, founder, http.StatusOK).Body.Bytes())
	for _, member := range members {
		if member["username"] == "bola" {
			t.Error("a mention admitted somebody into a private project's discussion")
		}
	}
}

/*
Taking part in a discussion is following it: the author hears every comment
on what they filed, and anybody who comments hears the replies — without
anybody having been named.
*/
func TestParticipantsHearEveryComment(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")

	startProject(t, handler, founder, "PART", "public")
	issue := fileIssue(t, handler, founder, "PART", "Follow me")
	room := issue["id"].(string)

	first := comment(t, handler, amina, room, "Is this still happening?")
	if _, told := toldAbout(t, handler, founder, first); !told {
		t.Error("the author was not told about a comment on their issue")
	}

	// Somebody who has asked to hear nothing anywhere is not made to by
	// commenting.
	quiet := registerMember(t, handler, "quiet")
	mustCall(t, handler, http.MethodPut, "/api/me/preferences/notifications",
		map[string]any{"level": "none"}, quiet, http.StatusNoContent)
	comment(t, handler, quiet, room, "Seen it too.")

	reply := comment(t, handler, founder, room, "Yes, since Tuesday.")
	if _, told := toldAbout(t, handler, amina, reply); !told {
		t.Error("somebody who commented was not told about the reply")
	}
	if _, told := toldAbout(t, handler, quiet, reply); told {
		t.Error("commenting overrode somebody's choice to hear nothing")
	}

	// And a channel is untouched: posting there follows nothing.
	channel := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms",
		map[string]any{"slug": "chatter"}, founder, http.StatusCreated))["id"].(string)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/join", nil, amina, http.StatusOK)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
		map[string]any{"body": "hello"}, amina, http.StatusCreated)
	chatter := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
		map[string]any{"body": "hi"}, founder, http.StatusCreated))["id"].(string)
	if _, told := toldAbout(t, handler, amina, chatter); told {
		t.Error("posting in an ordinary channel changed what somebody hears about it")
	}
}

/*
An issue made from a message refers to it rather than copying it, and the
message's room shows the issue — to people who may see the issue.
*/
func TestAnIssueMadeFromAMessage(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")

	channel := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms",
		map[string]any{"slug": "bugs"}, founder, http.StatusCreated))["id"].(string)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/join", nil, amina, http.StatusOK)
	message := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
		map[string]any{"body": "Sign-in fails on the second try\nIt says the code expired."}, amina,
		http.StatusCreated))["id"].(string)

	startProject(t, handler, founder, "AUTH", "public")
	issue := decode(t, mustCall(t, handler, http.MethodPost, "/api/projects/AUTH/issues", map[string]any{
		"title": "Sign-in fails on the second try", "originChannel": channel, "originEvent": message,
	}, founder, http.StatusCreated))

	origin := issue["origin"].(map[string]any)
	if origin["disclosed"] != true || origin["roomName"] != "bugs" || origin["eventId"] != message {
		t.Fatalf("a channel member sees the origin as %v", origin)
	}
	if body := origin["message"].(map[string]any)["body"]; !strings.Contains(body.(string), "code expired") {
		t.Errorf("the origin does not read as the message does: %v", body)
	}

	// Referred to, not copied: the issue's own record holds identifiers only.
	history := mustCall(t, handler, http.MethodGet, "/api/issues/AUTH-1/history", nil, founder, http.StatusOK)
	if strings.Contains(history.Body.String(), "code expired") {
		t.Error("the message's text was copied into the issue's history")
	}

	// An edit to the message shows through.
	mustCall(t, handler, http.MethodPatch, "/api/rooms/"+channel+"/messages/"+message,
		map[string]any{"body": "Sign-in fails on the second try, every time"}, amina, http.StatusCreated)
	again := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/AUTH-1", nil, amina, http.StatusOK))
	edited := again["origin"].(map[string]any)["message"].(map[string]any)
	if edited["edited"] != true || !strings.Contains(edited["body"].(string), "every time") {
		t.Errorf("an edited origin reads %v", edited)
	}

	// And the message's room says which issue it became.
	links := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+channel+"/issues",
		nil, amina, http.StatusOK).Body.Bytes())
	if len(links) != 1 || links[0]["eventId"] != message || links[0]["key"] != "AUTH-1" {
		t.Errorf("the room's issues are %v, want AUTH-1 against the message", links)
	}

	// Nobody may point at a message they cannot see: a private channel's
	// message answers exactly as one that does not exist.
	private := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms",
		map[string]any{"slug": "staff", "visibility": "private"}, founder, http.StatusCreated))["id"].(string)
	hidden := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+private+"/messages",
		map[string]any{"body": "internal"}, founder, http.StatusCreated))["id"].(string)
	mustCall(t, handler, http.MethodPut, "/api/projects/AUTH/members/amina",
		map[string]any{"role": "member"}, founder, http.StatusNoContent)
	unseen := call(t, handler, http.MethodPost, "/api/projects/AUTH/issues", map[string]any{
		"title": "x", "originChannel": private, "originEvent": hidden,
	}, amina)
	missing := call(t, handler, http.MethodPost, "/api/projects/AUTH/issues", map[string]any{
		"title": "x", "originChannel": private, "originEvent": "$nothing",
	}, amina)
	if unseen.Code != http.StatusNotFound || unseen.Body.String() != missing.Body.String() {
		t.Errorf("pointing at an unseen message answered %d %s, a missing one %s",
			unseen.Code, unseen.Body.String(), missing.Body.String())
	}
}

/*
Somebody outside the origin's channel sees that the issue came from a
conversation, and nothing else — and cannot tell a private origin from one
since deleted, because the two answers are the same bytes.
*/
func TestAnOriginDisclosesNothingOutsideItsChannel(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	outsider := registerMember(t, handler, "outsider")

	private := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms",
		map[string]any{"slug": "leadership", "visibility": "private"}, founder, http.StatusCreated))["id"].(string)
	kept := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+private+"/messages",
		map[string]any{"body": "Budget is cut; the rewrite waits"}, founder, http.StatusCreated))["id"].(string)
	removed := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+private+"/messages",
		map[string]any{"body": "Nobody outside should read this"}, founder, http.StatusCreated))["id"].(string)

	startProject(t, handler, founder, "OPEN", "public")
	for _, origin := range []string{kept, removed} {
		mustCall(t, handler, http.MethodPost, "/api/projects/OPEN/issues", map[string]any{
			"title": "Postpone the rewrite", "originChannel": private, "originEvent": origin,
		}, founder, http.StatusCreated)
	}
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+private+"/messages/"+removed, nil, founder, http.StatusCreated)

	originOf := func(ref string) string {
		issue := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/"+ref, nil, outsider, http.StatusOK))
		encoded, _ := json.Marshal(issue["origin"])
		return string(encoded)
	}
	present, deleted := originOf("OPEN-1"), originOf("OPEN-2")
	if present != `{"disclosed":false}` {
		t.Errorf("an outsider sees the origin as %s, want only that there was one", present)
	}
	if present != deleted {
		t.Errorf("a private origin and a deleted one are distinguishable from outside:\n  %s\n  %s", present, deleted)
	}
	for _, leaked := range []string{"leadership", "Budget", "founder"} {
		if strings.Contains(present, leaked) {
			t.Errorf("the undisclosed origin carries %q", leaked)
		}
	}

	// The member of the channel sees the deleted one as deleted.
	member := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/OPEN-2", nil, founder, http.StatusOK))
	if message := member["origin"].(map[string]any)["message"].(map[string]any); message["removed"] != true {
		t.Errorf("a channel member sees a deleted origin as %v", message)
	}

	// And the channel's own list of issues is no way in.
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+private+"/issues", nil, outsider, http.StatusNotFound)
}

// A room member outside a private project learns nothing from the room's list
// of issues about an issue made in that project.
func TestARoomDoesNotShowIssuesItsReaderCannotSee(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerMember(t, handler, "amina")

	channel := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms",
		map[string]any{"slug": "town-square"}, founder, http.StatusCreated))["id"].(string)
	message := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
		map[string]any{"body": "Somebody is probing the login"}, founder, http.StatusCreated))["id"].(string)

	startProject(t, handler, founder, "SEC", "private")
	mustCall(t, handler, http.MethodPost, "/api/projects/SEC/issues", map[string]any{
		"title": "Investigate login probing", "originChannel": channel, "originEvent": message,
	}, founder, http.StatusCreated)

	if links := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+channel+"/issues",
		nil, founder, http.StatusOK).Body.Bytes()); len(links) != 1 {
		t.Fatalf("the project's member sees %d issues from the room, want 1", len(links))
	}
	if links := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+channel+"/issues",
		nil, amina, http.StatusOK).Body.Bytes()); len(links) != 0 {
		t.Errorf("a room member outside the private project sees %v", links)
	}
}
