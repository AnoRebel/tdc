package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

/*
The directory's profiles, over HTTP.

Most of what is tested here is non-disclosure: that a profile says exactly what
its owner chose to whoever is asking, and that every way of not being allowed
to see something looks the same from outside. The happy paths are tested too,
because a check that refuses everything passes every non-disclosure test.
*/

// withDirectory builds a server, sets it up, and turns the directory on.
// Returns the handler and the founder's token.
func withDirectory(t *testing.T) (http.Handler, string) {
	t.Helper()
	handler := newTestAPI(t)
	cleanDirectory(t)
	founder := completeSetup(t, handler)
	setFeature(t, handler, founder, "directory", true)
	return handler, founder
}

/*
cleanDirectory removes the directory rows the shared harness does not.

Profiles cascade from their owners, which the harness deletes; a company does
not, since it outlives the person who created it, so it would otherwise carry
into the next test and take its address.
*/
func cleanDirectory(t *testing.T) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM company_profiles",
		// The skills a test proposed or approved; the seeded vocabulary was
		// never reviewed and stays.
		"DELETE FROM skills WHERE status = 'proposed' OR reviewed_at IS NOT NULL",
	} {
		if _, err := poolForTest.Exec(context.Background(), statement); err != nil {
			t.Fatalf("cleaning the directory: %v", err)
		}
	}
}

// saveProfile writes somebody's own profile and returns the response body.
func saveProfile(t *testing.T, handler http.Handler, token string, profile map[string]any) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodPut, "/api/directory/me", profile, token)
	if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
		t.Fatalf("saving a profile returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

// viewProfile reads somebody's profile as the given caller, "" for somebody
// not signed in.
func viewProfile(t *testing.T, handler http.Handler, token, username string) (int, map[string]any) {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/directory/profiles/"+username, nil, token)
	if recorder.Code != http.StatusOK {
		return recorder.Code, nil
	}
	return recorder.Code, decode(t, recorder)
}

// grantRole gives somebody a new role holding the named permissions.
func grantRole(t *testing.T, handler http.Handler, admin, holder string, permissions ...string) {
	t.Helper()

	created := call(t, handler, http.MethodPost, "/api/admin/roles", map[string]string{
		"name": "role-" + uuid.NewString()[:8],
	}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("creating a role: %d %s", created.Code, created.Body.String())
	}
	roleID, _ := decode(t, created)["id"].(string)

	for _, permission := range permissions {
		if recorder := call(t, handler, http.MethodPut, "/api/admin/roles/"+roleID+"/permissions",
			map[string]any{"permission": permission, "granted": true}, admin); recorder.Code != http.StatusNoContent {
			t.Fatalf("granting %s: %d %s", permission, recorder.Code, recorder.Body.String())
		}
	}
	if recorder := call(t, handler, http.MethodPost, "/api/admin/roles/"+roleID+"/members",
		map[string]string{"user": accountID(t, handler, holder)}, admin); recorder.Code != http.StatusNoContent {
		t.Fatalf("assigning a role: %d %s", recorder.Code, recorder.Body.String())
	}
}

/*
The directory does not exist until an operator turns it on.

Not an empty directory — no directory: the same answer as any path this server
has never heard of, so a server that only wants chat gives nobody a reason to
think it has one.
*/
func TestTheDirectoryIsAbsentUntilTurnedOn(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/directory/me", map[string]any{"bio": "hello"}, founder)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("saving a profile with the directory off returned %d", recorder.Code)
	}

	setFeature(t, handler, founder, "directory", true)
	if recorder := call(t, handler, http.MethodPut, "/api/directory/me",
		map[string]any{"bio": "hello"}, founder); recorder.Code != http.StatusCreated {
		t.Fatalf("saving a profile with the directory on returned %d: %s", recorder.Code, recorder.Body.String())
	}

	// And off again hides it at once, without touching what is stored.
	setFeature(t, handler, founder, "directory", false)
	if recorder := call(t, handler, http.MethodGet, "/api/directory/me", nil, founder); recorder.Code != http.StatusNotFound {
		t.Fatalf("reading a profile with the directory off again returned %d", recorder.Code)
	}
	setFeature(t, handler, founder, "directory", true)
	own := decode(t, call(t, handler, http.MethodGet, "/api/directory/me", nil, founder))
	if profile, _ := own["profile"].(map[string]any); profile["bio"] != "hello" {
		t.Errorf("turning the directory off and on lost the profile: %v", own)
	}
}

