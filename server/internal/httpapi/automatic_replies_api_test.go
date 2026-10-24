package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

/*
Automatic replies: what the server says on somebody's behalf while they are
away. Most of what is tested here is when it says nothing — that is where the
feature is easy to get wrong and where getting it wrong is noisy or
revealing.
*/

type timelineMessage struct {
	ID      string         `json:"id"`
	Sender  string         `json:"sender"`
	Content map[string]any `json:"content"`
}

func setAutomaticReply(t *testing.T, handler http.Handler, token string, rule map[string]any) string {
	t.Helper()
	if _, ok := rule["message"]; !ok {
		rule["message"] = "Away until Monday — ask @founder meanwhile."
	}
	created := decode(t, mustCall(t, handler, http.MethodPost, "/api/me/automatic-replies",
		rule, token, http.StatusCreated))
	id, _ := created["id"].(string)
	return id
}

func openDirectWith(t *testing.T, handler http.Handler, token string, usernames ...string) string {
	t.Helper()
	room := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"usernames": usernames}, token, http.StatusOK))
	id, _ := room["id"].(string)
	return id
}

func messagesIn(t *testing.T, handler http.Handler, token, roomID string) []timelineMessage {
	t.Helper()
	var timeline []timelineMessage
	recorder := mustCall(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, token, http.StatusOK)
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	return timeline
}

