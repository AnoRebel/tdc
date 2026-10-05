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
	"github.com/google/uuid"
)

/*
grantInChannel gives somebody a new role holding permissions in one channel
only, which is how an operator hands a channel to the people who look after it
without handing them the server.
*/
func grantInChannel(t *testing.T, handler http.Handler, admin, holder, roomID string, permissions ...string) {
	t.Helper()

	created := decode(t, mustCall(t, handler, http.MethodPost, "/api/admin/roles",
		map[string]string{"name": "keeper-" + uuid.NewString()[:8]}, admin, http.StatusCreated))
	roleID, _ := created["id"].(string)
	for _, permission := range permissions {
		mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+roleID+"/permissions",
			map[string]any{"permission": permission, "channel": roomID, "granted": true},
			admin, http.StatusNoContent)
	}
	mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+roleID+"/members",
		map[string]string{"user": accountID(t, handler, holder)}, admin, http.StatusNoContent)
}

// auditDetail finds the newest entry for an action about a target and returns
// its detail, failing when there is none.
func auditDetail(t *testing.T, handler http.Handler, admin, action, targetID string) map[string]any {
	t.Helper()

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=100&action="+action, nil, admin))
	for _, entry := range entries {
		if entry.Action == action && entry.TargetID == targetID {
			detail := map[string]any{}
			if entry.Detail != "" {
				if err := json.Unmarshal([]byte(entry.Detail), &detail); err != nil {
					t.Fatalf("decoding the detail of %s: %v", action, err)
				}
			}
			return detail
		}
	}
	t.Fatalf("no %s entry for %s in the audit log", action, targetID)
	return nil
}

// timelineTypes lists the types of the events in a room's timeline.
func timelineTypes(t *testing.T, handler http.Handler, token, roomID string) []string {
	t.Helper()

	var timeline []map[string]any
	recorder := mustCall(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token, http.StatusOK)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decoding the timeline: %v", err)
	}
	types := make([]string, 0, len(timeline))
	for _, event := range timeline {
		kind, _ := event["type"].(string)
		types = append(types, kind)
	}
	return types
}

func hasType(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

/*
Renaming a channel is channel.rename's, and only in the channel it was granted
for.

Before this the permission was in the catalogue and nothing asked for it: a
grant changed nothing and a withdrawal took nothing away.
*/
func TestRenamingAChannelNeedsChannelRename(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	other := makeRoom(t, handler, admin, "other", "public")

	keeper := registerAndSignIn(t, handler, "amina")
	member := registerAndSignIn(t, handler, "baraka")
	for _, person := range []string{keeper, member} {
		for _, room := range []string{general, other} {
			mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, person, http.StatusOK)
		}
	}
	grantInChannel(t, handler, admin, keeper, general, "channel.rename")

	rename := map[string]any{"name": "Karibu", "topic": "Where everybody starts"}
	if code := call(t, handler, http.MethodPatch, "/api/rooms/"+general, rename, member).Code; code != http.StatusForbidden {
		t.Errorf("a member without channel.rename renamed the channel: status %d, want 403", code)
	}
	if code := call(t, handler, http.MethodPatch, "/api/rooms/"+other, rename, keeper).Code; code != http.StatusForbidden {
		t.Errorf("a grant for one channel renamed another: status %d, want 403", code)
	}

	renamed := decode(t, mustCall(t, handler, http.MethodPatch, "/api/rooms/"+general, rename, keeper, http.StatusOK))
	if renamed["name"] != "Karibu" || renamed["topic"] != "Where everybody starts" {
		t.Errorf("the channel reads %v after renaming", renamed)
	}

	// The topic alone, leaving the name as it is.
	retopic := decode(t, mustCall(t, handler, http.MethodPatch, "/api/rooms/"+general,
		map[string]any{"topic": ""}, keeper, http.StatusOK))
	if retopic["name"] != "Karibu" || retopic["topic"] != "" {
		t.Errorf("changing only the topic left %v", retopic)
	}

	if !hasType(timelineTypes(t, handler, member, general), "room.renamed") {
		t.Error("the rename is not in the channel's timeline")
	}
	// The newest entry is the topic's change, carrying both values of each.
	detail := auditDetail(t, handler, admin, "channel.renamed", general)
	if detail["previousTopic"] != "Where everybody starts" || detail["topic"] != "" ||
		detail["previousName"] != "Karibu" || detail["name"] != "Karibu" {
		t.Errorf("the audit entry says %v, want the old and new values", detail)
	}

	tooLong := strings.Repeat("a", 81)
	if code := call(t, handler, http.MethodPatch, "/api/rooms/"+general,
		map[string]any{"name": tooLong}, keeper).Code; code != http.StatusBadRequest {
		t.Errorf("an 81-character name: status %d, want 400", code)
	}
}

