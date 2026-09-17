package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

/*
These tests drive the whole sign-in through a provider that is not real.

A fake provider rather than a recorded exchange, because what is being checked
is how this server behaves when a provider says a particular thing — that an
address is verified, or that it is not — and the interesting cases are the ones
no real provider will produce on demand.
*/

// fakeProvider stands in for a provider somebody else runs.
type fakeProvider struct {
	server *httptest.Server
	// account is what it answers when asked who signed in.
	account map[string]any
	// lastTokenForm is what the server sent to exchange the code, so a test can
	// check what actually went over the wire.
	lastTokenForm url.Values
	// refuseToken makes the exchange fail, as a provider does when a code has
	// already been used.
	refuseToken bool
}

func newFakeProvider(t *testing.T, account map[string]any) *fakeProvider {
	t.Helper()
	provider := &fakeProvider{account: account}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		provider.lastTokenForm = r.PostForm

		w.Header().Set("Content-Type", "application/json")
		if provider.refuseToken {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "provider-token", "token_type": "bearer",
		})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer provider-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(provider.account)
	})

	provider.server = httptest.NewServer(mux)
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *fakeProvider) config() config.OAuthProvider {
	return config.OAuthProvider{
		Name:         "testing",
		Label:        "Test Provider",
		ClientID:     "client",
		ClientSecret: "secret",
		// The fake is plain HTTP, which the configuration loader would refuse.
		// It is constructed directly here because what is under test is the
		// flow, and the refusal has its own test in the config package.
		AuthURL:  p.server.URL + "/authorize",
		TokenURL: p.server.URL + "/token",
		UserURL:  p.server.URL + "/user",
		Scopes:   []string{"openid", "email"},
	}
}

// withProvider builds a handler that signs people in through the fake.
func withProvider(t *testing.T, provider *fakeProvider) http.Handler {
	t.Helper()

	previousProviders := oauthProvidersForTest
	previousClient := oauthClientForTest
	oauthProvidersForTest = []config.OAuthProvider{provider.config()}
	// The fake's own client, so its plain-HTTP address is reachable: the real
	// one refuses to dial anything but a public address.
	oauthClientForTest = provider.server.Client()
	t.Cleanup(func() {
		oauthProvidersForTest = previousProviders
		oauthClientForTest = previousClient
	})

	return newTestAPI(t)
}

// begin starts a sign-in and returns the state the server issued.
func begin(t *testing.T, handler http.Handler, token string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/auth/oauth/testing/start", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("start: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	state, ok := decode(t, recorder)["state"].(string)
	if !ok || state == "" {
		t.Fatal("starting a sign-in returned no state")
	}
	return state
}

// callback plays the provider returning, and returns the handle it parked the
// outcome under.
func callback(t *testing.T, handler http.Handler, state string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodGet,
		"/api/auth/oauth/callback?state="+url.QueryEscape(state)+"&code=granted", nil, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("callback: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	location := recorder.Header().Get("Location")
	_, fragment, found := strings.Cut(location, "#oauth=")
	if !found {
		t.Fatalf("the callback redirected to %q, which carries no handle", location)
	}
	handle, err := url.QueryUnescape(fragment)
	if err != nil {
		t.Fatalf("the handle could not be decoded: %v", err)
	}
	return handle
}

// finish collects a parked outcome.
func finish(t *testing.T, handler http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, handler, http.MethodPost, "/api/auth/oauth/finish", body, "")
}

