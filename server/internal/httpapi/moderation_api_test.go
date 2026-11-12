package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// userIDOf looks somebody up through search, which is the only way a test can
// learn an identifier without reaching past the API.
func userIDOf(t *testing.T, handler http.Handler, token, username string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodGet,
		"/api/search/everything?q="+username+"&messages=false", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("searching for %q returned %d", username, recorder.Code)
	}

	var found struct {
		People []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"people"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &found); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	for _, person := range found.People {
		if person.Username == username {
			return person.ID
		}
	}
	t.Fatalf("no person called %q", username)
	return ""
}

/*
TestABanIsConfinedToItsChannel is the difference between a ban and a
suspension.

Somebody banned from one conversation carries on everywhere else. A ban that
reached other channels would be a suspension issued by whoever moderates one
room, which is authority nobody granted them.
*/
func TestABanIsConfinedToItsChannel(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	banned := makeRoom(t, handler, owner, "moja", "public")
	other := makeRoom(t, handler, owner, "mbili", "public")

	person := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, person, banned)
	joinRoom(t, handler, person, other)

	targetID := userIDOf(t, handler, owner, "amina")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+banned+"/bans",
		map[string]any{"user": targetID, "reason": "Kuvuruga mazungumzo"}, owner)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("banning returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	// Cannot post in the channel they are banned from.
	refused := call(t, handler, http.MethodPost, "/api/rooms/"+banned+"/messages",
		map[string]any{"body": "Bado nipo"}, person)
	if refused.Code != http.StatusForbidden {
		t.Errorf("a banned person posted, returning %d", refused.Code)
	}

	// And cannot rejoin it.
	rejoined := call(t, handler, http.MethodPost, "/api/rooms/"+banned+"/join", nil, person)
	if rejoined.Code == http.StatusOK {
		t.Error("a banned person rejoined the channel they were banned from")
	}

	// But the other channel is untouched — the whole point of confinement.
	elsewhere := call(t, handler, http.MethodPost, "/api/rooms/"+other+"/messages",
		map[string]any{"body": "Hapa ni sawa"}, person)
	if elsewhere.Code != http.StatusCreated {
		t.Errorf("a ban in one channel stopped posting in another, returning %d",
			elsewhere.Code)
	}
}

// TestABanRequiresAReason keeps a decision reviewable.
//
// The person banned is entitled to know why, and a ban nobody can explain is
// one nobody can review.
func TestABanRequiresAReason(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	person := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, person, roomID)
	targetID := userIDOf(t, handler, owner, "amina")

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/bans",
		map[string]any{"user": targetID}, owner)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("banning without a reason returned %d, expected 400", recorder.Code)
	}
}

// TestLiftingABanReadmits covers the reversal every moderator action needs.
func TestLiftingABanReadmits(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	person := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, person, roomID)
	targetID := userIDOf(t, handler, owner, "amina")

	call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/bans",
		map[string]any{"user": targetID, "reason": "Kwa muda"}, owner)

	lifted := call(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/bans/"+targetID, nil, owner)
	if lifted.Code != http.StatusNoContent {
		t.Fatalf("lifting returned %d: %s", lifted.Code, truncate(lifted.Body.String()))
	}

	rejoined := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, person)
	if rejoined.Code != http.StatusOK {
		t.Errorf("somebody could not rejoin after their ban was lifted: %d", rejoined.Code)
	}
}