/*
Archiving is channel.archive's, stops posting, keeps history, and is undone by
restoring — which the same permission governs.
*/
func TestArchivingAChannelNeedsChannelArchiveAndIsReversible(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	keeper := registerAndSignIn(t, handler, "amina")
	member := registerAndSignIn(t, handler, "baraka")
	for _, person := range []string{keeper, member} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/join", nil, person, http.StatusOK)
	}
	grantInChannel(t, handler, admin, keeper, general, "channel.archive")

	mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "before the archive"}, member, http.StatusCreated)

	if code := call(t, handler, http.MethodPost, "/api/rooms/"+general+"/archive", nil, member).Code; code != http.StatusForbidden {
		t.Errorf("a member without channel.archive archived the channel: status %d, want 403", code)
	}

	archived := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/archive", nil, keeper, http.StatusOK))
	if archived["archived"] != true {
		t.Errorf("the channel is not reported archived: %v", archived)
	}
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "after"}, member).Code; code != http.StatusConflict {
		t.Errorf("posting into an archived channel: status %d, want 409", code)
	}
	if code := call(t, handler, http.MethodPatch, "/api/rooms/"+general,
		map[string]any{"name": "renamed"}, admin).Code; code != http.StatusConflict {
		t.Errorf("renaming an archived channel: status %d, want 409", code)
	}
	// Archiving twice is not an error: two moderators asked for one thing.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/archive", nil, keeper, http.StatusOK)

	if code := call(t, handler, http.MethodDelete, "/api/rooms/"+general+"/archive", nil, member).Code; code != http.StatusForbidden {
		t.Errorf("a member without channel.archive restored the channel: status %d, want 403", code)
	}
	restored := decode(t, mustCall(t, handler, http.MethodDelete, "/api/rooms/"+general+"/archive", nil, keeper, http.StatusOK))
	if restored["archived"] != false {
		t.Errorf("the channel is still reported archived: %v", restored)
	}
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "after restoring"}, member, http.StatusCreated)

	types := timelineTypes(t, handler, member, general)
	for _, want := range []string{"room.archived", "room.restored", "message.sent"} {
		if !hasType(types, want) {
			t.Errorf("the timeline has no %s: %v", want, types)
		}
	}
	auditDetail(t, handler, admin, "channel.archived", general)
	auditDetail(t, handler, admin, "channel.restored", general)
}

