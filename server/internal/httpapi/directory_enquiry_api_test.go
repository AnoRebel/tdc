package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
Enquiries: first contact through a profile, governed by the profile's own
setting rather than by who may send direct messages, and limited so the
directory cannot be used to approach everybody at once.
*/

// sendEnquiry sends an enquiry and returns the response.
func sendEnquiry(t *testing.T, handler http.Handler, token, username, subject string) (int, map[string]any, string) {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/directory/profiles/"+username+"/enquiries",
		map[string]string{"subject": subject, "body": "We need a booking app for three clinics."}, token)
	if recorder.Code != http.StatusCreated {
		return recorder.Code, nil, recorder.Body.String()
	}
	return recorder.Code, decode(t, recorder), recorder.Body.String()
}

// enquiriesOf lists somebody's enquiries, received or sent.
func enquiriesOf(t *testing.T, handler http.Handler, token, box string) []map[string]any {
	t.Helper()
	path := "/api/directory/me/enquiries"
	if box != "" {
		path += "?box=" + box
	}
	listed := []map[string]any{}
	for _, entry := range decodeList(t, call(t, handler, http.MethodGet, path, nil, token)) {
		listed = append(listed, entry.(map[string]any))
	}
	return listed
}

// notificationsOf lists somebody's unread notifications.
func notificationsOf(t *testing.T, handler http.Handler, token string) []map[string]any {
	t.Helper()
	listed := []map[string]any{}
	for _, entry := range decodeList(t, call(t, handler, http.MethodGet, "/api/me/notifications", nil, token)) {
		listed = append(listed, entry.(map[string]any))
	}
	return listed
}

// A public profile takes enquiries unless its owner says otherwise.
func publicDeveloper(t *testing.T, handler http.Handler, username string) string {
	t.Helper()
	token := registerAndSignIn(t, handler, username)
	saveProfile(t, handler, token, map[string]any{"visibility": "public", "headline": "Developer"})
	return token
}

/*
An enquiry reaches its recipient, who is notified and can read it; its sender
is not notified of their own enquiry and cannot add to it until it is answered.
*/
func TestAnEnquiryReachesItsRecipient(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")

	code, sent, body := sendEnquiry(t, handler, client, "juma", "Clinic booking app")
	if code != http.StatusCreated {
		t.Fatalf("sending an enquiry returned %d: %s", code, body)
	}
	roomID := sent["roomId"].(string)

	received := enquiriesOf(t, handler, juma, "")
	if len(received) != 1 || received[0]["subject"] != "Clinic booking app" || received[0]["with"] != "client" {
		t.Fatalf("juma's enquiries are %v", received)
	}
	if outbox := enquiriesOf(t, handler, client, "sent"); len(outbox) != 1 || outbox[0]["status"] != "pending" {
		t.Errorf("the client's sent enquiries are %v", outbox)
	}

	notified := notificationsOf(t, handler, juma)
	if len(notified) != 1 || notified[0]["roomId"] != roomID {
		t.Errorf("juma was not notified of the enquiry: %v", notified)
	}
	if mine := notificationsOf(t, handler, client); len(mine) != 0 {
		t.Errorf("the sender was notified of their own enquiry: %v", mine)
	}

	timeline := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, juma)
	if timeline.Code != http.StatusOK || !strings.Contains(timeline.Body.String(), "booking app for three clinics") {
		t.Errorf("juma cannot read the enquiry: %d %s", timeline.Code, timeline.Body.String())
	}

	// Until juma answers, the client has said their piece.
	if recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "Hello? Are you there?"}, client); recorder.Code == http.StatusCreated {
		t.Error("the sender added to an enquiry nobody has answered")
	}
}

