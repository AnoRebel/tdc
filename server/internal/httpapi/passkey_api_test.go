package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/descope/virtualwebauthn"
)

/*
The passkey routes, driven by a software authenticator.

Same reasoning as the service's own tests: a real key pair signing real
challenges, so a server that skipped verification would fail here where a
recorded fixture would pass.
*/

const passkeyHost = "chat.example.test"

var passkeyRelyingParty = virtualwebauthn.RelyingParty{
	Name:   "Test Server",
	ID:     passkeyHost,
	Origin: "https://" + passkeyHost,
}

// withPasskeys builds a handler for a server that knows where it is, which is
// what having passkeys requires.
func withPasskeys(t *testing.T) http.Handler {
	t.Helper()

	previousOrigins := passkeyOriginsForTest
	passkeyOriginsForTest = []string{passkeyRelyingParty.Origin}
	t.Cleanup(func() { passkeyOriginsForTest = previousOrigins })

	return newTestAPIWithAddress(t, passkeyHost)
}

// enrolPasskey runs a whole enrolment through the routes.
func enrolPasskey(
	t *testing.T,
	handler http.Handler,
	token, label string,
) (virtualwebauthn.Authenticator, virtualwebauthn.Credential) {
	t.Helper()

	authenticator := virtualwebauthn.NewAuthenticator()
	credential := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	begun := call(t, handler, http.MethodPost, "/api/me/passkeys/begin", nil, token)
	if begun.Code != http.StatusOK {
		t.Fatalf("beginning enrolment: status %d, body %s", begun.Code, begun.Body.String())
	}

	options, err := virtualwebauthn.ParseAttestationOptions(begun.Body.String())
	if err != nil {
		t.Fatalf("parsing enrolment options: %v", err)
	}

	answer := virtualwebauthn.CreateAttestationResponse(
		passkeyRelyingParty, authenticator, credential, *options)

	finished := callWithHeaders(t, handler, http.MethodPost, "/api/me/passkeys/finish",
		json.RawMessage(answer), token, map[string]string{"X-Passkey-Label": label})
	if finished.Code != http.StatusCreated {
		t.Fatalf("finishing enrolment: status %d, body %s", finished.Code, finished.Body.String())
	}

	authenticator.AddCredential(credential)
	return authenticator, credential
}

// signInWithPasskey runs a whole sign-in through the routes.
func signInWithPasskey(
	t *testing.T,
	handler http.Handler,
	authenticator virtualwebauthn.Authenticator,
	credential virtualwebauthn.Credential,
	handle []byte,
) *httptest.ResponseRecorder {
	t.Helper()

	begun := call(t, handler, http.MethodPost, "/api/auth/passkey/signin/begin", nil, "")
	if begun.Code != http.StatusOK {
		t.Fatalf("beginning sign-in: status %d, body %s", begun.Code, begun.Body.String())
	}

	options, err := virtualwebauthn.ParseAssertionOptions(begun.Body.String())
	if err != nil {
		t.Fatalf("parsing sign-in options: %v", err)
	}

	authenticator.Options.UserHandle = handle
	answer := virtualwebauthn.CreateAssertionResponse(
		passkeyRelyingParty, authenticator, credential, *options)

	return call(t, handler, http.MethodPost, "/api/auth/passkey/signin/finish",
		json.RawMessage(answer), "")
}