/*
TestAnUnverifiedEmailDoesNotReachAnExistingAccount is the end-to-end form of the
rule the resolution tests cover in isolation.

The provider asserts, without verifying, the exact address of an account that
already exists. Nothing about the flow may turn that into a sign-in to it.
*/
func TestAnUnverifiedEmailDoesNotReachAnExistingAccount(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{
		"sub": "impostor-9", "email": "amina@example.test", "email_verified": false,
		"preferred_username": "amina",
	})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "email": "amina@example.test", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	handle := callback(t, handler, begin(t, handler, ""))

	// Collecting it without asking for anything says what it resolved to.
	recorder := finish(t, handler, map[string]any{"handle": handle, "username": "someone-else"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("finish: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	result := decode(t, recorder)
	user, _ := result["user"].(map[string]any)
	if user == nil {
		t.Fatal("finishing produced no account")
	}
	if user["username"] == "amina" {
		t.Fatal("an unverified email signed somebody in to the account that holds it")
	}
	if user["username"] != "someone-else" {
		t.Errorf("username = %v, want the one that was chosen", user["username"])
	}
}

/*
TestAVerifiedEmailAsksBeforeLinking.

The provider verified the address, so a link is offered — and the offer alone
must not sign anybody in. Collecting it yields a question, not a session.
*/
func TestAVerifiedEmailAsksBeforeLinking(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{
		"sub": "amina-1", "email": "amina@example.test", "email_verified": true,
	})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "email": "amina@example.test", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}

	handle := callback(t, handler, begin(t, handler, ""))

	// Accepting without the password: the address was verified at the provider,
	// but that says nothing about holding the account here.
	recorder := finish(t, handler, map[string]any{"handle": handle, "accept": true})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("accepting a link with no password: status %d, body %s",
			recorder.Code, recorder.Body.String())
	}
}

// TestAcceptingALinkNeedsThePassword, and with it, signs in to the account that
// was offered.
func TestAcceptingALinkNeedsThePassword(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{
		"sub": "amina-1", "email": "amina@example.test", "email_verified": true,
	})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "email": "amina@example.test", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("register: status %d", recorder.Code)
	}

	handle := callback(t, handler, begin(t, handler, ""))

	recorder := finish(t, handler, map[string]any{
		"handle": handle, "accept": true, "password": testPassword,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("accepting a link: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	result := decode(t, recorder)
	user, _ := result["user"].(map[string]any)
	if user == nil || user["username"] != "amina" {
		t.Fatalf("accepting the link signed in as %v, want amina", user)
	}
	if token, _ := result["token"].(string); token == "" {
		t.Error("accepting the link issued no session")
	}

	// And the identity is now attached, so the next sign-in needs no password
	// and no question.
	second := finish(t, handler, map[string]any{"handle": callback(t, handler, begin(t, handler, ""))})
	if second.Code != http.StatusOK {
		t.Fatalf("second sign-in: status %d, body %s", second.Code, second.Body.String())
	}
	if status := decode(t, second)["status"]; status != "signed-in" {
		t.Errorf("the second sign-in resolved to %v, want signed-in", status)
	}
}

// TestAStateIsGoodOnce: replaying a callback must not produce a second outcome.
func TestAStateIsGoodOnce(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "once-1", "preferred_username": "once"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	state := begin(t, handler, "")
	callback(t, handler, state)

	// The same state again. It is not an error to the browser — it is told the
	// sign-in expired — but it must not resolve a second time.
	recorder := call(t, handler, http.MethodGet,
		"/api/auth/oauth/callback?state="+url.QueryEscape(state)+"&code=granted", nil, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("replayed callback: status %d", recorder.Code)
	}

	handle := strings.SplitN(recorder.Header().Get("Location"), "#oauth=", 2)[1]
	decoded, _ := url.QueryUnescape(handle)
	result := decode(t, finish(t, handler, map[string]any{"handle": decoded}))
	if result["status"] != "expired" {
		t.Errorf("replaying a state resolved to %v, want expired", result["status"])
	}
}

// TestAHandleIsCollectedOnce, for the same reason: what it collects may be a
// session token.
func TestAHandleIsCollectedOnce(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "twice-1", "preferred_username": "twice"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	handle := callback(t, handler, begin(t, handler, ""))

	if recorder := finish(t, handler, map[string]any{"handle": handle}); recorder.Code != http.StatusOK {
		t.Fatalf("first collection: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if recorder := finish(t, handler, map[string]any{"handle": handle}); recorder.Code != http.StatusBadRequest {
		t.Errorf("collecting a handle twice: status %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

// TestAnUnknownStateIsRefused covers a callback nobody here issued.
func TestAnUnknownStateIsRefused(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "nobody"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet,
		"/api/auth/oauth/callback?state=invented&code=granted", nil, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("forged callback: status %d", recorder.Code)
	}

	handle := strings.SplitN(recorder.Header().Get("Location"), "#oauth=", 2)[1]
	decoded, _ := url.QueryUnescape(handle)
	if status := decode(t, finish(t, handler, map[string]any{"handle": decoded}))["status"]; status != "expired" {
		t.Errorf("a forged state resolved to %v, want expired", status)
	}
}

/*
TestPKCEAndTheSecretGoToTheProvider.

The verifier is what stops an authorization code obtained elsewhere being
redeemed here. It is not visible in any response, so the only way to check it is
sent is to look at what the provider received.
*/
func TestPKCEAndTheSecretGoToTheProvider(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "pkce-1", "preferred_username": "pkce"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	callback(t, handler, begin(t, handler, ""))

	if provider.lastTokenForm.Get("code_verifier") == "" {
		t.Error("the token exchange carried no PKCE verifier")
	}
	if provider.lastTokenForm.Get("client_secret") != "secret" {
		t.Error("the token exchange carried no client secret")
	}
	if provider.lastTokenForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", provider.lastTokenForm.Get("grant_type"))
	}
}

// TestTheAuthorizationURLCarriesAChallenge: the other half of PKCE, which the
// provider sees at the start rather than at the exchange.
func TestTheAuthorizationURLCarriesAChallenge(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "challenge-1"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/auth/oauth/testing/start", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("start: status %d", recorder.Code)
	}

	raw, _ := decode(t, recorder)["url"].(string)
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("the authorization url could not be parsed: %v", err)
	}

	query := parsed.Query()
	if query.Get("code_challenge") == "" {
		t.Error("the authorization url carries no challenge")
	}
	if got := query.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if got := query.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want code", got)
	}
	if got := query.Get("scope"); got != "openid email" {
		t.Errorf("scope = %q, want the configured scopes", got)
	}
	// The secret belongs in the exchange, which happens server to server. In
	// the authorization url it would be in the browser's address bar.
	if strings.Contains(raw, "secret") {
		t.Error("the client secret is in the authorization url")
	}
}

/*
TestTheOutcomeIsNotInTheRedirectURL.

Whatever the callback decided reaches the client through a handle, and the
handle travels in the fragment. A query string would put it in browser history,
in the referer header and in every proxy log between here and there — and what
it stands for may be a session token.
*/
func TestTheOutcomeIsNotInTheRedirectURL(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{
		"sub": "private-1", "email": "private@example.test", "email_verified": true,
	})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet,
		"/api/auth/oauth/callback?state="+url.QueryEscape(begin(t, handler, ""))+"&code=granted", nil, "")
	location := recorder.Header().Get("Location")

	before, _, _ := strings.Cut(location, "#")
	if strings.Contains(before, "?") {
		t.Errorf("the redirect carries a query string: %q", before)
	}
	for _, leaked := range []string{"private@example.test", "signed-in", "link-offered", "new-account"} {
		if strings.Contains(location, leaked) {
			t.Errorf("the redirect url discloses %q: %s", leaked, location)
		}
	}
}