/*
Somebody not accepting enquiries refuses them, and the sender is told plainly.

A profile's default follows its visibility — public takes enquiries, anything
else does not — and the owner's explicit choice overrides it either way.
*/
func TestAProfileNotAcceptingEnquiriesRefusesThem(t *testing.T) {
	handler, _ := withDirectory(t)
	closed := registerAndSignIn(t, handler, "closed")
	members := registerAndSignIn(t, handler, "members")
	opened := registerAndSignIn(t, handler, "opened")
	client := registerAndSignIn(t, handler, "client")

	saveProfile(t, handler, closed, map[string]any{"visibility": "public", "acceptsEnquiries": false})
	saveProfile(t, handler, members, map[string]any{"visibility": "members"})
	saveProfile(t, handler, opened, map[string]any{"visibility": "members", "acceptsEnquiries": true})

	for _, username := range []string{"closed", "members"} {
		code, _, body := sendEnquiry(t, handler, client, username, "Work")
		if code != http.StatusForbidden || !strings.Contains(body, "not accepting enquiries") {
			t.Errorf("an enquiry to %s was answered %d %s; want a plain refusal", username, code, body)
		}
		if _, view := viewProfile(t, handler, client, username); view["acceptsEnquiries"] != false {
			t.Errorf("%s's profile says it takes enquiries", username)
		}
	}
	if code, _, body := sendEnquiry(t, handler, client, "opened", "Work"); code != http.StatusCreated {
		t.Errorf("an enquiry to a profile that chose to take them was answered %d %s", code, body)
	}
	if len(enquiriesOf(t, handler, closed, "")) != 0 {
		t.Error("a refused enquiry was delivered")
	}
}

/*
Enquiries and direct messages are separate decisions.

Closing direct messages does not close enquiries; closing enquiries does not
close direct messages.
*/
func TestEnquiriesAndDirectMessagesAreIndependent(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	client := registerAndSignIn(t, handler, "client")

	setPrivacy(t, handler, juma, map[string]any{"directMessages": "nobody"})
	saveProfile(t, handler, amina, map[string]any{"visibility": "public", "acceptsEnquiries": false})

	openDirect := func(with string) int {
		return call(t, handler, http.MethodPost, "/api/direct",
			map[string]any{"userIds": []string{accountID(t, handler, with)}}, client).Code
	}

	if code := openDirect(juma); code == http.StatusCreated || code == http.StatusOK {
		t.Error("juma closed direct messages and the client opened one anyway")
	}
	if code, _, body := sendEnquiry(t, handler, client, "juma", "Work"); code != http.StatusCreated {
		t.Errorf("closing direct messages closed enquiries: %d %s", code, body)
	}

	if code, _, _ := sendEnquiry(t, handler, client, "amina", "Work"); code != http.StatusForbidden {
		t.Errorf("amina closed enquiries and one was accepted: %d", code)
	}
	if code := openDirect(amina); code != http.StatusCreated && code != http.StatusOK {
		t.Errorf("closing enquiries closed direct messages: %d", code)
	}
}

// Closing enquiries refuses new ones and leaves the ones already there.
func TestClosingEnquiriesLeavesExistingOnesAlone(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	other := registerAndSignIn(t, handler, "other")

	_, sent, _ := sendEnquiry(t, handler, client, "juma", "First")
	saveProfile(t, handler, juma, map[string]any{"visibility": "public", "acceptsEnquiries": false})

	if code, _, _ := sendEnquiry(t, handler, other, "juma", "Second"); code != http.StatusForbidden {
		t.Errorf("an enquiry after closing was answered %d", code)
	}
	if received := enquiriesOf(t, handler, juma, ""); len(received) != 1 {
		t.Errorf("closing enquiries changed the ones already received: %v", received)
	}
	if recorder := call(t, handler, http.MethodGet, "/api/rooms/"+sent["roomId"].(string)+"/timeline", nil, juma); recorder.Code != http.StatusOK {
		t.Errorf("closing enquiries closed an existing conversation: %d", recorder.Code)
	}
}