/*
Visibility is channel.rename's — and making a channel public, which opens its
history to the whole server, needs that permission for the whole server.

Recorded in the audit log with both values, as the specification asks.
*/
func TestChangingVisibilityIsGovernedAndAudited(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	keeper := registerAndSignIn(t, handler, "amina")
	member := registerAndSignIn(t, handler, "baraka")
	outsider := registerAndSignIn(t, handler, "chausiku")
	for _, person := range []string{keeper, member} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/join", nil, person, http.StatusOK)
	}
	grantInChannel(t, handler, admin, keeper, general, "channel.rename")

	private := map[string]any{"visibility": "private"}
	public := map[string]any{"visibility": "public"}

	if code := call(t, handler, http.MethodPut, "/api/rooms/"+general+"/visibility", private, member).Code; code != http.StatusForbidden {
		t.Errorf("a member without channel.rename made the channel private: status %d, want 403", code)
	}

	made := decode(t, mustCall(t, handler, http.MethodPut, "/api/rooms/"+general+"/visibility", private, keeper, http.StatusOK))
	if made["visibility"] != "private" {
		t.Errorf("the channel reads %v", made["visibility"])
	}
	detail := auditDetail(t, handler, admin, "channel.visibility.changed", general)
	if detail["from"] != "public" || detail["to"] != "private" {
		t.Errorf("the audit entry says %v, want from public to private", detail)
	}

	// Gone from sight for anybody not in it, as any private channel is.
	if code := call(t, handler, http.MethodGet, "/api/rooms/"+general, nil, outsider).Code; code != http.StatusNotFound {
		t.Errorf("an outsider can still see the channel made private: status %d, want 404", code)
	}

	// A grant for this channel does not reach the whole server's history.
	if code := call(t, handler, http.MethodPut, "/api/rooms/"+general+"/visibility", public, keeper).Code; code != http.StatusForbidden {
		t.Errorf("a channel-scoped grant made the channel public: status %d, want 403", code)
	}

	if code := call(t, handler, http.MethodPut, "/api/rooms/"+general+"/visibility",
		map[string]any{"visibility": "secret"}, admin).Code; code != http.StatusBadRequest {
		t.Errorf("an unknown visibility: status %d, want 400", code)
	}

	mustCall(t, handler, http.MethodPut, "/api/rooms/"+general+"/visibility", public, admin, http.StatusOK)
	mustCall(t, handler, http.MethodGet, "/api/rooms/"+general, nil, outsider, http.StatusOK)

	if !hasType(timelineTypes(t, handler, member, general), "room.visibility_changed") {
		t.Error("the change is not in the channel's timeline")
	}
}

/*
Removing somebody is channel.member.remove's, takes effect at once, and is not a
ban: they may come straight back to a public channel.
*/
func TestRemovingSomebodyFromAChannel(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	keeper := registerAndSignIn(t, handler, "amina")
	member := registerAndSignIn(t, handler, "baraka")
	stranger := registerAndSignIn(t, handler, "chausiku")
	for _, person := range []string{keeper, member} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/join", nil, person, http.StatusOK)
	}
	grantInChannel(t, handler, admin, keeper, general, "channel.member.remove")

	memberID := accountID(t, handler, member)
	keeperID := accountID(t, handler, keeper)

	if code := call(t, handler, http.MethodDelete, "/api/rooms/"+general+"/members/"+keeperID, nil, member).Code; code != http.StatusForbidden {
		t.Errorf("a member without channel.member.remove removed somebody: status %d, want 403", code)
	}
	if code := call(t, handler, http.MethodDelete, "/api/rooms/"+general+"/members/"+keeperID, nil, keeper).Code; code != http.StatusBadRequest {
		t.Errorf("removing oneself: status %d, want 400", code)
	}
	if code := call(t, handler, http.MethodDelete, "/api/rooms/"+general+"/members/"+accountID(t, handler, stranger), nil, keeper).Code; code != http.StatusNotFound {
		t.Errorf("removing somebody not in the channel: status %d, want 404", code)
	}

	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+general+"/members/"+memberID, nil, keeper, http.StatusNoContent)

	if room := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+general, nil, member, http.StatusOK)); room["member"] != false {
		t.Error("the person removed is still reported as a member")
	}
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "still here?"}, member).Code; code != http.StatusForbidden {
		t.Errorf("the person removed could still post: status %d, want 403", code)
	}
	if !hasType(timelineTypes(t, handler, keeper, general), "member.removed") {
		t.Error("the removal is not in the channel's timeline")
	}
	if detail := auditDetail(t, handler, admin, "channel.member.removed", general); detail["user"] != memberID {
		t.Errorf("the audit entry names %v, want %s", detail["user"], memberID)
	}

	// Not a ban: a public channel takes them back.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/join", nil, member, http.StatusOK)
}

// Somebody removed from a private channel cannot come back on their own, and
// the channel is gone from their sight.
func TestRemovalFromAPrivateChannelCloses(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	team := makeRoom(t, handler, admin, "team", "private")
	member := registerAndSignIn(t, handler, "baraka")
	issued := issueInvitation(t, handler, admin, team, map[string]any{})
	if code := redeem(t, handler, member, map[string]any{"token": issued.Token}); code != http.StatusOK {
		t.Fatalf("accepting the invitation: status %d", code)
	}

	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+team+"/members/"+accountID(t, handler, member), nil, admin, http.StatusNoContent)

	if code := call(t, handler, http.MethodGet, "/api/rooms/"+team+"/timeline", nil, member).Code; code != http.StatusNotFound && code != http.StatusForbidden {
		t.Errorf("the person removed could still read the private channel: status %d", code)
	}
	if code := call(t, handler, http.MethodPost, "/api/rooms/"+team+"/join", nil, member).Code; code == http.StatusOK {
		t.Error("the person removed rejoined a private channel without an invitation")
	}
}

