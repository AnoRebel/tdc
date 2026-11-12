package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

/*
What the member list discloses over HTTP.

The service tests prove a withheld detail is never read from the database.
These prove the response says so correctly: a withheld detail is an absent
field, not a field holding false or an empty string, because a client showing
"offline" for somebody who withheld their presence has disclosed exactly what
they withheld.
*/

// setPrivacy changes somebody's own disclosure settings.
func setPrivacy(t *testing.T, handler http.Handler, token string, body map[string]any) {
	t.Helper()
	recorder := call(t, handler, http.MethodPut, "/api/me/privacy", body, token)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("setting privacy returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

// memberNamed returns one person's raw JSON object from a member list.
func memberNamed(t *testing.T, handler http.Handler, token, roomID, username string) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/members", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("member list returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var members []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &members); err != nil {
		t.Fatalf("parsing the member list: %v", err)
	}
	for _, member := range members {
		if member["username"] == username {
			return member
		}
	}
	t.Fatalf("%s is not in the member list", username)
	return nil
}

/*
A withheld detail is absent from the JSON, not present and empty.

The distinction is the whole requirement. A field sent as false is an answer to
the question the person declined to answer, and a client cannot tell it from
somebody who is genuinely offline.
*/
func TestAWithheldDetailIsAbsentFromTheResponse(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	guest := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, guest, roomID)

	setPrivacy(t, handler, guest, map[string]any{
		"showsPresence": false,
		"showsStatus":   false,
		"showsLastSeen": false,
	})

	member := memberNamed(t, handler, owner, roomID, "juma")

	for _, field := range []string{"online", "status", "statusText", "lastSeenAt"} {
		if value, present := member[field]; present {
			t.Errorf("%s was withheld but the response carries it as %v", field, value)
		}
	}

	// The name and address are not the person's to withhold: without them the
	// list cannot attribute a message or open a conversation.
	if member["displayName"] == nil && member["username"] == nil {
		t.Error("the response withheld the name, which is never optional")
	}
	if member["address"] == nil {
		t.Error("the response withheld the address, which is never optional")
	}
}

/*
A disclosed detail is present.

The counterpart, and the one that catches a change which omits everything: a
response that withheld every field from everybody would pass the test above
while breaking the member list.
*/
func TestADisclosedDetailIsPresent(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	guest := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, guest, roomID)

	member := memberNamed(t, handler, owner, roomID, "juma")
	if _, present := member["online"]; !present {
		t.Error("somebody who withheld nothing has no presence in the response")
	}
}

/*
Withholding one detail leaves the others.

Three separate decisions, and a client that sends one must not disturb the
other two — which is why the request carries pointers and reads before writing.
*/
func TestWithholdingOneDetailLeavesTheOthers(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	guest := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, guest, roomID)

	setPrivacy(t, handler, guest, map[string]any{"showsLastSeen": false})

	member := memberNamed(t, handler, owner, roomID, "juma")
	if _, present := member["lastSeenAt"]; present {
		t.Error("last seen was withheld and is in the response")
	}
	if _, present := member["online"]; !present {
		t.Error("withholding last seen also withheld presence")
	}

	/*
		And a later request naming a different disclosure setting does not
		restore it.

		This is the case the pointers exist for. A request carrying only
		showsStatus must leave showsLastSeen as it was — writing all three from
		the request would silently undo a decision the person made earlier, and
		a client that knows about two of the three settings would withdraw the
		third on every save.
	*/
	setPrivacy(t, handler, guest, map[string]any{"showsStatus": false})

	/*
		Read back from the settings themselves rather than the member list.

		The member list omits a last-seen time for somebody who has never
		connected regardless of the setting, so it cannot tell a withheld
		detail from an absent one — asserting on it here would pass whether or
		not the setting survived.
	*/
	recorder := call(t, handler, http.MethodGet, "/api/me/privacy", nil, guest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading own privacy returned %d", recorder.Code)
	}
	var settings map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &settings); err != nil {
		t.Fatalf("parsing: %v", err)
	}

	if settings["showsLastSeen"] != false {
		t.Error("changing one disclosure setting restored another that was withheld")
	}
	if settings["showsStatus"] != false {
		t.Error("the setting that was just changed did not take effect")
	}
	if settings["showsPresence"] != true {
		t.Error("changing two settings withheld a third that was never touched")
	}

	after := memberNamed(t, handler, owner, roomID, "juma")
	if _, present := after["status"]; present {
		t.Error("the withheld status is still in the member list")
	}
	if _, present := after["online"]; !present {
		t.Error("presence was never withheld but is absent from the member list")
	}
}

