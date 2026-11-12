package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

/*
Which introductions somebody has been given, kept on their account.

The point of storing this server-side is that a tour shown on one client is not
shown again on another — so these check the record survives the round trip and
that the server does not quietly reshape what the client stores.
*/

func readTours(t *testing.T, handler http.Handler, token string) map[string]any {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/me/preferences/tours", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading tour progress: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("tour progress was not an object: %v", err)
	}
	return body
}

/*
A new account has seen nothing, and says so as an object.

Null would be the honest answer to "never set" and the wrong one to send: the
client reads this into a structure, and every caller would need a special case
for an account that has not been anywhere yet.
*/
func TestANewAccountHasSeenNoTours(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	progress := readTours(t, handler, token)

	if len(progress) != 0 {
		t.Errorf("a new account has already seen something: %v", progress)
	}
}

func TestTourProgressSurvivesBeingSet(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/me/preferences/tours", map[string]any{
		"seen":    []string{"chat"},
		"reached": map[string]int{"chat": 3},
	}, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("setting tour progress: status %d, body %s", recorder.Code, recorder.Body.String())
	}

	/*
		Read back rather than trusting the reply. A handler that echoes what it
		was sent without writing it passes a check of its own response, and the
		failure appears on the next sign-in — which is exactly the moment this
		feature exists for.
	*/
	after := readTours(t, handler, token)

	seen, ok := after["seen"].([]any)
	if !ok || len(seen) != 1 || seen[0] != "chat" {
		t.Errorf("what was seen did not survive: %v", after["seen"])
	}

	reached, ok := after["reached"].(map[string]any)
	if !ok || reached["chat"] != float64(3) {
		t.Errorf("how far they reached did not survive: %v", after["reached"])
	}
}

/*
The server does not know what a tour is.

It stores what the client defines and hands it back unchanged, which is what
lets a later phase add an introduction without a migration. A server that
validated tour names would have to be changed every time one was added, and
would refuse a client newer than itself.
*/
func TestTheServerDoesNotInterpretTourNames(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	call(t, handler, http.MethodPut, "/api/me/preferences/tours", map[string]any{
		"seen":    []string{"a-tour-added-in-a-later-phase"},
		"reached": map[string]int{"another": 7},
	}, token)

	after := readTours(t, handler, token)

	seen, _ := after["seen"].([]any)
	if len(seen) != 1 || seen[0] != "a-tour-added-in-a-later-phase" {
		t.Errorf("a name the server has never heard of was not kept: %v", after)
	}
}

/*
Replacing rather than merging.

The client reads the record, changes it and writes it back. A merge would have
to know that finishing a tour makes its step number meaningless — which is
knowing what a tour is.
*/
func TestTourProgressIsReplacedNotMerged(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	call(t, handler, http.MethodPut, "/api/me/preferences/tours", map[string]any{
		"seen": []string{"chat"}, "reached": map[string]int{"chat": 3},
	}, token)

	// Forgetting a tour, which is what somebody asking to see it again does.
	call(t, handler, http.MethodPut, "/api/me/preferences/tours", map[string]any{
		"seen": []string{}, "reached": map[string]int{"chat": 0},
	}, token)

	after := readTours(t, handler, token)

	seen, _ := after["seen"].([]any)
	if len(seen) != 0 {
		t.Errorf("forgetting a tour left it seen: %v", after["seen"])
	}
}

/*
Something that is not an object.

The client reads this into a structure with named fields, so an array or a
number would be a runtime error in the interface rather than a refusal here —
and this is an API a program calls directly, with nothing upstream having
already shaped the value.
*/
func TestTourProgressMustBeAnObject(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	for _, body := range []any{[]string{"chat"}, "chat", 7} {
		recorder := call(t, handler, http.MethodPut, "/api/me/preferences/tours", body, token)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%v was accepted: status %d", body, recorder.Code)
		}
	}
}

/*
And a record too large to be one.

Without a limit the only bound is the request body size, which is set for
attachments — so an account could be used as storage by anything holding a
token for it.
*/
func TestAnOversizedTourRecordIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/me/preferences/tours", map[string]any{
		"seen": strings.Repeat("a", 8<<10),
	}, token)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("an oversized record was accepted: status %d", recorder.Code)
	}
}

// One person's progress is not another's, which is what /api/me means.
func TestTourProgressIsPerPerson(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	if recorder := call(t, handler, http.MethodPost, "/api/auth/register", map[string]string{
		"username": "amina", "password": testPassword,
	}, ""); recorder.Code != http.StatusCreated {
		t.Fatalf("registering a second person: status %d", recorder.Code)
	}
	other := signIn(t, handler, "amina", testPassword)

	call(t, handler, http.MethodPut, "/api/me/preferences/tours", map[string]any{
		"seen": []string{"chat"},
	}, owner)

	theirs := readTours(t, handler, other)
	if len(theirs) != 0 {
		t.Errorf("one person's progress reached another: %v", theirs)
	}
}

func TestTourProgressNeedsAnAccount(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/me/preferences/tours", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous request was answered: status %d", recorder.Code)
	}
}
