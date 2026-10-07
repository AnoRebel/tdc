package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

/*
What the roles screen needs beyond granting and assigning: the list with who
holds what, renaming, putting an entry back to saying nothing, who holds a role,
and why somebody may do what they may.
*/

// TestRoleAdministrationIsRefusedToMembers checks every route the screen added
// is guarded, each by its own permission.
func TestRoleAdministrationIsRefusedToMembers(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	member := registerAndSignIn(t, handler, "amina")
	memberRole := roleNamed(t, handler, admin, "Member")
	someone := accountID(t, handler, member)

	for _, forbidden := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/api/admin/roles/" + memberRole, map[string]string{"name": "Member", "description": "x"}},
		{http.MethodDelete, "/api/admin/roles/" + memberRole + "/permissions/channel.post", nil},
		{http.MethodGet, "/api/admin/roles/" + memberRole + "/members", nil},
		{http.MethodGet, "/api/admin/users/" + someone + "/permissions", nil},
	} {
		if recorder := call(t, handler, forbidden.method, forbidden.path, forbidden.body, member); recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: status %d, want %d",
				forbidden.method, forbidden.path, recorder.Code, http.StatusForbidden)
		}
	}
}

// TestTheRoleListSaysWhoHoldsWhatAndWhere is what the list is drawn from.
func TestTheRoleListSaysWhoHoldsWhatAndWhere(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	member := registerAndSignIn(t, handler, "amina")
	room := makeRoom(t, handler, admin, "helpdesk", "public")

	moderator := makeRoleWith(t, handler, admin, "Moderator")
	setEntry(t, handler, admin, moderator, "moderation.review", "", true)
	setEntry(t, handler, admin, moderator, "message.delete.any", room, true)
	mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+moderator+"/members",
		map[string]string{"user": accountID(t, handler, member)}, admin, http.StatusNoContent)

	role := listedRole(t, handler, admin, "Moderator")
	if count, _ := role["memberCount"].(float64); count != 1 {
		t.Errorf("member count %v, want 1", role["memberCount"])
	}

	entries, _ := role["permissions"].([]any)
	var server, channel map[string]any
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		switch entry["permission"] {
		case "moderation.review":
			server = entry
		case "message.delete.any":
			channel = entry
		}
	}
	if server == nil || server["scope"] != "server" || server["granted"] != true {
		t.Errorf("the server-wide grant is listed as %v", server)
	}
	if channel == nil || channel["scope"] != "channel" || channel["scopeId"] != room ||
		channel["scopeName"] != "helpdesk" {
		t.Errorf("the channel grant is listed as %v", channel)
	}

	listed := decode(t, call(t, handler, http.MethodGet,
		"/api/admin/roles/"+moderator+"/members", nil, admin))
	members, _ := listed["members"].([]any)
	if len(members) != 1 || members[0].(map[string]any)["username"] != "amina" {
		t.Errorf("the role's members are %v", members)
	}
}