/*
Somebody's own settings come back to them.

The screen on which these are changed has to show what they currently are, so
this is the one response that withholds nothing.
*/
func TestSomebodyCanReadTheirOwnSettings(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	guest := registerAndSignIn(t, handler, "juma")
	setPrivacy(t, handler, guest, map[string]any{"showsStatus": false})

	recorder := call(t, handler, http.MethodGet, "/api/me/privacy", nil, guest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading own privacy returned %d", recorder.Code)
	}

	var settings map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &settings); err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if settings["showsStatus"] != false {
		t.Errorf("showsStatus came back as %v, want false", settings["showsStatus"])
	}
	if settings["showsPresence"] != true {
		t.Errorf("showsPresence came back as %v, want true", settings["showsPresence"])
	}
}

/*
A non-member cannot enumerate a private channel's members.

Refused as missing rather than forbidden, so the answer does not confirm that
the channel exists — which is the difference between a private channel and a
channel whose contents are private.
*/
func TestANonMemberCannotEnumerateAPrivateChannel(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "private-plans", "private")

	stranger := registerAndSignIn(t, handler, "juma")

	recorder := call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/members", nil, stranger)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("a stranger asking for a private channel's members got %d, want 404: %s",
			recorder.Code, recorder.Body.String())
	}
}

/*
An ordinary member is never told what somebody withheld.

"This field is withheld" answers the question the person declined to answer:
knowing somebody hides their last-seen time is itself information about them.
Only a moderator being shown the values gets the list, so they can tell an
exception from ordinary information.
*/
func TestAnOrdinaryMemberIsNotToldWhatWasWithheld(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	guest := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, guest, roomID)
	setPrivacy(t, handler, guest, map[string]any{"showsLastSeen": false})

	// Asked for by a member who holds no moderation permission.
	member := memberNamed(t, handler, guest, roomID, "juma")
	if withheld, present := member["withheld"]; present {
		t.Errorf("an ordinary member was told what is withheld: %v", withheld)
	}
}

/*
A moderator looking past somebody's settings is recorded.

A privacy setting a moderator can quietly step around is not a privacy setting.
The access is allowed — moderation needs it — so what makes the exception
answerable is the record, which the person it was used against can be shown.
*/
func TestAModeratorsViewOfWithheldDetailsIsRecorded(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	guest := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, guest, roomID)
	setPrivacy(t, handler, guest, map[string]any{"showsLastSeen": false})

	// The administrator from setup holds the moderation permission, and asks
	// to look as a moderator rather than as an ordinary member.
	recorder := call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/members?moderating=true", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("a moderator's member list returned %d", recorder.Code)
	}

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=20", nil, owner))

	found := false
	for _, entry := range entries {
		if entry.Action == "user.withheld_details.viewed" {
			found = true
		}
	}
	if !found {
		t.Error("a moderator saw what somebody withheld and nothing recorded it")
	}
}

/*
Opening a member list is not moderating.

Without asking, a moderator sees exactly what anybody else sees — and collects
no audit entry saying they looked past a setting, because they did not. An
entry for every member list a moderator opens would bury the ones that matter.
*/
func TestAModeratorWhoDidNotAskSeesWhatEverybodySees(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	guest := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, guest, roomID)
	setPrivacy(t, handler, guest, map[string]any{"showsPresence": false})

	member := memberNamed(t, handler, owner, roomID, "juma")
	if _, present := member["online"]; present {
		t.Error("a moderator who did not ask to moderate was shown a withheld detail")
	}
	if _, present := member["withheld"]; present {
		t.Error("a moderator who did not ask to moderate was told what is withheld")
	}

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=20", nil, owner))
	for _, entry := range entries {
		if entry.Action == "user.withheld_details.viewed" {
			t.Error("opening an ordinary member list was recorded as moderation")
		}
	}
}

/*
Asking to moderate without the permission changes nothing.

The query parameter is a request, not an assertion. Somebody who appends it
without holding the permission sees the ordinary list — otherwise the setting
would be defeated by anybody who read the documentation.
*/
func TestAskingToModerateWithoutThePermissionDisclosesNothing(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	guest := registerAndSignIn(t, handler, "juma")
	joinRoom(t, handler, guest, roomID)
	setPrivacy(t, handler, guest, map[string]any{"showsPresence": false})

	curious := registerAndSignIn(t, handler, "neema")
	joinRoom(t, handler, curious, roomID)

	recorder := call(t, handler, http.MethodGet,
		"/api/rooms/"+roomID+"/members?moderating=true", nil, curious)
	if recorder.Code != http.StatusOK {
		t.Fatalf("member list returned %d", recorder.Code)
	}

	var members []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &members); err != nil {
		t.Fatalf("parsing: %v", err)
	}
	for _, member := range members {
		if member["username"] != "juma" {
			continue
		}
		if _, present := member["online"]; present {
			t.Error("asking to moderate without the permission disclosed a withheld detail")
		}
		if _, present := member["withheld"]; present {
			t.Error("asking to moderate without the permission revealed what is withheld")
		}
	}
}
