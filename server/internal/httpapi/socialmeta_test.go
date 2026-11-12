package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
TestPublicChannelsPreview is the point of injecting metadata at all.

A client-rendered application returns an empty shell to anything that does not
run JavaScript — every link preview in every chat application, every crawler.
A public channel shared elsewhere previewed as a blank box with a URL, which is
indistinguishable from a broken link.
*/
func TestPublicChannelsPreview(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "mipango", "public")

	// Fetched the way a crawler would: no session, no JavaScript.
	recorder := call(t, handler, http.MethodGet, "/rooms/"+roomID, nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("fetching a public channel returned %d", recorder.Code)
	}

	body := recorder.Body.String()

	for _, want := range []string{
		"<title>",
		`property="og:title"`,
		`property="og:description"`,
		`name="twitter:card"`,
		"mipango",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the preview is missing %q:\n%s", want, truncate(body))
		}
	}
}

/*
TestPrivateChannelsPreviewAsNothing is the rule that keeps this from becoming a
disclosure.

A preview is generated for anybody holding the link, including somebody with no
account here — so anything put in one is published. A private channel therefore
gets no metadata at all, not even a generic "private channel" line, because
naming one confirms it exists.
*/
func TestPrivateChannelsPreviewAsNothing(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "bodi-ya-siri", "private")

	recorder := call(t, handler, http.MethodGet, "/rooms/"+roomID, nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("fetching returned %d", recorder.Code)
	}

	body := recorder.Body.String()

	if strings.Contains(body, "bodi-ya-siri") {
		t.Error("a private channel's name appears in the shell served to anybody with the link")
	}
	if strings.Contains(body, `property="og:title"`) {
		t.Errorf("a private channel produced preview metadata:\n%s", truncate(body))
	}
}

// TestUnknownRoomsPreviewAsNothing keeps a guessed identifier from confirming
// anything, and keeps a malformed one from erroring.
func TestUnknownRoomsPreviewAsNothing(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))
	completeSetup(t, handler)

	for _, path := range []string{
		"/rooms/00000000-0000-0000-0000-000000000000",
		"/rooms/not-a-uuid",
		"/rooms/",
	} {
		recorder := call(t, handler, http.MethodGet, path, nil, "")

		if recorder.Code != http.StatusOK {
			t.Errorf("%s returned %d; the shell should still be served", path, recorder.Code)
		}
		if strings.Contains(recorder.Body.String(), `property="og:title"`) {
			t.Errorf("%s produced preview metadata for a room that is not there", path)
		}
	}
}

/*
TestChannelMetadataIsEscaped is why every value goes through escaping.

A channel's name and topic are controlled by whoever created it. An unescaped
quote in a topic ends the attribute and lets the rest be read as markup — in a
document served from this origin, which is where markup does the most damage.
*/
func TestChannelMetadataIsEscaped(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	owner := completeSetup(t, handler)

	// A topic that closes the attribute and opens a script, set at creation
	// which is where a topic is set.
	created := call(t, handler, http.MethodPost, "/api/rooms", map[string]any{
		"slug":       "kawaida",
		"name":       "Kawaida",
		"visibility": "public",
		"topic":      `" onload="alert(1)"><script>alert(2)</script>`,
	}, owner)
	if created.Code != http.StatusCreated {
		t.Fatalf("creating returned %d: %s", created.Code, truncate(created.Body.String()))
	}

	roomID, _ := decode(t, created)["id"].(string)
	if roomID == "" {
		t.Fatal("the created room has no identifier")
	}

	recorder := call(t, handler, http.MethodGet, "/rooms/"+roomID, nil, "")
	body := recorder.Body.String()

	if strings.Contains(body, "<script>alert(2)</script>") {
		t.Error("a channel topic put an unescaped script tag into the shell")
	}
	if strings.Contains(body, `onload="alert(1)"`) {
		t.Error("a channel topic escaped its attribute and added an event handler")
	}
}

