package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

/*
A project's home offers a replica only for somebody this server hosts; one
naming nobody here is refused. An offer cannot take over a room of this
server's own by naming its identifier, and a discard from a server that is not
the replica's home removes nothing.
*/
func TestAProjectOfferIsAcceptedOnlyForSomebodyHere(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	owner := enableFederation(t, handler)
	setSetting(t, handler, owner, "federation.allow_private_addresses", "true")
	registerMember(t, handler, "dalila")
	home := newFakePeer(t, "home-key")
	stranger := newFakePeer(t, "stranger-key", "localhost")

	project := uuid.New()
	nobody := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer",
		map[string]any{"project": project, "name": "Website", "visibility": "private", "members": []string{"ghost@tdc.test"}})
	if nobody.Code != http.StatusNotFound {
		t.Fatalf("an offer naming nobody here was answered %d", nobody.Code)
	}

	accepted := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer",
		map[string]any{"project": project, "name": "Website", "visibility": "private", "members": []string{"dalila@tdc.test"}})
	if accepted.Code != http.StatusOK {
		t.Fatalf("an offer for a member here was answered %d: %s", accepted.Code, accepted.Body)
	}
	var homeServer string
	if err := poolForTest.QueryRow(t.Context(), "SELECT home_server FROM rooms WHERE id = $1 AND kind = 'project'", project).Scan(&homeServer); err != nil {
		t.Fatalf("no replica room was adopted: %v", err)
	}
	if homeServer != home.host(t) {
		t.Errorf("the replica's home is %q, want the server that offered it", homeServer)
	}

	room := makeRoom(t, handler, owner, "general", "public")
	takeover := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer",
		map[string]any{"project": room, "name": "Mine now", "visibility": "public", "members": []string{"dalila@tdc.test"}})
	if takeover.Code == http.StatusOK {
		t.Error("an offer naming a room of this server's own was accepted")
	}
	var name, kind string
	if err := poolForTest.QueryRow(t.Context(), "SELECT name, kind FROM rooms WHERE id = $1", room).Scan(&name, &kind); err != nil || name == "Mine now" || kind != "channel" {
		t.Errorf("this server's own room became %q, %q (%v)", name, kind, err)
	}

	discarded := stranger.signed(t, handler, http.MethodPost, "/api/federation/projects/discard", map[string]any{"project": project})
	if discarded.Code != http.StatusOK || decode(t, discarded)["discarded"] != false {
		t.Errorf("a server that is not the home had the replica discarded: %d %s", discarded.Code, discarded.Body)
	}
	if gone := home.signed(t, handler, http.MethodPost, "/api/federation/projects/discard", map[string]any{"project": project}); decode(t, gone)["discarded"] != true {
		t.Errorf("the home could not have its replica discarded: %s", gone.Body)
	}
}