// TestAProviderThatRefusesIsNotAnError: a code already used, or a provider
// having a bad day, ends the sign-in without a 500.
func TestAProviderThatRefusesIsNotAnError(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "refused-1"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	state := begin(t, handler, "")
	provider.refuseToken = true

	recorder := call(t, handler, http.MethodGet,
		"/api/auth/oauth/callback?state="+url.QueryEscape(state)+"&code=granted", nil, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("a refused exchange: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	handle := strings.SplitN(recorder.Header().Get("Location"), "#oauth=", 2)[1]
	decoded, _ := url.QueryUnescape(handle)
	if status := decode(t, finish(t, handler, map[string]any{"handle": decoded}))["status"]; status != "failed" {
		t.Errorf("a refused exchange resolved to %v, want failed", status)
	}
}

// TestDecliningAtTheProviderIsSaidPlainly: somebody who changed their mind is
// not an error either.
func TestDecliningAtTheProviderIsSaidPlainly(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "declined-1"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet,
		"/api/auth/oauth/callback?error=access_denied&state="+url.QueryEscape(begin(t, handler, "")), nil, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("declined: status %d", recorder.Code)
	}

	handle := strings.SplitN(recorder.Header().Get("Location"), "#oauth=", 2)[1]
	decoded, _ := url.QueryUnescape(handle)
	if status := decode(t, finish(t, handler, map[string]any{"handle": decoded}))["status"]; status != "declined" {
		t.Errorf("declining resolved to %v, want declined", status)
	}
}

// TestAServerWithNoProvidersOffersNone: the ordinary server, where these routes
// answer as if they were not there.
func TestAServerWithNoProvidersOffersNone(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodGet, "/api/auth/oauth/testing/start", nil, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("starting a sign-in on a server with no providers: status %d, want %d",
			recorder.Code, http.StatusNotFound)
	}

	info := decode(t, call(t, handler, http.MethodGet, "/api/server", nil, ""))
	if _, present := info["signInProviders"]; present {
		t.Error("a server with no providers still advertises the field")
	}
}

