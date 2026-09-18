package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

/*
Roles over HTTP.

The service beneath has had every safety check since the authorization work.
What is tested here is the thing that was missing: that any of it can be
reached, and that an ordinary member can do the first thing anybody joining a
community wants to do.
*/

/*
TestSomebodyWhoIsNotTheFirstAdministratorCanMakeAChannel.

The whole reason this task exists. An account with no role holds no permissions,
channel.create is enforced, and so on a fresh server only the first
administrator could make a channel — which for a community of hundreds is one
person doing setup by hand for everybody.
*/
func TestSomebodyWhoIsNotTheFirstAdministratorCanMakeAChannel(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	member := signIn(t, handler, "amina", testPassword)

	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]string{
		"slug": "introductions", "name": "Introductions",
	}, member)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("an ordinary member could not make a channel: status %d, body %s",
			recorder.Code, recorder.Body.String())
	}
}

/*
TestAnOperatorCanTakeThatAway.

The default role is an ordinary role, so a server where only administrators make
channels is one withdrawal away rather than a fork.
*/
func TestAnOperatorCanTakeThatAway(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}
	member := signIn(t, handler, "amina", testPassword)

	memberRole := roleNamed(t, handler, admin, "Member")

	if recorder := call(t, handler, http.MethodPut,
		"/api/admin/roles/"+memberRole+"/permissions",
		map[string]any{"permission": "channel.create", "granted": false},
		admin); recorder.Code != http.StatusNoContent {
		t.Fatalf("withdrawing: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	if recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]string{
		"slug": "after", "name": "After",
	}, member); recorder.Code != http.StatusForbidden {
		t.Errorf("after withdrawing: status %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

/*
TestARoleCanBeMadeAndHandedOut.

The other half: an operator can make a moderator without giving them everything,
which until now meant making them an administrator or nothing.
*/
func TestARoleCanBeMadeAndHandedOut(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}
	member := signIn(t, handler, "amina", testPassword)

	/*
		Permissions an ordinary member does not have.

		Every one of these routes checked, not just the first: they are guarded
		by two different permissions — role.manage for deciding what a role is,
		role.assign for deciding who holds it — and a route that lost its guard
		would be a member handing themselves whatever they liked.
	*/
	for _, forbidden := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/admin/roles", nil},
		{http.MethodPost, "/api/admin/roles", map[string]string{"name": "Sneaky"}},
		{http.MethodPut, "/api/admin/roles/" + uuid.New().String() + "/permissions",
			map[string]any{"permission": "server.administer", "granted": true}},
		{http.MethodPost, "/api/admin/roles/" + uuid.New().String() + "/members",
			map[string]string{"user": uuid.New().String()}},
		{http.MethodDelete, "/api/admin/roles/" + uuid.New().String(), nil},
		{http.MethodDelete,
			"/api/admin/roles/" + uuid.New().String() + "/members/" + uuid.New().String(), nil},
	} {
		if recorder := call(t, handler, forbidden.method, forbidden.path, forbidden.body, member); recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: status %d, want %d",
				forbidden.method, forbidden.path, recorder.Code, http.StatusForbidden)
		}
	}

	created := call(t, handler, http.MethodPost, "/api/admin/roles", map[string]string{
		"name": "Moderator", "description": "Keeps the peace",
	}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("creating a role: status %d, body %s", created.Code, created.Body.String())
	}
	roleID, _ := decode(t, created)["id"].(string)

	// It starts with nothing, which is what makes each grant a decision.
	if recorder := call(t, handler, http.MethodPut,
		"/api/admin/roles/"+roleID+"/permissions",
		map[string]any{"permission": "user.invite", "granted": true},
		admin); recorder.Code != http.StatusNoContent {
		t.Fatalf("granting: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	if recorder := call(t, handler, http.MethodPost, "/api/admin/roles/"+roleID+"/members",
		map[string]string{"user": accountID(t, handler, member)},
		admin); recorder.Code != http.StatusNoContent {
		t.Fatalf("assigning: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	// And the permission is theirs.
	held := decode(t, call(t, handler, http.MethodGet, "/api/me/permissions", nil, member))
	allowed, _ := held["permissions"].([]any)
	if !containsString(allowed, "user.invite") {
		t.Errorf("the assigned permission is not held: %v", allowed)
	}
}

/*
TestTheServerCannotBeLeftWithNoAdministrator.

The service has always refused this; what is new is that there is now a way to
try, which makes the refusal worth testing from outside.
*/
func TestTheServerCannotBeLeftWithNoAdministrator(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	administrator := roleNamed(t, handler, admin, "Administrator")

	recorder := call(t, handler, http.MethodPut,
		"/api/admin/roles/"+administrator+"/permissions",
		map[string]any{"permission": "server.administer", "granted": false, "confirmed": true},
		admin)
	if recorder.Code != http.StatusConflict {
		t.Errorf("withdrawing the last administration: status %d, want %d",
			recorder.Code, http.StatusConflict)
	}
}

// TestABuiltInRoleCannotBeDeleted, because the accounts holding it would be
// left holding nothing.
func TestABuiltInRoleCannotBeDeleted(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	// Register somebody, so the default role exists to be deleted.
	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}

	memberRole := roleNamed(t, handler, admin, "Member")

	if recorder := call(t, handler, http.MethodDelete,
		"/api/admin/roles/"+memberRole, nil, admin); recorder.Code != http.StatusConflict {
		t.Errorf("deleting a built-in role: status %d, want %d", recorder.Code, http.StatusConflict)
	}
}

// roleNamed finds a role by its name, which is how a test names one without
// knowing the identifier the server chose.
func roleNamed(t *testing.T, handler http.Handler, token, name string) string {
	t.Helper()

	listed := decodeList(t, call(t, handler, http.MethodGet, "/api/admin/roles", nil, token))
	for _, entry := range listed {
		role, _ := entry.(map[string]any)
		if role["name"] == name {
			identifier, _ := role["id"].(string)
			return identifier
		}
	}

	t.Fatalf("no role called %q; the server has %v", name, listed)
	return ""
}

// accountID is somebody's own identifier, as they see it.
func accountID(t *testing.T, handler http.Handler, token string) string {
	t.Helper()

	identifier, _ := decode(t, call(t, handler, http.MethodGet, "/api/me", nil, token))["id"].(string)
	if identifier == "" {
		t.Fatal("no account identifier")
	}
	return identifier
}

func containsString(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}