/*
TestTheServerItselfPreviews covers the link somebody shares to invite people to
the server at all.

The root is the address that gets posted when somebody says "join us here", and
it previewing as nothing is the first impression the whole product makes.
*/
func TestTheServerItselfPreviews(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	recorder := call(t, handler, http.MethodGet, "/", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("fetching the root returned %d", recorder.Code)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `property="og:title"`) {
		t.Errorf("the server's own address produced no preview:\n%s", truncate(body))
	}
	if !strings.Contains(body, "Test Server") {
		t.Error("the preview does not name the server")
	}
}

// TestTheShellStillWorks checks the rewriting did not break what it rewrites.
//
// The metadata is inserted into a document the application needs to start
// from: a malformed insertion would leave every page blank.
func TestTheShellStillWorks(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	recorder := call(t, handler, http.MethodGet, "/rooms/"+roomID, nil, "")
	body := recorder.Body.String()

	// Still the application's own shell, with its mount point intact.
	if !strings.Contains(body, "<div id=app>") {
		t.Errorf("the shell lost its content when metadata was inserted:\n%s", truncate(body))
	}
	// And the head is still a head.
	if strings.Count(body, "<head>") != 1 {
		t.Errorf("the shell has %d head tags after insertion", strings.Count(body, "<head>"))
	}
}

/*
TestInvitationLinksPreviewWithoutDisclosing is the rule that keeps an
invitation private while still previewing.

An invitation is posted into a chat application, which fetches the link to
preview it. Whatever comes back is shown to everybody in that conversation —
not only to the person being invited.

So it must not name the channel: an invitation to a private one would otherwise
announce its existence and its name to a room full of people who were not
invited. And it must not say whether the invitation is valid, or anybody could
test invitations by pasting them and reading the preview.
*/
func TestInvitationLinksPreviewWithoutDisclosing(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "bodi-ya-siri", "private")

	issued := issueInvitation(t, handler, owner, roomID, nil)

	// Fetched the way a chat application previews a link: no session.
	recorder := call(t, handler, http.MethodGet, "/invite/"+issued.Token, nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("previewing an invitation returned %d", recorder.Code)
	}

	body := recorder.Body.String()

	// It previews as something rather than a blank box.
	if !strings.Contains(body, `property="og:title"`) {
		t.Errorf("an invitation link produced no preview:\n%s", truncate(body))
	}

	// But names nothing about where it leads.
	if strings.Contains(body, "bodi-ya-siri") {
		t.Error("an invitation preview named the private channel it admits somebody to")
	}

	// And the secret is not echoed into the page for whoever renders it.
	if strings.Contains(body, issued.Token) {
		t.Error("an invitation preview repeated the invitation's own secret")
	}
}

/*
TestValidAndInvalidInvitationsPreviewAlike closes the oracle a preview would
otherwise open.

If a working invitation previewed differently from a revoked one, anybody could
test invitations by pasting them somewhere that renders previews — reaching the
same oracle the redemption endpoint refuses to be, by another route.
*/
func TestValidAndInvalidInvitationsPreviewAlike(t *testing.T) {
	handler := newTestAPIWithWebRoot(t, webRoot(t))

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "wageni", "private")

	working := issueInvitation(t, handler, owner, roomID, nil)

	revoked := issueInvitation(t, handler, owner, roomID, nil)
	if r := call(t, handler, http.MethodDelete, "/api/invitations/"+revoked.ID, nil, owner); r.Code != http.StatusNoContent {
		t.Fatalf("revoking returned %d", r.Code)
	}

	previews := map[string]string{}
	for name, token := range map[string]string{
		"working":       working.Token,
		"revoked":       revoked.Token,
		"never existed": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	} {
		recorder := call(t, handler, http.MethodGet, "/invite/"+token, nil, "")
		previews[name] = recorder.Body.String()
	}

	if previews["working"] != previews["revoked"] {
		t.Error("a working invitation previews differently from a revoked one, " +
			"which lets anybody test invitations by previewing them")
	}
	if previews["working"] != previews["never existed"] {
		t.Error("a real invitation previews differently from one that never existed")
	}
}
