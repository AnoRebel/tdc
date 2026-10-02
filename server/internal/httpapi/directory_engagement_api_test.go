package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

/*
Engagements: an enquiry taken up becomes a tracked engagement, both parties read
the same state from the same record, every step is recorded with who and when,
and the conversation is private to the two of them by being the same kind of
conversation a direct message is.
*/

// engagementBetween sends an enquiry from client to juma and has juma take it
// up. Returns the engagement.
func engagementBetween(t *testing.T, handler http.Handler, client, juma string) map[string]any {
	t.Helper()
	code, sent, body := sendEnquiry(t, handler, client, "juma", "Clinic booking app")
	if code != http.StatusCreated {
		t.Fatalf("sending an enquiry returned %d: %s", code, body)
	}
	recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+sent["id"].(string)+"/accept", nil, juma)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("taking up an enquiry returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

func engagementAs(t *testing.T, handler http.Handler, token, id string) (int, map[string]any, string) {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id, nil, token)
	if recorder.Code != http.StatusOK {
		return recorder.Code, nil, recorder.Body.String()
	}
	return recorder.Code, decode(t, recorder), recorder.Body.String()
}

/*
Taking up an enquiry starts an engagement in discussion, recorded as a step from
the enquiry by the person who took it up, and opens its conversation to the
sender — once, and only by the person it was sent to.
*/
func TestTakingUpAnEnquiryStartsAnEngagement(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")

	code, sent, _ := sendEnquiry(t, handler, client, "juma", "Clinic booking app")
	if code != http.StatusCreated {
		t.Fatalf("sending returned %d", code)
	}
	enquiry := sent["id"].(string)

	if recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+enquiry+"/accept", nil, client); recorder.Code != http.StatusNotFound {
		t.Errorf("the sender took up their own enquiry: %d", recorder.Code)
	}
	recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+enquiry+"/accept", nil, juma)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("taking up returned %d: %s", recorder.Code, recorder.Body.String())
	}
	engagement := decode(t, recorder)
	if recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+enquiry+"/accept", nil, juma); recorder.Code != http.StatusConflict {
		t.Errorf("taking up the same enquiry twice was answered %d", recorder.Code)
	}

	id := engagement["id"].(string)
	_, asClient, _ := engagementAs(t, handler, client, id)
	_, asDeveloper, _ := engagementAs(t, handler, juma, id)
	if asClient["state"] != "discussion" || asDeveloper["state"] != "discussion" {
		t.Errorf("the parties see %v and %v, want discussion for both", asClient["state"], asDeveloper["state"])
	}
	if asClient["you"] != "client" || asDeveloper["you"] != "developer" {
		t.Errorf("the parties are named %v and %v", asClient["you"], asDeveloper["you"])
	}
	transitions := asClient["transitions"].([]any)
	first := transitions[0].(map[string]any)
	if len(transitions) != 1 || first["from"] != "enquiry" || first["to"] != "discussion" || first["actor"] != "juma" || first["at"] == "" {
		t.Errorf("taking up the enquiry was not recorded with who and when: %v", transitions)
	}

	roomID := engagement["roomId"].(string)
	if recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "Thank you for taking this up"}, client); recorder.Code != http.StatusCreated {
		t.Errorf("the client cannot talk in the engagement's conversation: %d", recorder.Code)
	}
	if received := enquiriesOf(t, handler, juma, ""); received[0]["status"] != "accepted" || received[0]["engagementId"] != id {
		t.Errorf("the enquiry does not say it was taken up, and as what: %v", received[0])
	}
}