// TestARoleCanBeRenamedButABuiltInOneKeepsItsName, because the server finds
// Member and Administrator by name.
func TestARoleCanBeRenamedButABuiltInOneKeepsItsName(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	registerAndSignIn(t, handler, "amina")

	helpers := makeRoleWith(t, handler, admin, "Helpers")
	makeRoleWith(t, handler, admin, "Stewards")

	renamed := call(t, handler, http.MethodPatch, "/api/admin/roles/"+helpers,
		map[string]string{"name": "Mentors", "description": "Help newcomers"}, admin)
	if renamed.Code != http.StatusOK || decode(t, renamed)["name"] != "Mentors" {
		t.Fatalf("renaming: status %d, body %s", renamed.Code, renamed.Body)
	}

	if recorder := call(t, handler, http.MethodPatch, "/api/admin/roles/"+helpers,
		map[string]string{"name": "Stewards"}, admin); recorder.Code != http.StatusConflict {
		t.Errorf("taking another role's name: status %d, want 409", recorder.Code)
	}

	memberRole := roleNamed(t, handler, admin, "Member")
	if recorder := call(t, handler, http.MethodPatch, "/api/admin/roles/"+memberRole,
		map[string]string{"name": "Everyone", "description": "x"}, admin); recorder.Code != http.StatusConflict {
		t.Errorf("renaming Member: status %d, want 409", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPatch, "/api/admin/roles/"+memberRole,
		map[string]string{"name": "Member", "description": "Everybody here"}, admin); recorder.Code != http.StatusOK {
		t.Errorf("describing Member: status %d, body %s", recorder.Code, recorder.Body)
	}
	if listedRole(t, handler, admin, "Member")["description"] != "Everybody here" {
		t.Error("Member's description did not change")
	}
}

/*
TestClearingAnEntryLeavesTheQuestionToOtherRoles is the third state.

A denial on one role beats Member's baseline grant, so the holder cannot make a
channel; clearing it — not granting — gives the decision back to Member.
*/
func TestClearingAnEntryLeavesTheQuestionToOtherRoles(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	member := registerAndSignIn(t, handler, "amina")

	quiet := makeRoleWith(t, handler, admin, "Quiet")
	setEntry(t, handler, admin, quiet, "channel.create", "", false)
	mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+quiet+"/members",
		map[string]string{"user": accountID(t, handler, member)}, admin, http.StatusNoContent)

	if recorder := call(t, handler, http.MethodPost, "/api/rooms",
		map[string]string{"slug": "first", "name": "first"}, member); recorder.Code != http.StatusForbidden {
		t.Fatalf("making a channel while denied: status %d, want 403", recorder.Code)
	}

	mustCall(t, handler, http.MethodDelete, "/api/admin/roles/"+quiet+"/permissions/channel.create",
		nil, admin, http.StatusNoContent)

	if recorder := call(t, handler, http.MethodPost, "/api/rooms",
		map[string]string{"slug": "second", "name": "second"}, member); recorder.Code != http.StatusCreated {
		t.Errorf("making a channel once the denial was cleared: status %d, want 201", recorder.Code)
	}
	for _, raw := range listedRole(t, handler, admin, "Quiet")["permissions"].([]any) {
		t.Errorf("the role still says %v", raw)
	}

	// Clearing a channel's entry names the channel.
	room := makeRoom(t, handler, admin, "notices", "public")
	withdraw(t, handler, admin, "channel.post", room)
	memberRole := roleNamed(t, handler, admin, "Member")
	mustCall(t, handler, http.MethodDelete,
		"/api/admin/roles/"+memberRole+"/permissions/channel.post?channel="+room,
		nil, admin, http.StatusNoContent)
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, member, http.StatusOK)
	if recorder := call(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "hello"}, member); recorder.Code != http.StatusCreated {
		t.Errorf("posting once the channel's denial was cleared: status %d", recorder.Code)
	}
}

/*
TestTheLastAdministrationCannotBeClearedOrShared away.

Clearing the grant is a withdrawal and is checked as one. And withdrawing it from
a role every administrator holds, or deleting such a role, is refused however
many administrators there are: the count that matters is of those who would
keep it.
*/
func TestTheLastAdministrationCannotBeClearedOrSharedAway(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	member := registerAndSignIn(t, handler, "amina")
	amina := accountID(t, handler, member)
	administrator := roleNamed(t, handler, admin, "Administrator")

	mustCall(t, handler, http.MethodDelete,
		"/api/admin/roles/"+administrator+"/permissions/server.administer?confirmed=true",
		nil, admin, http.StatusConflict)

	// Two administrators, both through the one role.
	mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+administrator+"/members",
		map[string]string{"user": amina}, admin, http.StatusNoContent)
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+administrator+"/permissions",
		map[string]any{"permission": "server.administer", "granted": false, "confirmed": true},
		admin, http.StatusConflict)
	mustCall(t, handler, http.MethodDelete,
		"/api/admin/roles/"+administrator+"/permissions/server.administer?confirmed=true",
		nil, admin, http.StatusConflict)

	// A denial on another role both hold reaches both.
	everyone := makeRoleWith(t, handler, admin, "Everyone")
	for _, person := range []string{amina, accountID(t, handler, admin)} {
		mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+everyone+"/members",
			map[string]string{"user": person}, admin, http.StatusNoContent)
	}
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+everyone+"/permissions",
		map[string]any{"permission": "server.administer", "granted": false, "confirmed": true},
		admin, http.StatusConflict)

	// A custom role every administrator holds, and nothing else does.
	owners := makeRoleWith(t, handler, admin, "Owners")
	setEntry(t, handler, admin, owners, "server.administer", "", true)
	for _, person := range []string{amina, accountID(t, handler, admin)} {
		mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+owners+"/members",
			map[string]string{"user": person}, admin, http.StatusNoContent)
		mustCall(t, handler, http.MethodDelete,
			"/api/admin/roles/"+administrator+"/members/"+person+"?confirmed=true",
			nil, admin, http.StatusNoContent)
	}
	mustCall(t, handler, http.MethodDelete, "/api/admin/roles/"+owners, nil, admin, http.StatusConflict)

	// Still administered.
	mustCall(t, handler, http.MethodGet, "/api/admin/roles", nil, admin, http.StatusOK)
}

