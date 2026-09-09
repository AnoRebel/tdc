package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

/*
The appearance somebody chose, kept on their account.

Stored server-side rather than only in a browser so it follows them between
clients: a person who set the warm palette on the desktop application and
then opens the web one should not have to set it again. That is the whole
reason this is an API rather than a line in localStorage, so these check it
survives the round trip rather than only that the handler returns 200.
*/

func readAppearance(t *testing.T, handler http.Handler, token string) map[string]any {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/me/preferences/appearance", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the appearance: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("the appearance was not JSON: %v", err)
	}
	return body
}

func TestAnAppearanceSurvivesBeingSet(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	// Nothing chosen is the state everybody starts in, and it has to be
	// reportable: the client resolves an empty choice to the operator's
	// default, which it cannot do if the server sends something else.
	before := readAppearance(t, handler, token)
	if before["palette"] != "" || before["mode"] != "" || before["accent"] != "" {
		t.Errorf("a new account already has an appearance: %v", before)
	}

	recorder := call(t, handler, http.MethodPut, "/api/me/preferences/appearance", map[string]string{
		"palette": "congrator",
		"mode":    "dark",
		"accent":  "#4f46e5",
	}, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("setting the appearance: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	/*
		Read back rather than trusting the response. A handler that echoes what
		it was sent without writing it passes a check of its own reply, and the
		failure appears only on the next sign-in — somewhere else entirely.
	*/
	after := readAppearance(t, handler, token)
	if after["palette"] != "congrator" || after["mode"] != "dark" || after["accent"] != "#4f46e5" {
		t.Errorf("the appearance did not survive: %v", after)
	}
}

/*
Choosing nothing is a choice, and it has to be expressible.

Somebody clearing their accent means "use whatever the palette has", not "leave
the last one I picked" — so empty has to be storable rather than treated as an
absent field.
*/
func TestAnAppearanceCanBeCleared(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	call(t, handler, http.MethodPut, "/api/me/preferences/appearance", map[string]string{
		"palette": "congrator", "mode": "dark", "accent": "#4f46e5",
	}, token)

	recorder := call(t, handler, http.MethodPut, "/api/me/preferences/appearance", map[string]string{
		"palette": "", "mode": "", "accent": "",
	}, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("clearing the appearance: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	after := readAppearance(t, handler, token)
	if after["palette"] != "" || after["accent"] != "" {
		t.Errorf("clearing left something behind: %v", after)
	}
}

/*
A palette this server does not have.

Refused rather than stored, because an attribute selecting no rules looks like
the default and gives no reason why — and this is an API a program calls
directly, so nothing upstream has already narrowed the value to what a menu
offered.
*/
func TestAnUnknownPaletteIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	for _, value := range []string{"chartreuse", "Congrator", "default "} {
		recorder := call(t, handler, http.MethodPut, "/api/me/preferences/appearance",
			map[string]string{"palette": value}, token)

		// A trailing space is trimmed rather than refused, which is why
		// "default " is expected to succeed: somebody pasting a value should
		// not be told their whitespace is a different palette.
		want := http.StatusBadRequest
		if value == "default " {
			want = http.StatusOK
		}
		if recorder.Code != want {
			t.Errorf("palette %q: status %d, wanted %d", value, recorder.Code, want)
		}
	}
}

func TestAnUnknownModeIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/me/preferences/appearance",
		map[string]string{"mode": "sepia"}, token)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown appearance was accepted: status %d", recorder.Code)
	}
}

/*
An accent that is not a colour at all.

Checked here as well as in the browser, because the browser is one client of
this API rather than a gate in front of it: a program setting an accent
directly would otherwise store a value the client cannot use, which appears as
a setting that saves and does nothing.

Note what is *not* refused. Every colour that can be read has a readable pair
derived from it — the hue is kept and the lightness moved until each
appearance works — so there is no colour to refuse for being too pale or too
dark. Only a value that is not a colour is turned away.
*/
func TestAnAccentThatIsNotAColourIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/me/preferences/appearance",
		map[string]string{"accent": "rebeccapurple"}, token)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a value that is not a readable colour was accepted: status %d", recorder.Code)
	}

	// And the account is unchanged: a refused request that had already written
	// is worse than one that refuses, because the interface then disagrees
	// with the error.
	after := readAppearance(t, handler, token)
	if after["accent"] != "" {
		t.Errorf("a refused accent was stored anyway: %v", after["accent"])
	}
}

/*
Somebody else's appearance is not this person's.

The route is under /api/me, so this is really a check that it resolves the
account from the session rather than from anything in the request.
*/
func TestAnAppearanceIsPerPerson(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// A second account, made the way anybody makes one.
	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("registering a second person: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	other := signIn(t, handler, "amina", testPassword)

	call(t, handler, http.MethodPut, "/api/me/preferences/appearance",
		map[string]string{"palette": "congrator"}, owner)

	theirs := readAppearance(t, handler, other)
	if theirs["palette"] != "" {
		t.Errorf("one person's choice reached another: %v", theirs)
	}
}

// An appearance is something an account has, so asking without one is refused.
func TestAnAppearanceNeedsAnAccount(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/me/preferences/appearance", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous request was answered: status %d", recorder.Code)
	}
}