/*
Nobody outside an engagement can see it.

Not another member, not the server's administrator: each is told exactly what an
engagement that does not exist answers, and none of them can read its
conversation.
*/
func TestOnlyThePartiesCanSeeAnEngagement(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	other := registerAndSignIn(t, handler, "other")

	engagement := engagementBetween(t, handler, client, juma)
	id := engagement["id"].(string)

	_, _, missing := engagementAs(t, handler, other, "00000000-0000-0000-0000-000000000001")
	for name, token := range map[string]string{"another member": other, "the administrator": founder} {
		code, _, body := engagementAs(t, handler, token, id)
		if code != http.StatusNotFound || body != missing {
			t.Errorf("%s was answered %d %q; an engagement that does not exist answers %q", name, code, body, missing)
		}
		if recorder := call(t, handler, http.MethodGet, "/api/rooms/"+engagement["roomId"].(string)+"/timeline", nil, token); recorder.Code == http.StatusOK {
			t.Errorf("%s read the engagement's conversation", name)
		}
		if listed := decodeList(t, call(t, handler, http.MethodGet, "/api/directory/me/engagements", nil, token)); len(listed) != 0 {
			t.Errorf("%s has the engagement in their list", name)
		}
	}
}

/*
Either party can withdraw before an agreement, and the engagement closes cleanly:
withdrawn for both of them, recorded with who and why, its conversation kept and
closed to new messages, and nothing further possible.
*/
func TestWithdrawingClosesTheEngagementCleanly(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")

	engagement := engagementBetween(t, handler, client, juma)
	id := engagement["id"].(string)
	roomID := engagement["roomId"].(string)

	recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/withdraw",
		map[string]string{"reason": "We found somebody in town"}, client)
	if recorder.Code != http.StatusOK {
		t.Fatalf("withdrawing returned %d: %s", recorder.Code, recorder.Body.String())
	}

	for name, token := range map[string]string{"client": client, "developer": juma} {
		_, seen, _ := engagementAs(t, handler, token, id)
		if seen["state"] != "withdrawn" || seen["withdrawable"] != false {
			t.Errorf("the %s sees %v", name, seen["state"])
		}
		last := seen["transitions"].([]any)[len(seen["transitions"].([]any))-1].(map[string]any)
		if last["to"] != "withdrawn" || last["actor"] != "client" || last["note"] != "We found somebody in town" {
			t.Errorf("the withdrawal was not recorded with who and why: %v", last)
		}
		if _, present := seen["agreement"]; present {
			t.Error("a withdrawn engagement carries an agreement")
		}
	}

	if recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "One more thing"}, juma); recorder.Code == http.StatusCreated {
		t.Error("a withdrawn engagement's conversation still takes messages")
	}
	if recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, juma); recorder.Code != http.StatusOK {
		t.Errorf("withdrawing lost the conversation: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/withdraw", nil, juma); recorder.Code != http.StatusConflict {
		t.Errorf("withdrawing twice was answered %d", recorder.Code)
	}
}

/*
No step can be skipped, and nothing can be withdrawn after an agreement.

The database refuses a recorded step that is not one of the documented moves,
whatever wrote it.
*/
func TestNoStepCanBeSkipped(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	engagement := engagementBetween(t, handler, client, juma)
	id := engagement["id"].(string)

	ctx := context.Background()
	for _, skip := range [][2]string{{"discussion", "completed"}, {"discussion", "agreement"}, {"enquiry", "active"}, {"withdrawn", "discussion"}} {
		if _, err := poolForTest.Exec(ctx,
			"INSERT INTO engagement_transitions (engagement_id, from_state, to_state, actor_label) VALUES ($1, $2, $3, 'x')",
			id, skip[0], skip[1]); err == nil {
			t.Errorf("the database recorded a step from %s to %s", skip[0], skip[1])
		}
	}

	if _, err := poolForTest.Exec(ctx, "UPDATE engagements SET state = 'agreement' WHERE id = $1", id); err != nil {
		t.Fatalf("moving the engagement on: %v", err)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/withdraw", nil, client); recorder.Code != http.StatusConflict {
		t.Errorf("withdrawing after an agreement was answered %d", recorder.Code)
	}
}