func repliesTo(t *testing.T, handler http.Handler, token, roomID, rootID string) []timelineMessage {
	t.Helper()
	var thread struct {
		Replies []timelineMessage `json:"replies"`
	}
	recorder := mustCall(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/threads/"+rootID, nil, token, http.StatusOK)
	if err := json.Unmarshal(recorder.Body.Bytes(), &thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	return thread.Replies
}

// automatic picks out the automatic replies among some messages.
func automatic(messages []timelineMessage) []timelineMessage {
	var found []timelineMessage
	for _, message := range messages {
		if via, _ := message.Content["via"].(map[string]any); via["kind"] == "automatic" {
			found = append(found, message)
		}
	}
	return found
}

// A direct message is answered once, as the person and marked as automatic;
// the same sender writing again the same day is not answered again.
func TestADirectMessageIsAnsweredOnce(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	setAutomaticReply(t, handler, amina, map[string]any{"message": "On leave until **Monday**."})

	room := openDirectWith(t, handler, founder, "amina")
	sendMessage(t, handler, founder, room, "Are you around?")

	replies := automatic(messagesIn(t, handler, founder, room))
	if len(replies) != 1 {
		t.Fatalf("got %d automatic replies, want 1", len(replies))
	}
	reply := replies[0]
	if !strings.HasPrefix(reply.Sender, "amina@") {
		t.Errorf("the reply was sent as %q, want amina", reply.Sender)
	}
	if reply.Content["body"] != "On leave until **Monday**." {
		t.Errorf("the reply says %v", reply.Content["body"])
	}

	sendMessage(t, handler, founder, room, "Hello?")
	if again := automatic(messagesIn(t, handler, founder, room)); len(again) != 1 {
		t.Errorf("a second message from the same sender was answered again: %d replies", len(again))
	}

	status := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/status", nil, amina, http.StatusOK))
	if status["automaticReply"] != true {
		t.Errorf("her own status does not say her automatic reply is on: %v", status)
	}
}

// A mention in a channel is answered in the thread of the message that named
// the person, with the mention text, and the channel itself is left alone.
func TestAMentionIsAnsweredInItsThread(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	room := makeRoom(t, handler, founder, "general", "public")
	joinRoom(t, handler, amina, room)
	setAutomaticReply(t, handler, amina, map[string]any{
		"message": "Direct text", "mentionMessage": "Mention text",
	})

	sendMessage(t, handler, founder, room, "Nobody named here")
	if found := automatic(messagesIn(t, handler, founder, room)); len(found) != 0 {
		t.Fatalf("a message naming nobody was answered: %v", found)
	}

	root := sendMessage(t, handler, founder, room, "@amina can you look at this?")
	if found := automatic(messagesIn(t, handler, founder, room)); len(found) != 0 {
		t.Errorf("the answer to a mention went into the channel itself")
	}
	replies := automatic(repliesTo(t, handler, founder, room, root))
	if len(replies) != 1 || replies[0].Content["body"] != "Mention text" {
		t.Fatalf("the thread holds %v, want one reply saying the mention text", replies)
	}
}

// Outside its window, a reply says nothing; inside, it answers.
func TestAReplyAnswersOnlyInsideItsWindow(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")

	now := time.Now()
	setAutomaticReply(t, handler, amina, map[string]any{
		"startsAt": now.Add(time.Hour), "timeZone": "Africa/Dar_es_Salaam",
	})
	setAutomaticReply(t, handler, amina, map[string]any{
		"startsAt": now.Add(-2 * time.Hour), "endsAt": now.Add(-time.Hour),
	})

	room := openDirectWith(t, handler, founder, "amina")
	sendMessage(t, handler, founder, room, "Hi")
	if found := automatic(messagesIn(t, handler, founder, room)); len(found) != 0 {
		t.Fatalf("a reply outside its window answered: %v", found)
	}

	setAutomaticReply(t, handler, amina, map[string]any{
		"startsAt": now.Add(-time.Minute), "endsAt": now.Add(time.Hour),
	})
	other := openDirectWith(t, handler, baraka, "amina")
	sendMessage(t, handler, baraka, other, "Hi")
	if found := automatic(messagesIn(t, handler, baraka, other)); len(found) != 1 {
		t.Errorf("a reply inside its window gave %d answers, want 1", len(found))
	}
}

// A reply tied to a status answers only while it is held.
func TestAReplyTiedToAStatusFollowsIt(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	setAutomaticReply(t, handler, amina, map[string]any{"statuses": []string{"away"}})

	room := openDirectWith(t, handler, founder, "amina")
	sendMessage(t, handler, founder, room, "First")
	if found := automatic(messagesIn(t, handler, founder, room)); len(found) != 0 {
		t.Fatalf("answered while available: %v", found)
	}

	mustCall(t, handler, http.MethodPut, "/api/me/status", map[string]any{"status": "away"}, amina, http.StatusOK)
	sendMessage(t, handler, founder, room, "Second")
	if found := automatic(messagesIn(t, handler, founder, room)); len(found) != 1 {
		t.Errorf("answered %d times while away, want 1", len(found))
	}
}

/*
Appearing offline withholds a status from everybody, so a reply tied to that
status must not fire and announce it. A reply tied to Appear offline itself
does fire — it is the person's own words, and it does not depend on whether
they are connected.
*/
func TestAppearingOfflineIsNotContradicted(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	setAutomaticReply(t, handler, amina, map[string]any{"statuses": []string{"busy"}, "message": "In meetings"})

	mustCall(t, handler, http.MethodPut, "/api/me/status",
		map[string]any{"status": "busy", "invisible": true}, amina, http.StatusOK)

	room := openDirectWith(t, handler, founder, "amina")
	sendMessage(t, handler, founder, room, "Hi")
	if found := automatic(messagesIn(t, handler, founder, room)); len(found) != 0 {
		t.Fatalf("a reply tied to busy answered while she appears offline: %v", found)
	}

	setAutomaticReply(t, handler, amina, map[string]any{"statuses": []string{"offline"}, "message": "Not here today"})
	sendMessage(t, handler, founder, room, "Hi again")
	found := automatic(messagesIn(t, handler, founder, room))
	if len(found) != 1 || found[0].Content["body"] != "Not here today" {
		t.Errorf("a reply tied to Appear offline gave %v", found)
	}
}

// Somebody naming themselves, or writing in a conversation of their own,
// is never answered by their own reply; nor is an automatic reply answered.
func TestNeverAnswersOneselfOrAnotherAutomaticReply(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	room := makeRoom(t, handler, founder, "general", "public")
	joinRoom(t, handler, amina, room)
	setAutomaticReply(t, handler, amina, map[string]any{})
	setAutomaticReply(t, handler, founder, map[string]any{"message": "Founder is away"})

	root := sendMessage(t, handler, amina, room, "Note to self, @amina: tidy up")
	if found := automatic(repliesTo(t, handler, amina, room, root)); len(found) != 0 {
		t.Errorf("her own mention of herself was answered: %v", found)
	}

	// Both away: amina's answer to the founder must not draw the founder's.
	direct := openDirectWith(t, handler, founder, "amina")
	sendMessage(t, handler, founder, direct, "Hi")
	found := automatic(messagesIn(t, handler, founder, direct))
	if len(found) != 1 || !strings.HasPrefix(found[0].Sender, "amina@") {
		t.Errorf("two people away answered each other: %v", found)
	}
}

// A program naming somebody is never answered.
func TestNeverAnswersABot(t *testing.T) {
	handler, admin, roomID, _, credential := botFixture(t, "channel.read", "channel.post")
	setAutomaticReply(t, handler, admin, map[string]any{})

	postMessage(t, handler, credential, roomID, "@founder build 41 failed")
	for _, message := range messagesIn(t, handler, admin, roomID) {
		if replies := automatic(repliesTo(t, handler, admin, roomID, message.ID)); len(replies) != 0 {
			t.Fatalf("a bot's mention was answered: %v", replies)
		}
	}

	// And somebody naming the same person is, so the silence above is the
	// bot rule rather than nothing working.
	amina := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, amina, roomID)
	root := sendMessage(t, handler, amina, roomID, "@founder build 41 failed")
	if replies := automatic(repliesTo(t, handler, admin, roomID, root)); len(replies) != 1 {
		t.Errorf("a person's mention gave %d answers, want 1", len(replies))
	}
}

// Somebody the person has blocked is not answered, in a direct conversation
// that predates the block or in a channel they share.
func TestNeverAnswersSomebodyBlocked(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	room := makeRoom(t, handler, founder, "general", "public")
	joinRoom(t, handler, amina, room)
	direct := openDirectWith(t, handler, founder, "amina")

	setAutomaticReply(t, handler, amina, map[string]any{})
	me := decode(t, mustCall(t, handler, http.MethodGet, "/api/me", nil, founder, http.StatusOK))
	mustCall(t, handler, http.MethodPost, "/api/me/blocks", map[string]any{"user": me["id"]}, amina, http.StatusNoContent)

	sendMessage(t, handler, founder, direct, "Hi")
	if found := automatic(messagesIn(t, handler, founder, direct)); len(found) != 0 {
		t.Errorf("a blocked person's direct message was answered: %v", found)
	}
	root := sendMessage(t, handler, founder, room, "@amina hi")
	if found := automatic(repliesTo(t, handler, founder, room, root)); len(found) != 0 {
		t.Errorf("a blocked person's mention was answered: %v", found)
	}
}

// Somebody who blocked the person is not sent their words either, and the
// person's own choice of who may contact them holds for mentions too.
func TestNeverAnswersAcrossABlockOrAClosedDoor(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	baraka := registerAndSignIn(t, handler, "baraka")
	room := makeRoom(t, handler, founder, "general", "public")
	joinRoom(t, handler, amina, room)
	joinRoom(t, handler, baraka, room)
	setAutomaticReply(t, handler, amina, map[string]any{})

	// The founder blocks amina, then names her anyway.
	her := decode(t, mustCall(t, handler, http.MethodGet, "/api/me", nil, amina, http.StatusOK))
	mustCall(t, handler, http.MethodPost, "/api/me/blocks", map[string]any{"user": her["id"]}, founder, http.StatusNoContent)
	root := sendMessage(t, handler, founder, room, "@amina hi")
	if found := automatic(repliesTo(t, handler, founder, room, root)); len(found) != 0 {
		t.Errorf("somebody who blocked her was sent her automatic reply: %v", found)
	}

	// She accepts conversations from nobody: baraka naming her hears nothing.
	mustCall(t, handler, http.MethodPut, "/api/me/privacy", map[string]any{"directMessages": "nobody"}, amina, http.StatusNoContent)
	root = sendMessage(t, handler, baraka, room, "@amina hi")
	if found := automatic(repliesTo(t, handler, baraka, room, root)); len(found) != 0 {
		t.Errorf("somebody she does not accept conversations from was answered: %v", found)
	}
}

// What cannot be kept is refused with a reason.
func TestAnAutomaticReplyIsValidated(t *testing.T) {
	handler := newTestAPI(t)
	amina := completeSetup(t, handler)

	for name, body := range map[string]map[string]any{
		"empty":          {"message": "  "},
		"too long":       {"message": strings.Repeat("a", 501)},
		"unknown status": {"message": "x", "statuses": []string{"sleeping"}},
		"backwards":      {"message": "x", "startsAt": time.Now().Add(time.Hour), "endsAt": time.Now()},
		"no zone":        {"message": "x", "timeZone": "Mars/Olympus"},
		"answers nobody": {"message": "x", "replyToDirect": false, "replyToGroups": false, "replyToMentions": false},
	} {
		if recorder := call(t, handler, http.MethodPost, "/api/me/automatic-replies", body, amina); recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, recorder.Code)
		}
	}

	id := setAutomaticReply(t, handler, amina, map[string]any{})
	mustCall(t, handler, http.MethodPut, "/api/me/automatic-replies/"+id,
		map[string]any{"message": "Changed", "enabled": false}, amina, http.StatusOK)
	other := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodDelete, "/api/me/automatic-replies/"+id, nil, other, http.StatusNotFound)
	mustCall(t, handler, http.MethodDelete, "/api/me/automatic-replies/"+id, nil, amina, http.StatusNoContent)
}
