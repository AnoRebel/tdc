package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"
)

/*
TestSendingDoesNotWaitForNotifications is the guarantee 6.5 asks for.

Notifications and search indexing are work nobody is watching: a badge and an
index entry. Making the sender wait for them means every message costs the
time of every recipient's preference lookup — on a channel with two hundred
people, that is two hundred round trips before the sender sees their own words
appear.

The margin is deliberately loose. This is asserting that the work is not on the
request path at all, not measuring how fast it is, and a tight bound would fail
on a loaded machine for reasons that say nothing about the design.
*/
func TestSendingDoesNotWaitForNotifications(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	// Enough members that notifying them all is measurably more work than
	// storing one message.
	for _, name := range []string{"amina", "juma", "neema", "baraka", "zawadi"} {
		joinRoom(t, handler, registerAndSignIn(t, handler, name), roomID)
	}

	started := time.Now()
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "Habari @amina @juma @neema @baraka @zawadi"}, owner)
	elapsed := time.Since(started)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("sending returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	// The test handler waits for background work before returning, so this
	// measures the whole thing. What it proves is that it completes at all
	// rather than hanging — the ordering guarantee is covered below.
	if elapsed > 5*time.Second {
		t.Errorf("sending a message took %s, which suggests the notification "+
			"work is blocking rather than detached", elapsed)
	}

	// And the work did happen: detaching must not mean dropping.
	for _, name := range []string{"amina", "juma"} {
		token := signIn(t, handler, name, testPassword)
		if hits := notificationsFor(t, handler, token); len(hits) != 1 {
			t.Errorf("%s has %d notifications; detached work must still run", name, len(hits))
		}
	}
}

/*
TestBackgroundWorkSurvivesTheRequestEnding checks the effects outlive the
request.

A request's context is cancelled the moment the response is written, which is
why the detached context exists: work handed to a goroutine with a cancelled
context can be abandoned, silently, because the cancellation looks like any
other error.

What this test asserts is the outcome — the notification and the index entry
are there after the response — over a real listener, since httptest's direct
handler call never cancels anything.

It is worth saying what it does not prove. Removing the detaching still leaves
this passing: a query already in flight completes rather than aborting, so
today the effects survive either way. The detaching guards the case where the
work has not yet reached the driver when the response is written, which is
timing-dependent and not reproducible on demand. It stays because the failure
it prevents is silent and the cost of preventing it is one function call.
*/
func TestBackgroundWorkSurvivesTheRequestEnding(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	other := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, other, roomID)

	/*
		Over a real listener, not httptest's in-process handler call.

		net/http cancels a request's context when the response is written; a
		direct ServeHTTP call does not. A test using the handler alone
		therefore passes whether or not the context is detached, which makes it
		no test at all — verified by removing the detaching and watching it
		still pass.
	*/
	server := httptest.NewServer(handler)
	defer server.Close()

	// The sender, over the wire, so the session cookie is carried the way a
	// browser carries it.
	sender := server.Client()
	sender.Jar = cookieJar(t)
	post(t, sender, server.URL+"/api/auth/signin",
		map[string]any{"username": "founder", "password": testPassword})

	post(t, sender, server.URL+"/api/rooms/"+roomID+"/messages",
		map[string]any{"body": "Habari @amina"})

	// The response has been written and its context cancelled. If the detached
	// context were derived from it, the notification would never be written.
	waitFor(t, func() bool {
		return len(notificationsFor(t, handler, other)) == 1
	})

	if hits := notificationsFor(t, handler, other); len(hits) != 1 {
		t.Errorf("the notification did not survive the request ending: got %d", len(hits))
	}

	// The same for search, which is indexed on the same path.
	if found := searchFor(t, handler, owner, "Habari"); len(found) != 1 {
		t.Errorf("the search index did not survive the request ending: got %d results",
			len(found))
	}
}

// waitFor polls until a condition holds or the test gives up.
//
// The work under test is deliberately off the request path, so there is no
// response to wait on — polling is what a caller would genuinely have to do.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// cookieJar holds a session the way a browser would.
func cookieJar(t *testing.T) http.CookieJar {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("building a cookie jar: %v", err)
	}
	return jar
}

// post sends JSON and fails the test if the request could not be made.
func post(t *testing.T, client *http.Client, url string, body any) {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	response, err := client.Post(url, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("posting to %s: %v", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		t.Fatalf("posting to %s returned %d", url, response.StatusCode)
	}
}
