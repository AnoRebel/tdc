package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

/*
The credentials a program carries.

The questions worth asking are about authority rather than plumbing: that a
token does what it was given and nothing more, that it cannot be given more
than its account has, and that revoking it stops it at once. A token that
works is easy; a token that works only within its grant is the feature.
*/

// issueToken creates one and returns the secret, which exists exactly once.
func issueToken(t *testing.T, handler http.Handler, session, label string, scope []string) (string, string) {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/me/tokens",
		map[string]any{"label": label, "scope": scope}, session)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("issuing a token returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var issued struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &issued); err != nil {
		t.Fatalf("parsing the issued token: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("a token was issued without its secret, so nobody can use it")
	}
	return issued.Token, issued.ID
}

/*
A token answers the same routes a session does, identically.

This is the whole premise: everything the web client does is an HTTP call
somebody else can make. A separate namespace for programs would mean two
surfaces that drift.
*/
func TestATokenAnswersTheSameRoutesAsASession(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	token, _ := issueToken(t, handler, owner, "deploy bot", []string{"channel.create"})

	bySession := call(t, handler, http.MethodGet, "/api/me", nil, owner)
	byToken := call(t, handler, http.MethodGet, "/api/me", nil, token)

	if byToken.Code != bySession.Code {
		t.Fatalf("a token got %d where a session got %d", byToken.Code, bySession.Code)
	}

	var asSession, asToken map[string]any
	_ = json.Unmarshal(bySession.Body.Bytes(), &asSession)
	_ = json.Unmarshal(byToken.Body.Bytes(), &asToken)

	if asToken["username"] != asSession["username"] {
		t.Errorf("a token acted as %v where the session is %v",
			asToken["username"], asSession["username"])
	}
}

/*
A token cannot do what it was not given.

The account here is the administrator, which is the case that matters: a narrow
token on a powerful account must stay narrow, or issuing one would be a way of
handing over everything.
*/
func TestATokenCannotExceedItsScope(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// Nothing but the ability to create a channel.
	token, _ := issueToken(t, handler, owner, "channel maker", []string{"channel.create"})

	// What it was given.
	granted := call(t, handler, http.MethodPost, "/api/rooms",
		map[string]any{"slug": "made-by-a-token", "name": "Made by a token"}, token)
	if granted.Code != http.StatusCreated {
		t.Fatalf("a token was refused the permission it holds: %d %s",
			granted.Code, granted.Body.String())
	}

	/*
		And what it was not. The administrator who issued it can read the audit
		log; the token cannot, because it was never given that.
	*/
	refused := call(t, handler, http.MethodGet, "/api/admin/audit?limit=1", nil, token)
	if refused.Code != http.StatusForbidden {
		t.Errorf("a token read the audit log with %d; it holds no such permission", refused.Code)
	}

	// The session that issued it is unaffected.
	if allowed := call(t, handler, http.MethodGet, "/api/admin/audit?limit=1", nil, owner); allowed.Code != http.StatusOK {
		t.Errorf("issuing a token changed what its owner can do: %d", allowed.Code)
	}
}

/*
A token cannot be given what its account does not hold.

Otherwise an ordinary member could mint an administrator, and the permission
system would be advisory.
*/
func TestATokenCannotBeGivenMoreThanItsAccountHas(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	member := registerAndSignIn(t, handler, "juma")

	recorder := call(t, handler, http.MethodPost, "/api/me/tokens",
		map[string]any{"label": "ambitious", "scope": []string{"server.administer"}}, member)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("an ordinary account minted an administrator token: %d %s",
			recorder.Code, recorder.Body.String())
	}
}

// A scope naming something this server does not define is refused rather than
// quietly dropped, so a typo fails where it was made.
func TestAnUnknownPermissionIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/me/tokens",
		map[string]any{"label": "typo", "scope": []string{"channel.creat"}}, owner)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("a misspelled permission was accepted: %d %s",
			recorder.Code, recorder.Body.String())
	}
}

// Revoking stops it at once, not at the next expiry.
func TestRevokingATokenStopsItImmediately(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	token, id := issueToken(t, handler, owner, "short lived", []string{"channel.create"})

	if before := call(t, handler, http.MethodGet, "/api/me", nil, token); before.Code != http.StatusOK {
		t.Fatalf("the token did not work before revocation: %d", before.Code)
	}

	revoked := call(t, handler, http.MethodDelete, "/api/me/tokens/"+id, nil, owner)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoking returned %d: %s", revoked.Code, revoked.Body.String())
	}

	after := call(t, handler, http.MethodGet, "/api/me", nil, token)
	if after.Code != http.StatusUnauthorized {
		t.Errorf("a revoked token still worked: %d", after.Code)
	}
}

/*
The secret is shown once and never again.

A list that returned it would make every one of these credentials readable by
anybody who could read a list, which is the opposite of what a token is for.
*/
func TestTheSecretIsShownOnceAndNeverListed(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	token, _ := issueToken(t, handler, owner, "once only", nil)

	recorder := call(t, handler, http.MethodGet, "/api/me/tokens", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("listing tokens returned %d", recorder.Code)
	}

	body := recorder.Body.String()
	if strings.Contains(body, token) {
		t.Error("the list returned the secret, which is meant to exist only once")
	}
	if !strings.Contains(body, "once only") {
		t.Error("the list did not name the token that was issued")
	}
}

/*
A token cannot issue another token.

A program that could mint a successor would make revocation less than final:
the person who revoked one would have taken back nothing.
*/
func TestATokenCannotIssueAnotherToken(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	token, _ := issueToken(t, handler, owner, "parent", []string{"channel.create"})

	recorder := call(t, handler, http.MethodPost, "/api/me/tokens",
		map[string]any{"label": "child", "scope": []string{"channel.create"}}, token)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("a token issued another token: %d %s", recorder.Code, recorder.Body.String())
	}
}