// Somebody who never made a profile has none, and everything else works.
func TestAnAccountWithNoProfileIsComplete(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")

	if recorder := call(t, handler, http.MethodGet, "/api/directory/me", nil, juma); recorder.Code != http.StatusNotFound {
		t.Fatalf("a fresh account's own profile returned %d, want 404", recorder.Code)
	}

	// Messaging does not wait on a profile.
	roomID := makeRoom(t, handler, founder, "general", "public")
	joinRoom(t, handler, juma, roomID)
	if recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]string{"body": "habari"}, juma); recorder.Code != http.StatusCreated {
		t.Fatalf("somebody with no profile could not post: %d %s", recorder.Code, recorder.Body.String())
	}
}

/*
A profile saved without choosing a contact visibility publishes no contact
details — to members, to strangers, or anybody but its owner.
*/
func TestContactDetailsDefaultToHidden(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")

	saved := saveProfile(t, handler, juma, map[string]any{
		"headline":          "Backend developer",
		"visibility":        "public",
		"detailsVisibility": "public",
		"contactEmail":      "juma@example.com",
		"contactPhone":      "+255 700 000 000",
	})
	if profile, _ := saved["profile"].(map[string]any); profile["contactVisibility"] != "hidden" {
		t.Errorf("the contact visibility defaulted to %v, want hidden", profile["contactVisibility"])
	}

	for viewer, token := range map[string]string{"a member": amina, "a stranger": ""} {
		status, view := viewProfile(t, handler, token, "juma")
		if status != http.StatusOK {
			t.Fatalf("%s could not see a public profile: %d", viewer, status)
		}
		if view["headline"] != "Backend developer" {
			t.Errorf("%s did not see the headline the owner made public: %v", viewer, view)
		}
		for _, field := range []string{"contactEmail", "contactPhone", "contactUrl"} {
			if value, present := view[field]; present {
				t.Errorf("%s was shown %s = %v, which defaults to hidden", viewer, field, value)
			}
		}
	}

	// The owner still sees their own.
	own := decode(t, call(t, handler, http.MethodGet, "/api/directory/me", nil, juma))
	if profile, _ := own["profile"].(map[string]any); profile["contactEmail"] != "juma@example.com" {
		t.Errorf("the owner cannot see their own contact details: %v", own)
	}
}

/*
Every way of not being allowed to see a profile looks the same.

No such person, a person with no profile, a hidden profile, and a members-only
profile asked for by a stranger: one status and one body, byte for byte. Any
difference is an answer to the question the hidden profile declines.
*/
func TestEveryRefusalOfAProfileIsIdentical(t *testing.T) {
	handler, _ := withDirectory(t)
	amina := registerAndSignIn(t, handler, "amina")
	registerAndSignIn(t, handler, "noprofile")
	hidden := registerAndSignIn(t, handler, "hiddenone")
	membersOnly := registerAndSignIn(t, handler, "membersonly")

	saveProfile(t, handler, hidden, map[string]any{"visibility": "hidden", "bio": "secret"})
	saveProfile(t, handler, membersOnly, map[string]any{"visibility": "members", "bio": "for members"})

	type refusal struct {
		status int
		body   string
	}
	ask := func(token, username string) refusal {
		recorder := call(t, handler, http.MethodGet, "/api/directory/profiles/"+username, nil, token)
		return refusal{recorder.Code, recorder.Body.String()}
	}

	reference := ask(amina, "nobody-by-this-name")
	if reference.status != http.StatusNotFound {
		t.Fatalf("a username that does not exist returned %d", reference.status)
	}
	for description, got := range map[string]refusal{
		"a person with no profile":             ask(amina, "noprofile"),
		"a hidden profile":                     ask(amina, "hiddenone"),
		"a members-only profile to a stranger": ask("", "membersonly"),
		"no such person, asked by a stranger":  ask("", "nobody-by-this-name"),
	} {
		if got != reference {
			t.Errorf("%s answered %d %q; no such person answers %d %q",
				description, got.status, got.body, reference.status, reference.body)
		}
	}

	// The counterpart: a members-only profile is visible to a member.
	if status, _ := viewProfile(t, handler, amina, "membersonly"); status != http.StatusOK {
		t.Errorf("a member could not see a members-only profile: %d", status)
	}
}