/*
TestBlockingIsNotDisclosed is the rule the whole feature turns on.

Somebody who learns they have been blocked frequently escalates. The point of
blocking is to end contact rather than to announce it, so nothing the blocked
person can see may reveal it.
*/
func TestBlockingIsNotDisclosed(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	blocked := registerAndSignIn(t, handler, "amina")

	blockedID := userIDOf(t, handler, owner, "amina")

	recorder := call(t, handler, http.MethodPost, "/api/me/blocks",
		map[string]any{"user": blockedID}, owner)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("blocking returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	// The blocked person's own view says nothing about it.
	theirBlocks := call(t, handler, http.MethodGet, "/api/me/blocks", nil, blocked)
	if theirBlocks.Code != http.StatusOK {
		t.Fatalf("reading blocks returned %d", theirBlocks.Code)
	}

	var listed []map[string]any
	if err := json.Unmarshal(theirBlocks.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(listed) != 0 {
		t.Error("the blocked person's own block list revealed that somebody blocked them")
	}

	// And the blocker's list is their own, showing the person they blocked.
	ownList := call(t, handler, http.MethodGet, "/api/me/blocks", nil, owner)
	if !strings.Contains(ownList.Body.String(), "amina") {
		t.Error("the blocker cannot see who they have blocked")
	}
}

/*
TestABlockedPersonCannotOpenAConversation covers the effect of a block.

The refusal must not say why. "They have blocked you" and "they accept messages
only from colleagues" are different disclosures, and the first is one the
blocked person must never receive.
*/
func TestABlockedPersonCannotOpenAConversation(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	blocked := registerAndSignIn(t, handler, "amina")

	ownerID := userIDOf(t, handler, blocked, "founder")
	blockedID := userIDOf(t, handler, owner, "amina")

	call(t, handler, http.MethodPost, "/api/me/blocks",
		map[string]any{"user": blockedID}, owner)

	recorder := call(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{ownerID}}, blocked)

	if recorder.Code == http.StatusOK || recorder.Code == http.StatusCreated {
		t.Error("a blocked person opened a conversation with the person who blocked them")
	}
	if strings.Contains(recorder.Body.String(), "block") {
		t.Errorf("the refusal disclosed the block: %s", truncate(recorder.Body.String()))
	}
}

/*
TestDirectMessagePolicyRefusalsAreIndistinguishable is what keeps a setting
from becoming a disclosure.

Blocked and "nobody" must refuse identically. If they read differently,
somebody can tell whether they were singled out or whether the person simply
accepts nothing — and the first is exactly what must not be revealed.
*/
func TestDirectMessagePolicyRefusalsAreIndistinguishable(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)

	blockedBy := registerAndSignIn(t, handler, "amina")
	closedOff := registerAndSignIn(t, handler, "juma")
	sender := registerAndSignIn(t, handler, "neema")

	senderID := userIDOf(t, handler, owner, "neema")

	// One blocks the sender.
	call(t, handler, http.MethodPost, "/api/me/blocks",
		map[string]any{"user": senderID}, blockedBy)

	// The other accepts nothing from anybody.
	set := call(t, handler, http.MethodPut, "/api/me/privacy",
		map[string]any{"directMessages": "nobody"}, closedOff)
	if set.Code != http.StatusNoContent {
		t.Fatalf("setting a privacy policy returned %d", set.Code)
	}

	bodies := map[string]string{}
	for name, username := range map[string]string{
		"blocked": "amina",
		"closed":  "juma",
	} {
		targetID := userIDOf(t, handler, owner, username)
		recorder := call(t, handler, http.MethodPost, "/api/direct",
			map[string]any{"userIds": []string{targetID}}, sender)

		if recorder.Code == http.StatusOK || recorder.Code == http.StatusCreated {
			t.Errorf("%s accepted a conversation they should not have", name)
		}
		bodies[name] = recorder.Body.String()
	}

	if bodies["blocked"] != bodies["closed"] {
		t.Errorf("being blocked reads differently from a closed setting: %q against %q",
			bodies["blocked"], bodies["closed"])
	}
}

/*
TestSharedPolicyAdmitsColleagues covers the setting most people actually want.

"Anyone" and "nobody" leave out the case in the middle: colleagues can reach
me, strangers cannot.
*/
func TestSharedPolicyAdmitsColleagues(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "kazi", "public")

	receiver := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, receiver, roomID)

	colleague := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, colleague, roomID)

	stranger := registerAndSignIn(t, handler, "mgeni")

	set := call(t, handler, http.MethodPut, "/api/me/privacy",
		map[string]any{"directMessages": "shared"}, receiver)
	if set.Code != http.StatusNoContent {
		t.Fatalf("setting returned %d", set.Code)
	}

	receiverID := userIDOf(t, handler, owner, "amina")

	// Somebody sharing a channel gets through.
	fromColleague := call(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{receiverID}}, colleague)
	if fromColleague.Code != http.StatusOK && fromColleague.Code != http.StatusCreated {
		t.Errorf("a colleague was refused with %d", fromColleague.Code)
	}

	// Somebody who shares nothing does not.
	fromStranger := call(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{receiverID}}, stranger)
	if fromStranger.Code == http.StatusOK || fromStranger.Code == http.StatusCreated {
		t.Error("a stranger reached somebody who accepts only colleagues")
	}
}

/*
TestNarrowingKeepsExistingConversations is what makes the setting safe to
change.

Somebody who tightens their setting is deciding who may start a conversation,
not discarding the ones they already have.
*/
func TestNarrowingKeepsExistingConversations(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	receiver := registerAndSignIn(t, handler, "amina")
	receiverID := userIDOf(t, handler, owner, "amina")

	// A conversation opened while the setting was open.
	opened := call(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{receiverID}}, owner)
	if opened.Code != http.StatusOK && opened.Code != http.StatusCreated {
		t.Fatalf("opening returned %d: %s", opened.Code, truncate(opened.Body.String()))
	}

	var conversation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &conversation); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	// Then it narrows.
	call(t, handler, http.MethodPut, "/api/me/privacy",
		map[string]any{"directMessages": "nobody"}, receiver)

	// The existing conversation still works, both ways.
	stillThere := call(t, handler, http.MethodGet, "/api/rooms/"+conversation.ID, nil, receiver)
	if stillThere.Code != http.StatusOK {
		t.Errorf("an existing conversation returned %d after narrowing", stillThere.Code)
	}

	sent := call(t, handler, http.MethodPost, "/api/rooms/"+conversation.ID+"/messages",
		map[string]any{"body": "Bado tunaongea"}, owner)
	if sent.Code != http.StatusCreated {
		t.Errorf("sending into an existing conversation returned %d after narrowing", sent.Code)
	}
}

