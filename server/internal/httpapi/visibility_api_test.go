package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
TestAPublicServerDescribesItselfToAnybody is the default.

A server nobody can find is a server nobody joins, so an operator who wants
that should choose it rather than discover they have it.
*/
func TestAPublicServerDescribesItselfToAnybody(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")
	if recorder.Code != http.StatusOK {
		t.Errorf("a public server returned %d to a stranger, expected 200", recorder.Code)
	}
}

/*
TestAPrivateServerIsNotDiscoverable is the scenario the requirement names.

Not discoverable at all: somebody scanning addresses learns the same thing from
this one as from an address that has never existed.
*/
func TestAPrivateServerIsNotDiscoverable(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "server.visibility", "private")

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")

	if recorder.Code == http.StatusOK {
		t.Fatal("a private server described itself to a stranger")
	}
	/*
		404 rather than 403. A refusal that distinguished "you may not know"
		from "there is nothing here" would answer the question it is refusing:
		somebody learns the server exists from the shape of the denial.
	*/
	if recorder.Code != http.StatusNotFound {
		t.Errorf("a private server answered %d; anything but 404 discloses "+
			"that something is here", recorder.Code)
	}
}

/*
TestAPrivateServerStillAnswersItsOwnMembers is what keeps the setting usable.

Undiscoverable is not unreachable. Hiding a server from the people already on
it would make private visibility a way to break your own deployment.
*/
func TestAPrivateServerStillAnswersItsOwnMembers(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "server.visibility", "private")

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Errorf("a private server returned %d to its own administrator", recorder.Code)
	}

	member := registerAndSignIn(t, handler, "amina")
	if asMember := call(t, handler, http.MethodGet, "/api/server", nil, member); asMember.Code != http.StatusOK {
		t.Errorf("a private server returned %d to one of its members", asMember.Code)
	}
}

/*
TestALimitedServerAnswersOnlyTheServersItLists is the middle setting.

An unlisted requester is refused the same way a stranger is refused by a
private server, for the same reason.
*/
func TestALimitedServerAnswersOnlyTheServersItLists(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "server.visibility", "limited")
	setSetting(t, handler, owner, "server.visible_to", "vue.tdc.or.tz, go.tdc.or.tz")

	// Nobody in particular.
	if anonymous := call(t, handler, http.MethodGet, "/api/server", nil, ""); anonymous.Code == http.StatusOK {
		t.Error("a limited server described itself to a caller naming no server")
	}

	// A listed peer.
	listed := callAsPeer(t, handler, "/api/server", "vue.tdc.or.tz")
	if listed.Code != http.StatusOK {
		t.Errorf("a limited server refused a peer it lists: %d", listed.Code)
	}

	// Differently capitalised, which is the same domain.
	cased := callAsPeer(t, handler, "/api/server", "VUE.TDC.OR.TZ")
	if cased.Code != http.StatusOK {
		t.Error("a listed peer was refused for the capitalisation of its domain")
	}

	// One it does not list.
	unlisted := callAsPeer(t, handler, "/api/server", "somewhere.else")
	if unlisted.Code == http.StatusOK {
		t.Error("a limited server described itself to a server it does not list")
	}
}

/*
TestAPrivateRoomOnAPublicServerStaysUndiscoverable is the third clause of the
requirement, and the one that is about rooms rather than servers.

A server being findable says nothing about what inside it is.
*/
func TestAPrivateRoomOnAPublicServerStaysUndiscoverable(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)

	// A private room the owner is in, on a server anybody may find.
	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]any{
		"slug": "siri-kabisa", "name": "Siri kabisa", "visibility": "private",
	}, owner)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("creating a private room returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}

	// Somebody else on the same server.
	stranger := registerAndSignIn(t, handler, "amina")

	rooms := call(t, handler, http.MethodGet, "/api/rooms", nil, stranger)
	if rooms.Code != http.StatusOK {
		t.Fatalf("listing rooms returned %d", rooms.Code)
	}

	if strings.Contains(rooms.Body.String(), "siri-kabisa") {
		t.Errorf("a private room appeared to somebody who is not in it: %s",
			truncate(rooms.Body.String()))
	}
}

/*
TestAnUnknownVisibilityIsRefused keeps a typo from meaning something.

A misspelt visibility that fell through to public would be a server its
operator believes is hidden.
*/
func TestAnUnknownVisibilityIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/admin/settings/server.visibility",
		map[string]any{"value": "hidden"}, owner)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown visibility returned %d, expected 400", recorder.Code)
	}
}

// callAsPeer makes a request on behalf of another server, the way a federated
// peer would identify itself.
func callAsPeer(t *testing.T, handler http.Handler, path, peer string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("TDC-Peer-Server", peer)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