/*
A hidden section is absent from the response, not present and empty.

And it is absent from every part of the response: the portfolio and companies
belong to the details, so hiding the details hides them too.
*/
func TestAHiddenSectionIsAbsent(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")

	saveProfile(t, handler, juma, map[string]any{
		"visibility":        "members",
		"detailsVisibility": "hidden",
		"contactVisibility": "members",
		"bio":               "private bio",
		"contactEmail":      "juma@example.com",
		"portfolio":         []map[string]any{{"title": "Site", "url": "https://example.com"}},
	})
	// Public on the account, and still withheld here: the profile's hidden
	// details are its ceiling.
	setLocation(t, handler, juma, "Arusha", "public")

	// A company juma agreed to be listed by, which is part of what the
	// profile says and so part of what hiding the details hides.
	call(t, handler, http.MethodPost, "/api/directory/companies",
		map[string]any{"slug": "kilima", "name": "Kilima", "visibility": "public"}, amina)
	call(t, handler, http.MethodPost, "/api/directory/companies/kilima/members",
		map[string]string{"username": "juma"}, amina)
	if recorder := call(t, handler, http.MethodPost, "/api/directory/me/companies/kilima/accept", nil, juma); recorder.Code != http.StatusNoContent {
		t.Fatalf("accepting returned %d", recorder.Code)
	}

	status, view := viewProfile(t, handler, amina, "juma")
	if status != http.StatusOK {
		t.Fatalf("a member could not see a members profile: %d", status)
	}
	for _, field := range []string{"bio", "location", "headline", "experience", "portfolio", "companies"} {
		if value, present := view[field]; present {
			t.Errorf("%s belongs to hidden details and was sent as %v", field, value)
		}
	}
	if view["contactEmail"] != "juma@example.com" {
		t.Errorf("the contact details were made visible to members and were not shown: %v", view)
	}
	if strings.Contains(mustJSON(t, view), "private bio") {
		t.Error("the hidden bio appears somewhere in the response")
	}
}

/*
A section is never more visible than its profile.

Contact details marked public on a members-only profile are not public: a
stranger cannot see the profile at all, and so cannot see anything in it.
*/
func TestASectionIsNeverMoreVisibleThanItsProfile(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")

	saveProfile(t, handler, juma, map[string]any{
		"visibility":        "members",
		"detailsVisibility": "public",
		"contactVisibility": "public",
		"contactEmail":      "juma@example.com",
	})
	if status, _ := viewProfile(t, handler, "", "juma"); status != http.StatusNotFound {
		t.Errorf("a stranger saw a members-only profile whose sections were public: %d", status)
	}
}

// A private server's directory is as undiscoverable as the server.
func TestAStrangerCannotBrowseAPrivateServersDirectory(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	saveProfile(t, handler, juma, map[string]any{"visibility": "public", "detailsVisibility": "public"})

	if status, _ := viewProfile(t, handler, "", "juma"); status != http.StatusOK {
		t.Fatalf("a stranger could not see a public profile on a public server: %d", status)
	}

	if recorder := call(t, handler, http.MethodPut, "/api/admin/settings/server.visibility",
		map[string]string{"value": "private"}, founder); recorder.Code != http.StatusNoContent {
		t.Fatalf("making the server private: %d %s", recorder.Code, recorder.Body.String())
	}
	if status, _ := viewProfile(t, handler, "", "juma"); status != http.StatusNotFound {
		t.Errorf("a stranger saw a public profile on a private server: %d", status)
	}
}