/*
TestAPasskeyEnrolledThroughTheAPISignsSomebodyIn.

The whole feature over HTTP: enrol while signed in, then sign in from nothing
with the authenticator alone.
*/
func TestAPasskeyEnrolledThroughTheAPISignsSomebodyIn(t *testing.T) {
	handler := withPasskeys(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}
	token := signIn(t, handler, "amina", testPassword)

	authenticator, credential := enrolPasskey(t, handler, token, "Phone")
	handle := webauthnHandle(t, handler, token)

	recorder := signInWithPasskey(t, handler, authenticator, credential, handle)
	if recorder.Code != http.StatusOK {
		t.Fatalf("signing in with a passkey: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	result := decode(t, recorder)
	user, _ := result["user"].(map[string]any)
	if user == nil || user["username"] != "amina" {
		t.Errorf("signed in as %v, want amina", user)
	}
	// The desktop has no cookie jar, so the token is in the body as well.
	if token, _ := result["token"].(string); token == "" {
		t.Error("a passkey sign-in issued no token in the body")
	}
}

/*
TestEnrollingAPasskeyNeedsASession.

Adding a passkey adds a way into an account. An unauthenticated caller who could
do it would be adding their own authenticator to somebody else's account.

Two things refuse it — the route's middleware and the handler's own look for a
signed-in user — and either alone is enough: this still passes with the
middleware removed. Both are kept because the handler's check is what makes the
handler correct on its own terms, and the middleware is what makes the routing
table say where authority is required.
*/
func TestEnrollingAPasskeyNeedsASession(t *testing.T) {
	handler := withPasskeys(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/me/passkeys/begin", nil, ""); recorder.Code != http.StatusUnauthorized {
		t.Errorf("enrolling with no session: status %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

/*
TestAPasskeyBesideAPasswordMayBeRemoved.

The counterpart to the refusal, and the reason it counts across kinds rather
than across passkeys: this account has a password, so its only passkey is not
the only way in and removing it is ordinary. The refusal itself is exercised at
the service, where an account can be built with nothing else to fall back on.
*/
func TestAPasskeyBesideAPasswordMayBeRemoved(t *testing.T) {
	handler := withPasskeys(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "solo", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}
	token := signIn(t, handler, "solo", testPassword)

	enrolPasskey(t, handler, token, "Only one")

	listed := decodeList(t, call(t, handler, http.MethodGet, "/api/me/passkeys", nil, token))
	if len(listed) != 1 {
		t.Fatalf("got %d passkeys, want 1", len(listed))
	}

	first, _ := listed[0].(map[string]any)
	identifier, _ := first["id"].(string)

	if recorder := call(t, handler, http.MethodDelete, "/api/me/passkeys/"+identifier, nil, token); recorder.Code != http.StatusNoContent {
		t.Fatalf("revoking a passkey beside a password: status %d, body %s",
			recorder.Code, recorder.Body.String())
	}

	if remaining := decodeList(t, call(t, handler, http.MethodGet, "/api/me/passkeys", nil, token)); len(remaining) != 0 {
		t.Errorf("got %d passkeys after revoking, want 0", len(remaining))
	}
}

/*
TestRevokingAPasskeyEndsOtherSessions.

A revoked credential must stop working, and a session it established outliving
it would leave whoever holds that session with what the revocation was meant to
take away.
*/
func TestRevokingAPasskeyEndsOtherSessions(t *testing.T) {
	handler := withPasskeys(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}

	// Two sessions: the one doing the revoking, and one elsewhere.
	revoking := signIn(t, handler, "amina", testPassword)
	elsewhere := signIn(t, handler, "amina", testPassword)

	enrolPasskey(t, handler, revoking, "Phone")
	listed := decodeList(t, call(t, handler, http.MethodGet, "/api/me/passkeys", nil, revoking))
	first, _ := listed[0].(map[string]any)
	identifier, _ := first["id"].(string)

	if recorder := call(t, handler, http.MethodDelete, "/api/me/passkeys/"+identifier, nil, revoking); recorder.Code != http.StatusNoContent {
		t.Fatalf("revoking: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	// The other session is gone.
	if recorder := call(t, handler, http.MethodGet, "/api/me", nil, elsewhere); recorder.Code != http.StatusUnauthorized {
		t.Errorf("the other session still works: status %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	// This one is not: somebody removing a credential has not asked to be
	// signed out of what they are doing it from.
	if recorder := call(t, handler, http.MethodGet, "/api/me", nil, revoking); recorder.Code != http.StatusOK {
		t.Errorf("the revoking session was ended too: status %d", recorder.Code)
	}
}

// TestPasskeysAreNamedAndRenamed, which is how somebody tells three of them
// apart a year later.
func TestPasskeysAreNamedAndRenamed(t *testing.T) {
	handler := withPasskeys(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}
	token := signIn(t, handler, "amina", testPassword)

	enrolPasskey(t, handler, token, "Phone")

	listed := decodeList(t, call(t, handler, http.MethodGet, "/api/me/passkeys", nil, token))
	first, _ := listed[0].(map[string]any)
	if first["label"] != "Phone" {
		t.Errorf("label = %v, want Phone", first["label"])
	}
	// Never used yet, which is how somebody spots one they enrolled and forgot.
	if _, present := first["lastUsed"]; present {
		t.Error("a passkey that has never signed anybody in reports a last use")
	}
	// Nothing about the key itself belongs in this response.
	for _, leaked := range []string{"secret", "publicKey", "passkey"} {
		if _, present := first[leaked]; present {
			t.Errorf("the response carries %q", leaked)
		}
	}

	identifier, _ := first["id"].(string)
	renamed := call(t, handler, http.MethodPatch, "/api/me/passkeys/"+identifier,
		map[string]string{"label": "Work laptop"}, token)
	if renamed.Code != http.StatusOK {
		t.Fatalf("renaming: status %d, body %s", renamed.Code, renamed.Body.String())
	}
	if decode(t, renamed)["label"] != "Work laptop" {
		t.Error("renaming did not take")
	}
}

/*
TestAServerThatDoesNotKnowWhereItIsOffersNoPasskeys.

A passkey is bound to a hostname. Offering them without one would mean enrolling
against a guess, and every passkey stopping the day the guess changed.
*/
func TestAServerThatDoesNotKnowWhereItIsOffersNoPasskeys(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/passkey/signin/begin", nil, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("beginning a sign-in on a server with no address: status %d, want %d",
			recorder.Code, http.StatusNotFound)
	}

	if advertised, _ := decode(t, call(t, handler, http.MethodGet, "/api/server", nil, ""))["passkeys"].(bool); advertised {
		t.Error("a server that cannot offer passkeys advertises them")
	}
}

// TestPasskeysAreAdvertised, so a client knows to draw the button.
func TestPasskeysAreAdvertised(t *testing.T) {
	handler := withPasskeys(t)
	completeSetup(t, handler)

	if advertised, _ := decode(t, call(t, handler, http.MethodGet, "/api/server", nil, ""))["passkeys"].(bool); !advertised {
		t.Error("a server that offers passkeys does not say so")
	}
}

/*
webauthnHandle reads the handle the server established, which the authenticator
would have stored when the passkey was made.

Taken from the enrolment options rather than the database, because that is where
a real authenticator gets it: the server puts it in the options it sends, and
anything else would be this test agreeing with itself about a value it invented.
*/
func webauthnHandle(t *testing.T, handler http.Handler, token string) []byte {
	t.Helper()

	begun := call(t, handler, http.MethodPost, "/api/me/passkeys/begin", nil, token)
	if begun.Code != http.StatusOK {
		t.Fatalf("asking for options: status %d", begun.Code)
	}

	options, err := virtualwebauthn.ParseAttestationOptions(begun.Body.String())
	if err != nil {
		t.Fatalf("parsing options: %v", err)
	}

	// Already decoded by the parser: UserID holds the raw handle bytes as a
	// string rather than the base64url the options carried them in.
	return []byte(options.UserID)
}

// decodeList reads a JSON array response.
func decodeList(t *testing.T, recorder *httptest.ResponseRecorder) []any {
	t.Helper()
	var body []any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return body
}

/*
callWithHeaders is call, with headers the passkey routes need.

The enrolment's name travels in a header because the body is the authenticator's
own response, parsed by the library from the request itself; adding a field to
it would mean reshaping what the browser made.
*/
func callWithHeaders(
	t *testing.T,
	handler http.Handler,
	method, path string,
	body any,
	token string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