// TestProvidersAreAdvertisedForTheSignInScreen, which is how a client knows to
// draw a button at all.
func TestProvidersAreAdvertisedForTheSignInScreen(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "advertised-1"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	info := decode(t, call(t, handler, http.MethodGet, "/api/server", nil, ""))
	listed, _ := info["signInProviders"].([]any)
	if len(listed) != 1 {
		t.Fatalf("got %d providers advertised, want 1", len(listed))
	}

	first, _ := listed[0].(map[string]any)
	if first["name"] != "testing" || first["label"] != "Test Provider" {
		t.Errorf("advertised %v, want the configured name and label", first)
	}
	// A button needs a name and a label. Anything else here is a secret that
	// has escaped into an unauthenticated response.
	if len(first) != 2 {
		t.Errorf("the advertised provider carries %d fields: %v", len(first), first)
	}
}

// TestAnUnknownProviderIsNotFound: the name comes from the caller's own URL.
func TestAnUnknownProviderIsNotFound(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"sub": "unknown-1"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodGet, "/api/auth/oauth/nosuch/start", nil, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("an unconfigured provider: status %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

/*
TestBothShapesOfSubjectAreAccepted.

There is no agreed type for the provider's own identifier: OpenID Connect
defines "sub" as a string and most providers send one that is not numeric, while
GitHub and GitLab send "id" as a bare number. Reading only one shape fails at the
last step of a sign-in that otherwise worked — the person has already approved
at the provider and comes back to an error.
*/
func TestBothShapesOfSubjectAreAccepted(t *testing.T) {
	shapes := map[string]map[string]any{
		"a string subject, as OpenID Connect defines it": {
			"sub": "8f14e45f-ceea-467a-9f3c-1b2c3d4e5f60", "preferred_username": "oidc",
		},
		"a numeric id, as GitHub and GitLab send": {
			"id": 4815162342, "login": "numeric",
		},
		"a numeric subject": {
			"sub": 1234567, "preferred_username": "numericsub",
		},
	}

	for name, account := range shapes {
		t.Run(name, func(t *testing.T) {
			provider := newFakeProvider(t, account)
			handler := withProvider(t, provider)
			completeSetup(t, handler)

			recorder := finish(t, handler, map[string]any{
				"handle": callback(t, handler, begin(t, handler, "")),
			})
			if recorder.Code != http.StatusOK {
				t.Fatalf("finish: status %d, body %s", recorder.Code, recorder.Body.String())
			}
			if status := decode(t, recorder)["status"]; status != "signed-in" {
				t.Errorf("resolved to %v, want a completed sign-in", status)
			}
		})
	}
}

// TestAProviderThatNamesNobodyIsRefused: without a stable identifier there is
// nothing to key a credential on, and keying it on the email would mean the
// account moves whenever the email does.
func TestAProviderThatNamesNobodyIsRefused(t *testing.T) {
	provider := newFakeProvider(t, map[string]any{"email": "nobody@example.test"})
	handler := withProvider(t, provider)
	completeSetup(t, handler)

	recorder := finish(t, handler, map[string]any{
		"handle": callback(t, handler, begin(t, handler, "")),
	})
	if status := decode(t, recorder)["status"]; status != "failed" {
		t.Errorf("a provider naming nobody resolved to %v, want failed", status)
	}
}