// Links a stranger will click are web links and nothing else.
func TestAPortfolioLinkMustBeAWebLink(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")

	for _, link := range []string{"javascript:alert(1)", "data:text/html,hi", "ftp://example.com", "example.com"} {
		recorder := call(t, handler, http.MethodPut, "/api/directory/me", map[string]any{
			"portfolio": []map[string]any{{"title": "x", "url": link}},
		}, juma)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("a portfolio link %q was accepted: %d", link, recorder.Code)
		}
	}
}

/*
Editing an item keeps its identity; removing it removes it.

The identifier is what a verification will name, so an edit that silently
replaced the item with a new one would be indistinguishable from deleting it.
*/
func TestPortfolioItemsKeepTheirIdentityWhenEdited(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")

	first := saveProfile(t, handler, juma, map[string]any{
		"portfolio": []map[string]any{
			{"title": "One", "url": "https://one.example"},
			{"title": "Two", "url": "https://two.example"},
		},
	})
	items := first["profile"].(map[string]any)["portfolio"].([]any)
	one := items[0].(map[string]any)["id"].(string)

	second := saveProfile(t, handler, juma, map[string]any{
		"portfolio": []map[string]any{{"id": one, "title": "One, edited", "url": "https://one.example"}},
	})
	after := second["profile"].(map[string]any)["portfolio"].([]any)
	if len(after) != 1 {
		t.Fatalf("the item left out was not removed: %v", after)
	}
	if kept := after[0].(map[string]any); kept["id"] != one || kept["title"] != "One, edited" {
		t.Errorf("the edited item lost its identity or its edit: %v", kept)
	}
}

// Somebody else's item identifier claims nothing.
func TestAnotherPersonsPortfolioItemCannotBeClaimed(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")

	saved := saveProfile(t, handler, juma, map[string]any{
		"portfolio": []map[string]any{{"title": "Juma's", "url": "https://juma.example"}},
	})
	jumas := saved["profile"].(map[string]any)["portfolio"].([]any)[0].(map[string]any)["id"].(string)

	saveProfile(t, handler, amina, map[string]any{
		"portfolio": []map[string]any{{"id": jumas, "title": "Amina's now", "url": "https://amina.example"}},
	})

	own := decode(t, call(t, handler, http.MethodGet, "/api/directory/me", nil, juma))
	item := own["profile"].(map[string]any)["portfolio"].([]any)[0].(map[string]any)
	if item["id"] != jumas || item["title"] != "Juma's" {
		t.Errorf("naming another person's item changed it: %v", item)
	}
}

