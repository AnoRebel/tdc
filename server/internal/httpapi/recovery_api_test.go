package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
Recovery over HTTP.

The service's own tests cover the rules. What is checked here is that the route
keeps the one that is a property of the response rather than of any function: an
address with an account and one without must be answered identically, in body
and in status.
*/

// withMail builds a handler for a server that can send, which recovery needs.
func withMail(t *testing.T) (http.Handler, *testPostbox) {
	t.Helper()

	post := &testPostbox{}
	previous := postboxForTest
	postboxForTest = post
	t.Cleanup(func() { postboxForTest = previous })

	// Recovery links need somewhere to point, as on a real server.
	return newTestAPIWithAddress(t, "chat.example.test"), post
}

// registerVerified makes an account whose address recovery will actually mail.
func registerVerified(t *testing.T, handler http.Handler, username, email string) {
	t.Helper()

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": username, "email": email, "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register %s: status %d, body %s", username, recorder.Code, recorder.Body.String())
	}

	// Registration leaves an address unverified, and recovery deliberately
	// will not mail one. Marked directly because there is no route for it yet.
	markEmailVerified(t, handler, email)
}

/*
TestRecoveryAnswersTheSameWhateverItFinds.

The rule the specification states, and the one that lives in the response rather
than in a function: same status, same body, for an address with an account and
one without.
*/
func TestRecoveryAnswersTheSameWhateverItFinds(t *testing.T) {
	handler, post := withMail(t)
	completeSetup(t, handler)
	registerVerified(t, handler, "amina", "amina@example.test")

	known := call(t, handler, http.MethodPost, "/api/auth/recover",
		map[string]string{"email": "amina@example.test"}, "")
	unknown := call(t, handler, http.MethodPost, "/api/auth/recover",
		map[string]string{"email": "nobody@example.test"}, "")

	if known.Code != unknown.Code {
		t.Errorf("status %d for a known address and %d for an unknown one",
			known.Code, unknown.Code)
	}
	if known.Body.String() != unknown.Body.String() {
		t.Errorf("different bodies:\n known:   %s\n unknown: %s",
			known.Body.String(), unknown.Body.String())
	}

	// And the message says nothing either.
	message, _ := decode(t, known)["message"].(string)
	for _, leaked := range []string{"amina", "exists", "found", "no account"} {
		if mentions(message, leaked) {
			t.Errorf("the response mentions %q: %s", leaked, message)
		}
	}

	// Only the real one produced mail.
	delivered := post.delivered()
	if len(delivered) != 1 {
		t.Fatalf("%d messages sent, want 1", len(delivered))
	}
	if delivered[0].To != "amina@example.test" {
		t.Errorf("mail went to %q", delivered[0].To)
	}
}

// TestALinkResetsThePassword, which is the whole point.
func TestALinkResetsThePassword(t *testing.T) {
	handler, post := withMail(t)
	completeSetup(t, handler)
	registerVerified(t, handler, "amina", "amina@example.test")

	if recorder := call(t, handler, http.MethodPost, "/api/auth/recover",
		map[string]string{"email": "amina@example.test"}, ""); recorder.Code != http.StatusAccepted {
		t.Fatalf("requesting: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	token := tokenFromMail(t, post)

	// The link is good before it is used.
	if recorder := call(t, handler, http.MethodGet,
		"/api/auth/recover?token="+token, nil, ""); recorder.Code != http.StatusOK {
		t.Errorf("checking a fresh link: status %d", recorder.Code)
	}

	const chosen = "a completely different passphrase"
	if recorder := call(t, handler, http.MethodPost, "/api/auth/recover/redeem",
		map[string]string{"token": token, "password": chosen}, ""); recorder.Code != http.StatusOK {
		t.Fatalf("redeeming: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	// The new password works and the old one does not.
	signIn(t, handler, "amina", chosen)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/signin",
		map[string]string{"username": "amina", "password": testPassword}, ""); recorder.Code == http.StatusOK {
		t.Error("the old password still signs somebody in")
	}
}

/*
TestRedeemingIssuesNoSession.

Somebody who has just set a password signs in with it, which proves they know it
and goes through the second factor where the account has one. A session handed
out here would skip both, and a recovery link would become a way past the second
factor rather than past a forgotten password.
*/
func TestRedeemingIssuesNoSession(t *testing.T) {
	handler, post := withMail(t)
	completeSetup(t, handler)
	registerVerified(t, handler, "amina", "amina@example.test")

	call(t, handler, http.MethodPost, "/api/auth/recover",
		map[string]string{"email": "amina@example.test"}, "")

	recorder := call(t, handler, http.MethodPost, "/api/auth/recover/redeem", map[string]string{
		"token": tokenFromMail(t, post), "password": "a completely different passphrase",
	}, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("redeeming: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	result := decode(t, recorder)
	for _, handed := range []string{"token", "user", "expiresAt"} {
		if _, present := result[handed]; present {
			t.Errorf("redeeming handed back %q, which is a sign-in", handed)
		}
	}
}

// TestASpentLinkIsRefusedByBothRoutes, so somebody following an old link is
// told before they type a password rather than after.
func TestASpentLinkIsRefusedByBothRoutes(t *testing.T) {
	handler, post := withMail(t)
	completeSetup(t, handler)
	registerVerified(t, handler, "amina", "amina@example.test")

	call(t, handler, http.MethodPost, "/api/auth/recover",
		map[string]string{"email": "amina@example.test"}, "")
	token := tokenFromMail(t, post)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/recover/redeem",
		map[string]string{"token": token, "password": "a completely different passphrase"},
		""); recorder.Code != http.StatusOK {
		t.Fatalf("redeeming: status %d", recorder.Code)
	}

	if recorder := call(t, handler, http.MethodGet,
		"/api/auth/recover?token="+token, nil, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("checking a spent link: status %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/auth/recover/redeem",
		map[string]string{"token": token, "password": "yet another passphrase here"},
		""); recorder.Code != http.StatusNotFound {
		t.Errorf("redeeming a spent link: status %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

/*
TestAServerWithNoMailOffersNoRecovery.

A "forgotten your password" link on a server that cannot send is worse than no
link: it promises something nobody can deliver.
*/
func TestAServerWithNoMailOffersNoRecovery(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/recover",
		map[string]string{"email": "amina@example.test"}, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("requesting on a server with no mail: status %d, want %d",
			recorder.Code, http.StatusNotFound)
	}

	if offered, _ := decode(t, call(t, handler, http.MethodGet, "/api/server", nil, ""))["recovery"].(bool); offered {
		t.Error("a server that cannot send mail advertises recovery")
	}
}

// TestRecoveryIsAdvertisedWhereItWorks, so a client knows to draw the link.
func TestRecoveryIsAdvertisedWhereItWorks(t *testing.T) {
	handler, _ := withMail(t)
	completeSetup(t, handler)

	if offered, _ := decode(t, call(t, handler, http.MethodGet, "/api/server", nil, ""))["recovery"].(bool); !offered {
		t.Error("a server that can send mail does not offer recovery")
	}
}

// tokenFromMail reads the link's token out of what was delivered.
func tokenFromMail(t *testing.T, post *testPostbox) string {
	t.Helper()

	delivered := post.delivered()
	if len(delivered) == 0 {
		t.Fatal("nothing was sent")
	}

	body := delivered[len(delivered)-1].Body
	_, after, found := strings.Cut(body, "?token=")
	if !found {
		t.Fatalf("no link in the message:\n%s", body)
	}
	token, _, _ := strings.Cut(after, "\n")
	return strings.TrimSpace(token)
}