// Somebody else's token is not revocable, and not confirmed to exist.
func TestATokenIsOnlyRevocableByItsOwner(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	_, id := issueToken(t, handler, owner, "not yours", nil)

	stranger := registerAndSignIn(t, handler, "juma")

	recorder := call(t, handler, http.MethodDelete, "/api/me/tokens/"+id, nil, stranger)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("somebody revoked a token that was not theirs: %d", recorder.Code)
	}

	// And it still works, which is the part that would matter.
	if list := call(t, handler, http.MethodGet, "/api/me/tokens", nil, owner); !strings.Contains(list.Body.String(), "not yours") {
		t.Error("the token was revoked by somebody who did not own it")
	}
}

// An expired token is refused the same way a revoked one is.
func TestAnExpiredTokenIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	past := time.Now().Add(-time.Hour)
	recorder := call(t, handler, http.MethodPost, "/api/me/tokens",
		map[string]any{"label": "already over", "expiresAt": past}, owner)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("issuing returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var issued struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &issued)

	if used := call(t, handler, http.MethodGet, "/api/me", nil, issued.Token); used.Code != http.StatusUnauthorized {
		t.Errorf("a token that expired an hour ago still worked: %d", used.Code)
	}
}

// Issuing and revoking are both recorded: a credential added by somebody who
// should not have is how an account is kept after a password is changed.
func TestTokenLifetimeIsAudited(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	_, id := issueToken(t, handler, owner, "audited", nil)
	call(t, handler, http.MethodDelete, "/api/me/tokens/"+id, nil, owner)

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?limit=20", nil, owner))

	var issued, revoked bool
	for _, entry := range entries {
		switch entry.Action {
		case "auth.token.issued":
			issued = true
		case "auth.token.revoked":
			revoked = true
		}
	}
	if !issued {
		t.Error("issuing a token left no record")
	}
	if !revoked {
		t.Error("revoking a token left no record")
	}
}

/*
Recovering an account ends the tokens it held.

A password is changed after a compromise, and a token minted by whoever had the
account is exactly what would let them back in: it never depended on the
password, so replacing the password does not touch it. Ending the sessions and
leaving the tokens takes back the visible half of an intrusion and leaves the
durable half.
*/
func TestRecoveringAnAccountEndsItsTokens(t *testing.T) {
	handler, post := withMail(t)
	completeSetup(t, handler)
	registerVerified(t, handler, "amina", "amina@example.test")

	session := signIn(t, handler, "amina", testPassword)
	token, _ := issueToken(t, handler, session, "left behind", nil)

	if before := call(t, handler, http.MethodGet, "/api/me", nil, token); before.Code != http.StatusOK {
		t.Fatalf("the token did not work before recovery: %d", before.Code)
	}

	if recorder := call(t, handler, http.MethodPost, "/api/auth/recover",
		map[string]string{"email": "amina@example.test"}, ""); recorder.Code != http.StatusAccepted {
		t.Fatalf("requesting recovery: %d", recorder.Code)
	}

	if recorder := call(t, handler, http.MethodPost, "/api/auth/recover/redeem",
		map[string]string{"token": tokenFromMail(t, post), "password": "a completely different passphrase"},
		""); recorder.Code != http.StatusOK {
		t.Fatalf("redeeming: %d %s", recorder.Code, recorder.Body.String())
	}

	after := call(t, handler, http.MethodGet, "/api/me", nil, token)
	if after.Code != http.StatusUnauthorized {
		t.Errorf("a token issued before the account was recovered still works: %d", after.Code)
	}
}

/*
Every route answers a token the way it answers a session.

The premise of the whole phase is that there is one API rather than a human one
and a machine one. A route that behaved differently for a program would be a
second surface, and two surfaces drift — usually in the direction of the one
nobody is looking at.
*/
func TestEveryRouteAnswersATokenLikeASession(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	/*
		Every permission the account holds, so a difference in the answer is a
		difference in the route rather than in the grant.

		Named one by one rather than reaching for server.administer alone: that
		permission acts as a wildcard for the account, but a token's scope is
		a list, and a route asking for role.manage wants role.manage. Finding
		that out is what this test is for — the first version of it listed four
		permissions and reported a parity failure that was really a scope
		it had not been given.
	*/
	token, _ := issueToken(t, handler, owner, "wide", []string{
		"server.administer", "server.configure", "server.audit.read",
		"role.manage", "role.assign", "user.invite", "user.read.all",
		"channel.create", "channel.read", "channel.post",
	})

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/me/permissions"},
		{http.MethodGet, "/api/rooms"},
		{http.MethodGet, "/api/admin/audit?limit=1"},
		{http.MethodGet, "/api/admin/permissions"},
		{http.MethodGet, "/api/notifications"},
	} {
		bySession := call(t, handler, route.method, route.path, nil, owner)
		byToken := call(t, handler, route.method, route.path, nil, token)

		if byToken.Code != bySession.Code {
			t.Errorf("%s %s: a token got %d where a session got %d",
				route.method, route.path, byToken.Code, bySession.Code)
		}
	}
}

/*
A token is not a session, and is not listed as one.

Somebody reading their signed-in devices is deciding what to end. A token
appearing there would be confusing in both directions: it is not a device, and
ending it is not done from that list.
*/
func TestATokenIsNotListedAmongSessions(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	issueToken(t, handler, owner, "not a device", nil)

	recorder := call(t, handler, http.MethodGet, "/api/me/sessions", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("listing sessions returned %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "not a device") {
		t.Error("a token appeared in the list of signed-in devices")
	}
}