/*
Nobody but a moderator can change somebody else's profile, and a moderator's
change is recorded with who, why and what — but not the content.
*/
func TestOnlyAModeratorCanChangeAnotherProfileAndItIsAudited(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	moderator := registerAndSignIn(t, handler, "kheri")

	saveProfile(t, handler, juma, map[string]any{"bio": "something abusive", "visibility": "public"})

	change := map[string]any{"bio": "", "visibility": "hidden", "reason": "abusive bio"}
	if recorder := call(t, handler, http.MethodPut, "/api/directory/moderation/profiles/juma", change, amina); recorder.Code != http.StatusForbidden {
		t.Fatalf("an ordinary member changed another profile: %d", recorder.Code)
	}

	grantRole(t, handler, founder, moderator, "directory.profile.manage")

	if recorder := call(t, handler, http.MethodPut, "/api/directory/moderation/profiles/juma",
		map[string]any{"bio": "", "visibility": "hidden"}, moderator); recorder.Code != http.StatusBadRequest {
		t.Errorf("a moderator's change without a reason was accepted: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPut, "/api/directory/moderation/profiles/juma", change, moderator); recorder.Code != http.StatusOK {
		t.Fatalf("a moderator could not change a profile: %d %s", recorder.Code, recorder.Body.String())
	}

	own := decode(t, call(t, handler, http.MethodGet, "/api/directory/me", nil, juma))
	if profile := own["profile"].(map[string]any); profile["bio"] != "" || profile["visibility"] != "hidden" {
		t.Errorf("the moderator's change did not take: %v", profile)
	}

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?action=directory.profile.moderated", nil, founder))
	if len(entries) != 1 {
		t.Fatalf("the moderator's change was recorded %d times, want once", len(entries))
	}
	if entries[0].TargetID != accountID(t, handler, juma) {
		t.Errorf("the entry names %s, not the profile's owner", entries[0].TargetID)
	}
	if !strings.Contains(entries[0].Detail, "abusive bio") || !strings.Contains(entries[0].Detail, "bio") {
		t.Errorf("the entry does not say what changed and why: %s", entries[0].Detail)
	}
	if strings.Contains(entries[0].Detail, "something abusive") {
		t.Error("the audit log copied the content that was removed")
	}
}

/*
Deleting a profile is leaving the directory, not the server.

The account signs in, its session survives, and its messages are where they
were.
*/
func TestDeletingAProfileLeavesTheAccount(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")

	roomID := makeRoom(t, handler, founder, "general", "public")
	joinRoom(t, handler, juma, roomID)
	call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages", map[string]string{"body": "still here"}, juma)

	saveProfile(t, handler, juma, map[string]any{"visibility": "members", "bio": "hello"})
	if recorder := call(t, handler, http.MethodDelete, "/api/directory/me", nil, juma); recorder.Code != http.StatusNoContent {
		t.Fatalf("deleting a profile returned %d", recorder.Code)
	}

	if status, _ := viewProfile(t, handler, amina, "juma"); status != http.StatusNotFound {
		t.Errorf("a deleted profile is still visible: %d", status)
	}
	if recorder := call(t, handler, http.MethodGet, "/api/me", nil, juma); recorder.Code != http.StatusOK {
		t.Errorf("deleting a profile ended the session: %d", recorder.Code)
	}
	signIn(t, handler, "juma", testPassword)

	timeline := decodeTimeline(t, call(t, handler, http.MethodGet, "/api/rooms/"+roomID+"/timeline", nil, founder))
	found := false
	for _, event := range timeline {
		if strings.Contains(mustJSON(t, event), "still here") {
			found = true
		}
	}
	if !found {
		t.Error("deleting a profile removed the person's messages")
	}

	// Deleting twice is not an error somebody has to understand, but it is
	// not a success either: there was nothing to delete.
	if recorder := call(t, handler, http.MethodDelete, "/api/directory/me", nil, juma); recorder.Code != http.StatusNotFound {
		t.Errorf("deleting a profile that does not exist returned %d", recorder.Code)
	}
}

/*
An exported profile can be sent back to recreate what it describes.

Deleted, restored from its own export, and exported again: the two exports
agree on everything the person wrote and chose.
*/
func TestAnExportedProfileRoundTrips(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")

	saveProfile(t, handler, juma, map[string]any{
		"headline": "Backend developer", "bio": "Go and Postgres",
		"experience": "Eight years", "contactEmail": "juma@example.com",
		"visibility": "public", "detailsVisibility": "members", "contactVisibility": "hidden",
		"portfolio": []map[string]any{{"title": "Site", "url": "https://example.com", "description": "Mine"}},
	})

	exportOnce := func() map[string]any {
		recorder := call(t, handler, http.MethodGet, "/api/directory/me/export", nil, juma)
		if recorder.Code != http.StatusOK {
			t.Fatalf("export returned %d", recorder.Code)
		}
		if !strings.Contains(recorder.Header().Get("Content-Disposition"), "attachment") {
			t.Error("the export is not offered as a download")
		}
		return decode(t, recorder)
	}

	first := exportOnce()
	if first["format"] != "tdc.directory.profile/1" {
		t.Errorf("the export does not name its format: %v", first["format"])
	}
	// The export holds what was written — without this, a save that dropped
	// a field would round-trip perfectly, having lost it both times.
	exported := first["profile"].(map[string]any)
	for field, want := range map[string]any{
		"headline": "Backend developer", "bio": "Go and Postgres",
		"experience": "Eight years", "contactEmail": "juma@example.com", "visibility": "public",
	} {
		if exported[field] != want {
			t.Errorf("the export has %s = %v, want %v", field, exported[field], want)
		}
	}
	if items, _ := exported["portfolio"].([]any); len(items) != 1 {
		t.Errorf("the export has %d portfolio items, want 1", len(items))
	}

	call(t, handler, http.MethodDelete, "/api/directory/me", nil, juma)
	saveProfile(t, handler, juma, first["profile"].(map[string]any))
	second := exportOnce()

	if !reflect.DeepEqual(withoutIDs(first["profile"]), withoutIDs(second["profile"])) {
		t.Errorf("the profile did not survive a round trip:\n first: %v\nsecond: %v",
			first["profile"], second["profile"])
	}
}

// withoutIDs drops the identifiers a restore necessarily changes.
func withoutIDs(profile any) map[string]any {
	copied := map[string]any{}
	for key, value := range profile.(map[string]any) {
		copied[key] = value
	}
	items := []any{}
	for _, item := range copied["portfolio"].([]any) {
		entry := map[string]any{}
		for key, value := range item.(map[string]any) {
			if key != "id" {
				entry[key] = value
			}
		}
		items = append(items, entry)
	}
	copied["portfolio"] = items
	return copied
}

/*
Having a profile changes no other disclosure.

The directory is a separate decision: creating a public profile must not
loosen, or tighten, anything user_privacy already says.
*/
func TestAProfileLeavesPrivacySettingsAlone(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")

	setPrivacy(t, handler, juma, map[string]any{"showsPresence": false, "directMessages": "nobody"})
	before := call(t, handler, http.MethodGet, "/api/me/privacy", nil, juma).Body.String()

	saveProfile(t, handler, juma, map[string]any{
		"visibility": "public", "detailsVisibility": "public", "contactVisibility": "public",
		"contactEmail": "juma@example.com",
	})
	call(t, handler, http.MethodDelete, "/api/directory/me", nil, juma)

	after := call(t, handler, http.MethodGet, "/api/me/privacy", nil, juma).Body.String()
	if before != after {
		t.Errorf("creating and deleting a profile changed privacy settings:\nbefore %s\n after %s", before, after)
	}
}

/*
A company lists only people who agreed, and leaving ends it on both sides.
*/
func TestACompanyListsOnlyThoseWhoAccepted(t *testing.T) {
	handler, _ := withDirectory(t)
	owner := registerAndSignIn(t, handler, "owner")
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")

	saveProfile(t, handler, juma, map[string]any{"visibility": "members"})

	created := call(t, handler, http.MethodPost, "/api/directory/companies", map[string]any{
		"slug": "kilima", "name": "Kilima Labs", "location": "Arusha", "role": "Founder",
	}, owner)
	if created.Code != http.StatusCreated {
		t.Fatalf("creating a company returned %d: %s", created.Code, created.Body.String())
	}

	if recorder := call(t, handler, http.MethodPost, "/api/directory/companies/kilima/members",
		map[string]string{"username": "juma", "role": "Backend lead"}, owner); recorder.Code != http.StatusAccepted {
		t.Fatalf("inviting returned %d: %s", recorder.Code, recorder.Body.String())
	}

	members := func(token string) []string {
		company := decode(t, call(t, handler, http.MethodGet, "/api/directory/companies/kilima", nil, token))
		names := []string{}
		for _, member := range company["members"].([]any) {
			names = append(names, member.(map[string]any)["username"].(string))
		}
		return names
	}
	profileCompanies := func() []any {
		_, view := viewProfile(t, handler, amina, "juma")
		companies, _ := view["companies"].([]any)
		return companies
	}

	if listed := members(amina); len(listed) != 1 || listed[0] != "owner" {
		t.Errorf("before accepting, the company lists %v", listed)
	}
	if companies := profileCompanies(); len(companies) != 0 {
		t.Errorf("before accepting, the profile references %v", companies)
	}
	// Nobody else can accept on juma's behalf.
	if recorder := call(t, handler, http.MethodPost, "/api/directory/me/companies/kilima/accept", nil, amina); recorder.Code != http.StatusNotFound {
		t.Errorf("somebody not invited accepted: %d", recorder.Code)
	}

	if recorder := call(t, handler, http.MethodPost, "/api/directory/me/companies/kilima/accept", nil, juma); recorder.Code != http.StatusNoContent {
		t.Fatalf("accepting returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if listed := members(amina); len(listed) != 2 {
		t.Errorf("after accepting, the company lists %v", listed)
	}
	if companies := profileCompanies(); len(companies) != 1 {
		t.Errorf("after accepting, the profile references %v", companies)
	}

	if recorder := call(t, handler, http.MethodDelete, "/api/directory/me/companies/kilima", nil, juma); recorder.Code != http.StatusNoContent {
		t.Fatalf("leaving returned %d", recorder.Code)
	}
	if listed := members(amina); len(listed) != 1 {
		t.Errorf("after leaving, the company still lists %v", listed)
	}
	if companies := profileCompanies(); len(companies) != 0 {
		t.Errorf("after leaving, the profile still references %v", companies)
	}
}

// Only a company's administrators change it, and the last cannot walk away.
func TestOnlyACompanysAdministratorsChangeIt(t *testing.T) {
	handler, _ := withDirectory(t)
	owner := registerAndSignIn(t, handler, "owner")
	amina := registerAndSignIn(t, handler, "amina")

	if recorder := call(t, handler, http.MethodPost, "/api/directory/companies",
		map[string]any{"slug": "kilima", "name": "Kilima"}, owner); recorder.Code != http.StatusCreated {
		t.Fatalf("creating a company returned %d: %s", recorder.Code, recorder.Body.String())
	}

	if recorder := call(t, handler, http.MethodPut, "/api/directory/companies/kilima",
		map[string]any{"name": "Taken over"}, amina); recorder.Code != http.StatusForbidden {
		t.Errorf("somebody else changed a company: %d", recorder.Code)
	}

	// Being listed is not administering: an ordinary member who accepted can
	// change nothing either.
	call(t, handler, http.MethodPost, "/api/directory/companies/kilima/members",
		map[string]string{"username": "amina"}, owner)
	call(t, handler, http.MethodPost, "/api/directory/me/companies/kilima/accept", nil, amina)
	if recorder := call(t, handler, http.MethodPut, "/api/directory/companies/kilima",
		map[string]any{"name": "Taken over"}, amina); recorder.Code != http.StatusForbidden {
		t.Errorf("an ordinary member changed a company: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/companies/kilima/members",
		map[string]string{"username": "amina"}, amina); recorder.Code != http.StatusForbidden {
		t.Errorf("somebody else invited to a company: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodDelete, "/api/directory/me/companies/kilima", nil, owner); recorder.Code != http.StatusConflict {
		t.Errorf("the last administrator left a company: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/companies",
		map[string]any{"slug": "kilima", "name": "Another"}, amina); recorder.Code != http.StatusConflict {
		t.Errorf("a second company took a taken address: %d", recorder.Code)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return string(encoded)
}

/*
A moderator may read what an owner hid, and every such read is recorded.

The exception moderation needs, accounted for so that the person it was used
on can see it was.
*/
func TestAModeratorReadingAHiddenProfileIsRecorded(t *testing.T) {
	handler, founder := withDirectory(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	moderator := registerAndSignIn(t, handler, "kheri")
	grantRole(t, handler, founder, moderator, "directory.profile.manage")

	saveProfile(t, handler, juma, map[string]any{"visibility": "hidden", "bio": "hidden bio"})

	if recorder := call(t, handler, http.MethodGet, "/api/directory/moderation/profiles/juma", nil, amina); recorder.Code != http.StatusForbidden {
		t.Errorf("a member read a hidden profile through moderation: %d", recorder.Code)
	}

	recorder := call(t, handler, http.MethodGet, "/api/directory/moderation/profiles/juma", nil, moderator)
	if recorder.Code != http.StatusOK {
		t.Fatalf("a moderator could not read a hidden profile: %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "hidden bio") {
		t.Error("the moderator was not shown what the owner hid")
	}

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?action=directory.profile.viewed", nil, founder))
	if len(entries) != 1 || entries[0].TargetID != accountID(t, handler, juma) {
		t.Errorf("the moderator's read was not recorded against the profile: %v", entries)
	}
}
