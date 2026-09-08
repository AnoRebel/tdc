package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
The setting has to be readable, not only writable.

The control that sets it opens showing a state, and the only honest source for
that state is the account. A checkbox that always opens unchecked would tell
somebody their messages are kept off their lock screen when they are not — a
promise broken in the place it was made.
*/
func TestTheHideContentPreferenceCanBeReadBack(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/me/preferences/hide-push-content", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the preference: status %d, body %s", recorder.Code, recorder.Body.String())
	}
	if hide := decode(t, recorder)["hide"]; hide != false {
		t.Errorf("a new account starts with hiding %v, which is not the documented default", hide)
	}

	if recorder := call(t, handler, http.MethodPut,
		"/api/me/preferences/hide-push-content", map[string]bool{"hide": true}, token,
	); recorder.Code != http.StatusNoContent {
		t.Fatalf("setting the preference: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	recorder = call(t, handler, http.MethodGet, "/api/me/preferences/hide-push-content", nil, token)
	if hide := decode(t, recorder)["hide"]; hide != true {
		t.Errorf("after asking for hiding the account still reports %v", hide)
	}

	// And back, because a preference that cannot be withdrawn is not a
	// preference.
	if recorder := call(t, handler, http.MethodPut,
		"/api/me/preferences/hide-push-content", map[string]bool{"hide": false}, token,
	); recorder.Code != http.StatusNoContent {
		t.Fatalf("clearing the preference: status %d", recorder.Code)
	}

	recorder = call(t, handler, http.MethodGet, "/api/me/preferences/hide-push-content", nil, token)
	if hide := decode(t, recorder)["hide"]; hide != false {
		t.Errorf("after withdrawing it the account still reports %v", hide)
	}
}

/*
One person's setting is their own.

Read from the requester rather than from anywhere shared: a preference keyed on
the wrong thing would hand somebody else's answer back, and here that answer is
a claim about where their messages appear.
*/
func TestTheHideContentPreferenceIsPerAccount(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	other := registerSecondAccount(t, handler, "amina")

	if recorder := call(t, handler, http.MethodPut,
		"/api/me/preferences/hide-push-content", map[string]bool{"hide": true}, founder,
	); recorder.Code != http.StatusNoContent {
		t.Fatalf("setting the preference: status %d", recorder.Code)
	}

	recorder := call(t, handler, http.MethodGet, "/api/me/preferences/hide-push-content", nil, other)
	if hide := decode(t, recorder)["hide"]; hide != false {
		t.Errorf("somebody else's choice reached this account as %v", hide)
	}
}

// The preference belongs to an account, so it is not answered to nobody.
func TestTheHideContentPreferenceNeedsAnAccount(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		recorder := call(t, handler, method, "/api/me/preferences/hide-push-content",
			map[string]bool{"hide": true}, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s answered a request with no session: status %d", method, recorder.Code)
		}
	}
}

/*
A server whose operator has generated no keys says so plainly.

Off is an ordinary way to run — notifications still exist inside the
application — and the browser has to be able to tell that apart from a failure,
because one hides a control and the other reports a problem.
*/
func TestAServerWithoutKeysSaysPushIsNotOffered(t *testing.T) {
	handler := newTestAPI(t)

	recorder := call(t, handler, http.MethodGet, "/api/push/config", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("asking about push: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	body := decode(t, recorder)
	if body["enabled"] != false {
		t.Errorf("a server with no keys reports push as %v", body["enabled"])
	}
	if _, present := body["publicKey"]; present {
		t.Error("a server with no keys still offered a key")
	}
}

/*
Subscribing to a server that cannot send is refused rather than accepted.

Storing the subscription would leave a device that appears subscribed and never
receives anything, which nobody investigates: an absent notification looks
exactly like a quiet afternoon.
*/
func TestSubscribingToAServerWithoutKeysIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/me/push/subscriptions", map[string]any{
		"endpoint": "https://push.example/endpoint",
		"keys":     map[string]string{"p256dh": "key", "auth": "secret"},
	}, token)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("subscribing to a server with no keys: status %d, body %s",
			recorder.Code, recorder.Body.String())
	}

	// And nothing was recorded, so the list stays honest.
	recorder = call(t, handler, http.MethodGet, "/api/me/push/subscriptions", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("listing subscriptions: status %d", recorder.Code)
	}
	var devices []map[string]any
	decodeInto(t, recorder, &devices)
	if len(devices) != 0 {
		t.Errorf("a refused subscription was stored anyway: %v", devices)
	}
}

// registerSecondAccount makes an ordinary account and returns its token, for
// the tests asking whether one person's setting reaches another.
func registerSecondAccount(t *testing.T, handler http.Handler, username string) string {
	t.Helper()

	recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": username, "password": testPassword,
	}, "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register %q: status %d, body %s", username, recorder.Code, recorder.Body.String())
	}

	return signIn(t, handler, username, testPassword)
}

// decodeInto reads a response that is not an object, which decode cannot.
func decodeInto(t *testing.T, recorder *httptest.ResponseRecorder, into any) {
	t.Helper()

	if err := json.Unmarshal(recorder.Body.Bytes(), into); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}