// Declining or withdrawing an enquiry ends it without an engagement.
func TestAnEnquiryCanBeDeclinedOrWithdrawn(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	other := registerAndSignIn(t, handler, "other")

	_, first, _ := sendEnquiry(t, handler, client, "juma", "First")
	_, second, _ := sendEnquiry(t, handler, other, "juma", "Second")

	if recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+first["id"].(string)+"/decline", nil, client); recorder.Code != http.StatusNotFound {
		t.Errorf("the sender declined their own enquiry: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+first["id"].(string)+"/decline", nil, juma); recorder.Code != http.StatusOK {
		t.Fatalf("declining returned %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+second["id"].(string)+"/withdraw", nil, juma); recorder.Code != http.StatusNotFound {
		t.Errorf("the recipient withdrew somebody else's enquiry: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+second["id"].(string)+"/withdraw", nil, other); recorder.Code != http.StatusOK {
		t.Fatalf("withdrawing returned %d", recorder.Code)
	}

	for _, id := range []string{first["id"].(string), second["id"].(string)} {
		if recorder := call(t, handler, http.MethodPost, "/api/directory/enquiries/"+id+"/accept", nil, juma); recorder.Code != http.StatusConflict {
			t.Errorf("an enquiry already answered was taken up: %d", recorder.Code)
		}
	}
	if listed := decodeList(t, call(t, handler, http.MethodGet, "/api/directory/me/engagements", nil, juma)); len(listed) != 0 {
		t.Errorf("an enquiry that was not taken up became an engagement: %v", listed)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/rooms/"+first["roomId"].(string)+"/messages",
		map[string]string{"body": "After all"}, juma); recorder.Code == http.StatusCreated {
		t.Error("a declined enquiry's conversation still takes messages")
	}
}

/*
An engagement's conversation never appears in anybody else's search.

Not another member's, not an administrator's ordinary search: the conversation is
found by the two people in it and nobody else, by its name or by what was said.
*/
func TestAnEngagementConversationIsNotDiscoverable(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	other := registerAndSignIn(t, handler, "other")

	engagement := engagementBetween(t, handler, client, juma)
	roomID := engagement["roomId"].(string)
	call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "zanzibarpineapple budget discussion"}, client)

	for name, token := range map[string]string{"another member": other, "the administrator": founder} {
		for _, query := range []string{"zanzibarpineapple", "Clinic booking"} {
			body := call(t, handler, http.MethodGet, "/api/search/everything?q="+strings.ReplaceAll(query, " ", "+"), nil, token).Body.String()
			if strings.Contains(body, roomID) || strings.Contains(body, "zanzibarpineapple") {
				t.Errorf("%s's search for %q found the engagement's conversation", name, query)
			}
		}
		if body := call(t, handler, http.MethodGet, "/api/rooms", nil, token).Body.String(); strings.Contains(body, roomID) {
			t.Errorf("%s has the engagement's conversation in their rooms", name)
		}
	}

	// The control: the parties themselves find it, so the searches above
	// would have found it had it been visible.
	body := call(t, handler, http.MethodGet, "/api/search/everything?q=zanzibarpineapple", nil, juma).Body.String()
	if !strings.Contains(body, "zanzibarpineapple") {
		t.Error("a party's own search does not find their engagement's messages")
	}
}

/*
The conversation is subject to the same rules as any conversation, because it is
one: files are uploaded and refused by the same rules, and the server's
retention removes its content like anybody else's.
*/
func TestAnEngagementConversationFollowsTheOrdinaryRules(t *testing.T) {
	handler, services := newTestAPIWithServices(t)
	cleanDirectory(t)
	founder := completeSetup(t, handler)
	setFeature(t, handler, founder, "directory", true)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	other := registerAndSignIn(t, handler, "other")

	engagement := engagementBetween(t, handler, client, juma)
	roomID := engagement["roomId"].(string)

	if recorder := uploadFile(t, handler, client, roomID, "brief.txt", []byte("the brief")); recorder.Code != http.StatusCreated {
		t.Errorf("a party could not attach a file: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := uploadFile(t, handler, other, roomID, "spam.txt", []byte("spam")); recorder.Code == http.StatusCreated {
		t.Error("somebody outside the engagement attached a file to its conversation")
	}

	sendMessage(t, handler, client, roomID, "Old terms we discussed")
	ageMessages(t, roomID, "10 days")
	if _, err := services.Moderation.ExpireDue(context.Background(), 7*24*3600, 100, nil); err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline?limit=50", nil, juma).Body.String()
	if strings.Contains(timeline, "Old terms we discussed") {
		t.Error("the server's retention did not reach the engagement's conversation")
	}
}
