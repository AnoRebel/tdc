package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
)

/*
Changing what somebody is called.

The display name is free; the username is half of an address, so the tests
here are mostly about what stops a rename doing harm: proof that it is the
person, the pacing, the old name kept for its owner, the refusal where another
server knows them, and the record left behind.
*/

// execSQL runs a statement against the test database, for the states a test
// cannot reach through the API in reasonable time — a change thirty days ago.
func execSQL(t *testing.T, statement string, args ...any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, db.Config{URL: dbtest.URL(t, "httpapi")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, statement, args...); err != nil {
		t.Fatalf("running %q: %v", statement, err)
	}
}

func me(t *testing.T, handler http.Handler, token string) map[string]any {
	t.Helper()
	return decode(t, mustCall(t, handler, http.MethodGet, "/api/me", nil, token, http.StatusOK))
}

func rename(t *testing.T, handler http.Handler, token, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": username, "password": password}, token)
}

func TestADisplayNameCanBeChangedAndIsCheckedLikeRegistration(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")

	mustCall(t, handler, http.MethodPut, "/api/me/display-name",
		map[string]string{"displayName": "  Amina Juma  "}, token, http.StatusOK)
	if got := me(t, handler, token)["displayName"]; got != "Amina Juma" {
		t.Errorf("display name after the change is %q, want it trimmed and kept", got)
	}

	// Empty is "show my username", as at registration.
	mustCall(t, handler, http.MethodPut, "/api/me/display-name",
		map[string]string{"displayName": ""}, token, http.StatusOK)
	if got := me(t, handler, token)["displayName"]; got != "amina" {
		t.Errorf("an emptied display name is %q, want the username", got)
	}

	for name, value := range map[string]string{
		"a right-to-left override": "Amina\u202eanimA",
		"a zero-width space":       "Ami\u200bna",
		"a control character":      "Amina\x07",
		"a reserved name":          "A.d.m.i.n",
		"too long":                 strings.Repeat("a", 65),
	} {
		recorder := call(t, handler, http.MethodPut, "/api/me/display-name",
			map[string]string{"displayName": value}, token)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("a display name with %s: status %d, want 400", name, recorder.Code)
		}
	}

	// Registration holds to the same rules, so they cannot be dodged by
	// choosing the name at sign-up instead.
	recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "mallory", "password": testPassword, "displayName": "System",
	}, "")
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("registering with a reserved display name: status %d, want 400", recorder.Code)
	}
}

func TestAUsernameChangeKeepsTheOldNameForItsOwner(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")
	id, _ := me(t, handler, token)["id"].(string)

	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "amina.j", "password": testPassword}, token, http.StatusOK)

	after := me(t, handler, token)
	if after["username"] != "amina.j" {
		t.Fatalf("username after the change is %v", after["username"])
	}
	former, _ := after["formerAddresses"].([]any)
	if len(former) != 1 || !strings.HasPrefix(former[0].(string), "amina@") {
		t.Errorf("the person is not told their former address, so their old messages read as somebody else's: %v", former)
	}

	// The new name signs in; so does the old one, which is still theirs.
	signIn(t, handler, "amina.j", testPassword)
	signIn(t, handler, "amina", testPassword)

	// Nobody else may take it while it is held, by renaming or by registering.
	other := registerAndSignIn(t, handler, "baraka")
	if recorder := rename(t, handler, other, "amina", testPassword); recorder.Code != http.StatusConflict {
		t.Errorf("taking a held name by renaming: status %d, want 409", recorder.Code)
	}
	recorder := call(t, handler, http.MethodPost, "/api/auth/register",
		map[string]string{"username": "Amina", "password": testPassword}, "")
	if recorder.Code != http.StatusConflict {
		t.Errorf("registering a held name: status %d, want 409", recorder.Code)
	}

	// The record: the audit log names both, and the administrators' list
	// says who somebody was.
	detail := auditDetail(t, handler, admin, "user.username.changed", id)
	if detail["previous"] != "amina" || detail["username"] != "amina.j" {
		t.Errorf("the audit entry does not say what changed: %v", detail)
	}
	var people []map[string]any
	if err := json.Unmarshal(mustCall(t, handler, http.MethodGet, "/api/admin/users", nil, admin, http.StatusOK).Body.Bytes(), &people); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, person := range people {
		if person["id"] == id {
			names, _ := person["formerUsernames"].([]any)
			found = len(names) == 1 && names[0] == "amina"
		}
	}
	if !found {
		t.Error("an administrator cannot see the name somebody had before")
	}

	// The owner may come back to it once the wait allows.
	setSetting(t, handler, admin, "accounts.username_change_days", "0")
	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "amina", "password": testPassword}, token, http.StatusOK)
}

func TestAReleasedNameGoesToWhoeverTakesItNext(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	setSetting(t, handler, admin, "accounts.username_hold_days", "0")

	token := registerAndSignIn(t, handler, "amina")
	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "amina.j", "password": testPassword}, token, http.StatusOK)

	other := registerAndSignIn(t, handler, "baraka")
	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "amina", "password": testPassword}, other, http.StatusOK)

	// The name now means its new holder, not the person who gave it up.
	session := signIn(t, handler, "amina", testPassword)
	if got := me(t, handler, session)["username"]; got != "amina" {
		t.Errorf("signing in as the released name reached %v", got)
	}
	if got := me(t, handler, session)["id"]; got == me(t, handler, token)["id"] {
		t.Error("a released name still resolves to the person who gave it up")
	}
}