// TestModeratorsCanStillMakeContact keeps somebody from hiding from the people
// responsible for the server.
func TestModeratorsCanStillMakeContact(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	hidden := registerAndSignIn(t, handler, "amina")

	call(t, handler, http.MethodPut, "/api/me/privacy",
		map[string]any{"directMessages": "nobody"}, hidden)

	hiddenID := userIDOf(t, handler, owner, "amina")

	// The owner completed setup, so they hold the moderation permission.
	recorder := call(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"userIds": []string{hiddenID}}, owner)

	if recorder.Code != http.StatusOK && recorder.Code != http.StatusCreated {
		t.Errorf("a moderator was refused with %d; nobody may put themselves "+
			"beyond contact by the people responsible for the server", recorder.Code)
	}
}

/*
TestSuspensionEndsLiveSessions is the claim 7.10 makes.

A suspension that only stopped the next sign-in would leave somebody working
for as long as their session lasted — which for a device left open is
indefinitely. The flag is checked on every request and the sessions are revoked
as well, so a suspension takes effect on a connection already in use.
*/
func TestSuspensionEndsLiveSessions(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	person := registerAndSignIn(t, handler, "amina")
	targetID := userIDOf(t, handler, owner, "amina")

	// Working before.
	before := call(t, handler, http.MethodGet, "/api/me", nil, person)
	if before.Code != http.StatusOK {
		t.Fatalf("the session did not work before suspension: %d", before.Code)
	}

	suspended := call(t, handler, http.MethodPost,
		"/api/admin/users/"+targetID+"/suspend",
		map[string]any{"reason": "Kuvunja sheria za seva"}, owner)
	if suspended.Code != http.StatusNoContent {
		t.Fatalf("suspending returned %d: %s", suspended.Code, truncate(suspended.Body.String()))
	}

	// The same session, immediately after.
	after := call(t, handler, http.MethodGet, "/api/me", nil, person)
	if after.Code == http.StatusOK {
		t.Error("a suspended account's existing session still worked")
	}

	// And they cannot sign in again.
	again := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]any{
		"username": "amina", "password": testPassword,
	}, "")
	if again.Code == http.StatusOK {
		t.Error("a suspended account signed in again")
	}
}

// TestSuspensionRequiresAReason keeps the decision reviewable.
func TestSuspensionRequiresAReason(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	registerAndSignIn(t, handler, "amina")
	targetID := userIDOf(t, handler, owner, "amina")

	recorder := call(t, handler, http.MethodPost,
		"/api/admin/users/"+targetID+"/suspend", map[string]any{}, owner)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("suspending without a reason returned %d, expected 400", recorder.Code)
	}
}

/*
TestSuspendingYourselfIsRefused prevents a mistake nobody recovers from.

The server's own administrator suspending themselves would lock them out of the
server with no way back except database access.
*/
func TestSuspendingYourselfIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	ownerID := userIDOf(t, handler, owner, "founder")

	recorder := call(t, handler, http.MethodPost,
		"/api/admin/users/"+ownerID+"/suspend",
		map[string]any{"reason": "Kwa bahati mbaya"}, owner)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("suspending yourself returned %d, expected it to be refused",
			recorder.Code)
	}

	// And they are still working.
	still := call(t, handler, http.MethodGet, "/api/me", nil, owner)
	if still.Code != http.StatusOK {
		t.Error("the administrator locked themselves out")
	}
}

// TestLiftingASuspensionRestoresTheAccount covers the reversal.
//
// Sessions are not restored: somebody returning signs in again, rather than a
// device they may have lost access to coming back with them.
func TestLiftingASuspensionRestoresTheAccount(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	registerAndSignIn(t, handler, "amina")
	targetID := userIDOf(t, handler, owner, "amina")

	call(t, handler, http.MethodPost, "/api/admin/users/"+targetID+"/suspend",
		map[string]any{"reason": "Kwa muda"}, owner)

	lifted := call(t, handler, http.MethodDelete,
		"/api/admin/users/"+targetID+"/suspend", nil, owner)
	if lifted.Code != http.StatusNoContent {
		t.Fatalf("lifting returned %d: %s", lifted.Code, truncate(lifted.Body.String()))
	}

	again := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]any{
		"username": "amina", "password": testPassword,
	}, "")
	if again.Code != http.StatusOK {
		t.Errorf("a reinstated account could not sign in: %d", again.Code)
	}
}