/*
The person removed hears it at once, on the connection they have open — and
then stops receiving the channel without reconnecting.

Membership is read at delivery, which is what cuts them off; it is also what
would keep them from being told, since the removal is delivered after they are
no longer a member.
*/
func TestTheRemovedPersonIsToldLive(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	member := registerAndSignIn(t, handler, "baraka")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/join", nil, member, http.StatusOK)
	memberID := accountID(t, handler, member)

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + member}}})
	if err != nil {
		t.Fatalf("opening the live connection: %v", err)
	}
	defer func() { _ = socket.Close(websocket.StatusNormalClosure, "") }()

	removed := false
	for !removed {
		_, data, err := socket.Read(ctx)
		if err != nil {
			t.Fatalf("the connection ended before the removal arrived: %v", err)
		}
		var frame map[string]any
		if json.Unmarshal(data, &frame) != nil {
			continue
		}

		switch frame["type"] {
		case "ready":
			request, _ := json.Marshal(map[string]any{"action": "subscribe", "rooms": []string{general}})
			if err := socket.Write(ctx, websocket.MessageText, request); err != nil {
				t.Fatalf("subscribing: %v", err)
			}
		case "subscribed":
			mustCall(t, handler, http.MethodDelete, "/api/rooms/"+general+"/members/"+memberID,
				nil, admin, http.StatusNoContent)
		case "event":
			payload, _ := frame["payload"].(map[string]any)
			if payload["type"] == "member.removed" {
				removed = true
			}
		}
	}

	// Anything said afterwards does not reach them.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "after the removal"}, admin, http.StatusCreated)

	quiet, stop := context.WithTimeout(ctx, 750*time.Millisecond)
	defer stop()
	for {
		_, data, err := socket.Read(quiet)
		if err != nil {
			return
		}
		var frame map[string]any
		if json.Unmarshal(data, &frame) == nil && frame["type"] == "event" {
			t.Fatalf("the person removed still received the channel: %s", data)
		}
	}
}

/*
A direct conversation is not a channel, and holding every permission does not
make it one: nobody renames, archives or removes somebody from a conversation
between two people.
*/
func TestADirectConversationIsNotAdministered(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	other := registerAndSignIn(t, handler, "baraka")

	direct := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{accountID(t, handler, other)}}, admin, http.StatusOK))
	roomID, _ := direct["id"].(string)

	for _, attempt := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/api/rooms/" + roomID, map[string]any{"name": "ours"}},
		{http.MethodPut, "/api/rooms/" + roomID + "/visibility", map[string]any{"visibility": "public"}},
		{http.MethodPost, "/api/rooms/" + roomID + "/archive", nil},
		{http.MethodDelete, "/api/rooms/" + roomID + "/members/" + accountID(t, handler, other), nil},
	} {
		if code := call(t, handler, attempt.method, attempt.path, attempt.body, admin).Code; code != http.StatusConflict {
			t.Errorf("%s %s on a direct conversation: status %d, want 409", attempt.method, attempt.path, code)
		}
	}

	answer := decode(t, mustCall(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/administration", nil, admin, http.StatusOK))
	if answer["administrable"] != false || answer["rename"] == true {
		t.Errorf("a direct conversation is described as administrable: %v", answer)
	}
}

// The client is told what it may offer, including grants made for this
// channel alone, which the server-wide permission list does not carry.
func TestTheAdministrationAnswerFollowsTheGrants(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	keeper := registerAndSignIn(t, handler, "amina")
	member := registerAndSignIn(t, handler, "baraka")
	outsider := registerAndSignIn(t, handler, "chausiku")
	for _, person := range []string{keeper, member} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/join", nil, person, http.StatusOK)
	}
	grantInChannel(t, handler, admin, keeper, general, "channel.rename", "channel.member.remove")

	path := "/api/rooms/" + general + "/administration"

	plain := decode(t, mustCall(t, handler, http.MethodGet, path, nil, member, http.StatusOK))
	for _, field := range []string{"rename", "archive", "makePrivate", "makePublic", "removeMembers"} {
		if plain[field] == true {
			t.Errorf("an ordinary member is offered %s", field)
		}
	}

	kept := decode(t, mustCall(t, handler, http.MethodGet, path, nil, keeper, http.StatusOK))
	if kept["rename"] != true || kept["removeMembers"] != true || kept["makePrivate"] != true {
		t.Errorf("the keeper is not offered what they were granted: %v", kept)
	}
	if kept["makePublic"] == true || kept["archive"] == true {
		t.Errorf("the keeper is offered more than they were granted: %v", kept)
	}

	full := decode(t, mustCall(t, handler, http.MethodGet, path, nil, admin, http.StatusOK))
	if full["makePublic"] != true || full["archive"] != true {
		t.Errorf("an administrator is not offered everything: %v", full)
	}

	// Somebody not in the channel is told nothing about it.
	if code := call(t, handler, http.MethodGet, path, nil, outsider).Code; code != http.StatusForbidden {
		t.Errorf("an outsider asking: status %d, want 403", code)
	}
}