/*
A blocked sender is refused exactly as a closed profile refuses anybody.

The same status and the same words, and the profile reads as taking no
enquiries — so the block is not disclosed.
*/
func TestABlockedSenderCannotTellTheyWereBlocked(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	publicDeveloper(t, handler, "closed")
	client := registerAndSignIn(t, handler, "client")
	closedToken := signIn(t, handler, "closed", testPassword)
	saveProfile(t, handler, closedToken, map[string]any{"visibility": "public", "acceptsEnquiries": false})

	if recorder := call(t, handler, http.MethodPost, "/api/me/blocks",
		map[string]string{"user": accountID(t, handler, client)}, juma); recorder.Code >= 300 {
		t.Fatalf("blocking returned %d", recorder.Code)
	}

	blockedCode, _, blockedBody := sendEnquiry(t, handler, client, "juma", "Work")
	closedCode, _, closedBody := sendEnquiry(t, handler, client, "closed", "Work")
	if blockedCode != closedCode || blockedBody != closedBody {
		t.Errorf("a block is distinguishable from a closed profile: %d %q vs %d %q",
			blockedCode, blockedBody, closedCode, closedBody)
	}
	if _, view := viewProfile(t, handler, client, "juma"); view["acceptsEnquiries"] != false {
		t.Error("the profile tells the blocked person it takes enquiries")
	}
}

/*
Sending enquiries is rate limited per sender. Somebody approaching everybody
listed is refused; somebody else sending one at the same time is not.
*/
func TestEnquiriesAreRateLimited(t *testing.T) {
	handler, founder := withDirectory(t)
	for _, username := range []string{"one", "two", "three"} {
		publicDeveloper(t, handler, username)
	}
	bulk := registerAndSignIn(t, handler, "bulk")

	if recorder := call(t, handler, http.MethodPut, "/api/admin/settings/directory.enquiries_per_hour",
		map[string]string{"value": "2"}, founder); recorder.Code != http.StatusNoContent {
		t.Fatalf("setting the rate: %d", recorder.Code)
	}

	for _, username := range []string{"one", "two"} {
		if code, _, body := sendEnquiry(t, handler, bulk, username, "Work"); code != http.StatusCreated {
			t.Fatalf("enquiry to %s was refused: %d %s", username, code, body)
		}
		// Asking again before an answer is refused, and costs nothing: it is
		// not an enquiry sent.
		if code, _, _ := sendEnquiry(t, handler, bulk, username, "Again"); code != http.StatusConflict {
			t.Fatalf("a second enquiry to %s before an answer was answered %d", username, code)
		}
	}
	recorder := call(t, handler, http.MethodPost, "/api/directory/profiles/three/enquiries",
		map[string]string{"subject": "Work", "body": "Same message to everybody."}, bulk)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") == "" {
		t.Errorf("the third enquiry in an hour was answered %d, want 429", recorder.Code)
	}
	if len(enquiriesOf(t, handler, signIn(t, handler, "three", testPassword), "")) != 0 {
		t.Error("a rate-limited enquiry was delivered")
	}

	if code, _, body := sendEnquiry(t, handler, founder, "three", "Work"); code != http.StatusCreated {
		t.Errorf("one sender's limit refused another: %d %s", code, body)
	}
}

// One unanswered enquiry at a time, and never to oneself or from a stranger.
func TestEnquiriesAreOneAtATimeAndFromMembers(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")

	sendEnquiry(t, handler, client, "juma", "First")
	if code, _, _ := sendEnquiry(t, handler, client, "juma", "Again"); code != http.StatusConflict {
		t.Errorf("a second enquiry before an answer was answered %d", code)
	}
	if code, _, _ := sendEnquiry(t, handler, juma, "juma", "Myself"); code != http.StatusForbidden {
		t.Errorf("an enquiry to oneself was answered %d", code)
	}
	if code, _, _ := sendEnquiry(t, handler, "", "juma", "Stranger"); code != http.StatusUnauthorized {
		t.Errorf("an enquiry from somebody not signed in was answered %d", code)
	}
}