func TestAUsernameChangeAsksForProof(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")

	for name, password := range map[string]string{"no password": "", "the wrong password": "not it at all"} {
		if recorder := rename(t, handler, token, "amina.j", password); recorder.Code != http.StatusForbidden {
			t.Errorf("with %s: status %d, want 403", name, recorder.Code)
		}
	}
	if got := me(t, handler, token)["username"]; got != "amina" {
		t.Fatalf("an unconfirmed change went through: %v", got)
	}

	// A program holding a token is not the person, whatever it knows.
	apiToken, _ := issueToken(t, handler, token, "script", []string{"channel.read"})
	if recorder := rename(t, handler, apiToken, "amina.j", testPassword); recorder.Code != http.StatusForbidden {
		t.Errorf("with an API token: status %d, want 403", recorder.Code)
	}
}

/*
An account with no password proves itself by having just signed in.

Made by removing the password from an ordinary account: the passkey and
provider routes that create such accounts need a browser or a provider, and
what is under test is only what the change asks of an account without one.
*/
func TestAnAccountWithoutAPasswordMustHaveJustSignedIn(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")
	id, _ := me(t, handler, token)["id"].(string)
	execSQL(t, `DELETE FROM credentials WHERE user_id = $1 AND kind = 'password'`, id)

	execSQL(t, `UPDATE sessions SET created_at = NOW() - INTERVAL '1 hour' WHERE user_id = $1`, id)
	if recorder := rename(t, handler, token, "amina.j", ""); recorder.Code != http.StatusForbidden {
		t.Errorf("an hour-old session: status %d, want 403", recorder.Code)
	}

	execSQL(t, `UPDATE sessions SET created_at = NOW() WHERE user_id = $1`, id)
	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "amina.j"}, token, http.StatusOK)
}

func TestUsernameChangesArePacedAndCanBeTurnedOff(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")
	id, _ := me(t, handler, token)["id"].(string)

	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "amina.j", "password": testPassword}, token, http.StatusOK)

	if recorder := rename(t, handler, token, "amina.k", testPassword); recorder.Code != http.StatusForbidden {
		t.Errorf("a second change at once: status %d, want 403", recorder.Code)
	}
	policy := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/username", nil, token, http.StatusOK))
	if policy["canChange"] != false || policy["refusal"] != "waiting" || policy["nextChangeAt"] == nil {
		t.Errorf("the policy does not say when the next change is allowed: %v", policy)
	}

	// Thirty-one days on, it is allowed again.
	execSQL(t, `UPDATE username_history SET changed_at = NOW() - INTERVAL '31 days' WHERE user_id = $1`, id)
	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "amina.k", "password": testPassword}, token, http.StatusOK)

	setSetting(t, handler, admin, "accounts.username_changes", "false")
	setSetting(t, handler, admin, "accounts.username_change_days", "0")
	if recorder := rename(t, handler, token, "amina.l", testPassword); recorder.Code != http.StatusForbidden {
		t.Errorf("with changes turned off: status %d, want 403", recorder.Code)
	}
	policy = decode(t, mustCall(t, handler, http.MethodGet, "/api/me/username", nil, token, http.StatusOK))
	if policy["refusal"] != "disabled" {
		t.Errorf("the policy does not say changes are off: %v", policy)
	}
}

/*
Another server knows a person by their address, and cannot be told it changed.

The peer is written directly: federating a room through the API needs a second
server answering, and what is under test is only that being in such a room is
what refuses the change.
*/
func TestAUsernameCannotChangeWhileAnotherServerKnowsIt(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")
	roomID := makeRoom(t, handler, admin, "bridge", "public")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/join", nil, token, http.StatusOK)

	// In a room only this server takes part in, nothing is in the way.
	policy := decode(t, mustCall(t, handler, http.MethodGet, "/api/me/username", nil, token, http.StatusOK))
	if policy["canChange"] != true {
		t.Fatalf("a purely local member may not change their name: %v", policy)
	}

	execSQL(t, `INSERT INTO room_peers (room_id, peer) VALUES ($1, 'elsewhere.example')`, roomID)

	if recorder := rename(t, handler, token, "amina.j", testPassword); recorder.Code != http.StatusForbidden {
		t.Errorf("a member of a federated room: status %d, want 403", recorder.Code)
	}
	policy = decode(t, mustCall(t, handler, http.MethodGet, "/api/me/username", nil, token, http.StatusOK))
	if policy["refusal"] != "federated" {
		t.Errorf("the policy does not say why: %v", policy)
	}
}

func TestUsernameAvailabilityAnswersAsTheChangeWould(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")
	other := registerAndSignIn(t, handler, "baraka")
	mustCall(t, handler, http.MethodPut, "/api/me/username",
		map[string]string{"username": "baraka.m", "password": testPassword}, other, http.StatusOK)

	for wanted, want := range map[string]string{
		"amina.j": "",
		"amina":   "current",
		"Founder": "taken",
		"baraka":  "held",
		"admin":   "invalid",
		"-amina":  "invalid",
	} {
		answer := decode(t, mustCall(t, handler, http.MethodGet,
			"/api/me/username/availability?username="+url.QueryEscape(wanted), nil, token, http.StatusOK))
		if want == "" {
			if answer["available"] != true {
				t.Errorf("%q: %v, want available", wanted, answer)
			}
			continue
		}
		if answer["available"] != false || answer["reason"] != want {
			t.Errorf("%q: %v, want unavailable as %q", wanted, answer, want)
		}
	}
}

func TestUsernameChangeAttemptsAreLimited(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)
	token := registerAndSignIn(t, handler, "amina")

	for range 5 {
		rename(t, handler, token, "amina.j", "not it at all")
	}
	if recorder := rename(t, handler, token, "amina.j", testPassword); recorder.Code != http.StatusTooManyRequests {
		t.Errorf("the sixth attempt in an hour: status %d, want 429", recorder.Code)
	}
}