/*
message.edit.any lets a moderator change somebody else's message, recorded as
their edit and in the audit log.

Before this it was catalogued and never asked for, so the specification's
"without moderation permission" had no permission behind it.
*/
func TestAModeratorEditsAnothersMessage(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	general := makeRoom(t, handler, admin, "general", "public")
	moderator := registerAndSignIn(t, handler, "amina")
	author := registerAndSignIn(t, handler, "baraka")
	bystander := registerAndSignIn(t, handler, "chausiku")
	for _, person := range []string{moderator, author, bystander} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/join", nil, person, http.StatusOK)
	}
	grantInChannel(t, handler, admin, moderator, general, "message.edit.any")

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "my phone number is 0700 000 000"}, author, http.StatusCreated))
	messageID, _ := sent["id"].(string)
	path := "/api/rooms/" + general + "/messages/" + messageID

	if code := call(t, handler, http.MethodPatch, path, map[string]any{"body": "mine now"}, bystander).Code; code != http.StatusForbidden {
		t.Errorf("somebody without message.edit.any edited another's message: status %d, want 403", code)
	}

	edit := decode(t, mustCall(t, handler, http.MethodPatch, path,
		map[string]any{"body": "[number removed by a moderator]"}, moderator, http.StatusCreated))
	if sender, _ := edit["sender"].(string); !strings.HasPrefix(sender, "amina@") {
		t.Errorf("the edit is attributed to %q, want the moderator", sender)
	}
	content, _ := edit["content"].(map[string]any)
	if content["byModerator"] != true || content["replaces"] != messageID {
		t.Errorf("the edit does not say it was a moderator's: %v", content)
	}
	auditDetail(t, handler, admin, "message.edited_by_moderator", messageID)

	// An author's own edit is ordinary use, and neither marked nor recorded.
	own := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+general+"/messages",
		map[string]any{"body": "typo"}, author, http.StatusCreated))
	ownID, _ := own["id"].(string)
	ownEdit := decode(t, mustCall(t, handler, http.MethodPatch, "/api/rooms/"+general+"/messages/"+ownID,
		map[string]any{"body": "typo fixed"}, author, http.StatusCreated))
	if ownContent, _ := ownEdit["content"].(map[string]any); ownContent["byModerator"] != nil {
		t.Errorf("an author's own edit is marked as a moderator's: %v", ownContent)
	}

	// A deleted message is not edited back into view.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+general+"/messages/"+ownID, nil, author, http.StatusCreated)
	if code := call(t, handler, http.MethodPatch, "/api/rooms/"+general+"/messages/"+ownID,
		map[string]any{"body": "back again"}, moderator).Code; code != http.StatusForbidden {
		t.Errorf("a moderator edited a deleted message: status %d, want 403", code)
	}
}