/*
TestWhatSomebodyMayDoSaysWhy is the answer to "did the role do what I meant".

Checked against what the server actually does in the same places, so the
explanation cannot drift from enforcement.
*/
func TestWhatSomebodyMayDoSaysWhy(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	member := registerAndSignIn(t, handler, "amina")
	amina := accountID(t, handler, member)
	room := makeRoom(t, handler, admin, "announcements", "public")
	withdraw(t, handler, admin, "channel.post", room)

	moderator := makeRoleWith(t, handler, admin, "Moderator")
	setEntry(t, handler, admin, moderator, "moderation.review", "", true)
	mustCall(t, handler, http.MethodPost, "/api/admin/roles/"+moderator+"/members",
		map[string]string{"user": amina}, admin, http.StatusNoContent)

	everywhere := explained(t, handler, admin, amina, "")
	if review := everywhere["moderation.review"]; review["allowed"] != true || review["basis"] != "server" ||
		!fromRole(review, "Moderator", true, "server") {
		t.Errorf("moderation.review is explained as %v", review)
	}
	if post := everywhere["channel.post"]; post["allowed"] != true || !fromRole(post, "Member", true, "server") {
		t.Errorf("channel.post everywhere is explained as %v", post)
	}
	if audit := everywhere["server.audit.read"]; audit["allowed"] != false || audit["basis"] != "none" {
		t.Errorf("server.audit.read is explained as %v", audit)
	}

	there := explained(t, handler, admin, amina, room)
	if post := there["channel.post"]; post["allowed"] != false || post["basis"] != "scope" ||
		!fromRole(post, "Member", false, "channel") {
		t.Errorf("channel.post in the announcement channel is explained as %v", post)
	}
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/join", nil, member, http.StatusOK)
	if recorder := call(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"body": "may I?"}, member); recorder.Code != http.StatusForbidden {
		t.Errorf("the explanation said no and the server said %d", recorder.Code)
	}

	founder := explained(t, handler, admin, accountID(t, handler, admin), "")
	if audit := founder["server.audit.read"]; audit["allowed"] != true || audit["basis"] != "administrator" {
		t.Errorf("an administrator's audit access is explained as %v", audit)
	}

	if recorder := call(t, handler, http.MethodGet,
		"/api/admin/users/"+uuid.New().String()+"/permissions", nil, admin); recorder.Code != http.StatusNotFound {
		t.Errorf("explaining nobody: status %d, want 404", recorder.Code)
	}
}

func makeRoleWith(t *testing.T, handler http.Handler, admin, name string) string {
	t.Helper()
	created := call(t, handler, http.MethodPost, "/api/admin/roles", map[string]string{"name": name}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("creating %s: status %d, body %s", name, created.Code, created.Body)
	}
	identifier, _ := decode(t, created)["id"].(string)
	return identifier
}

func setEntry(t *testing.T, handler http.Handler, admin, role, permission, channel string, granted bool) {
	t.Helper()
	body := map[string]any{"permission": permission, "granted": granted}
	if channel != "" {
		body["channel"] = channel
	}
	mustCall(t, handler, http.MethodPut, "/api/admin/roles/"+role+"/permissions", body, admin, http.StatusNoContent)
}

func listedRole(t *testing.T, handler http.Handler, admin, name string) map[string]any {
	t.Helper()
	for _, raw := range decodeList(t, call(t, handler, http.MethodGet, "/api/admin/roles", nil, admin)) {
		if role, _ := raw.(map[string]any); role["name"] == name {
			return role
		}
	}
	t.Fatalf("no role called %s", name)
	return nil
}

// explained is somebody's permissions keyed by name.
func explained(t *testing.T, handler http.Handler, admin, user, channel string) map[string]map[string]any {
	t.Helper()
	path := "/api/admin/users/" + user + "/permissions"
	if channel != "" {
		path += "?channel=" + channel
	}
	recorder := call(t, handler, http.MethodGet, path, nil, admin)
	if recorder.Code != http.StatusOK {
		t.Fatalf("explaining: status %d, body %s", recorder.Code, recorder.Body)
	}
	byName := map[string]map[string]any{}
	permissions, _ := decode(t, recorder)["permissions"].([]any)
	for _, raw := range permissions {
		entry, _ := raw.(map[string]any)
		name, _ := entry["name"].(string)
		byName[name] = entry
	}
	return byName
}

func fromRole(permission map[string]any, role string, granted bool, scope string) bool {
	sources, _ := permission["sources"].([]any)
	for _, raw := range sources {
		source, _ := raw.(map[string]any)
		if source["roleName"] == role && source["granted"] == granted && source["scope"] == scope {
			return true
		}
	}
	return false
}
