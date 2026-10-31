package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/events"
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

/*
A change asked for through a replica is refused from a server speaking for
somebody it does not host, and from one holding no replica of the project —
before anything about the person's role is asked. From the server that does,
for its own member, it is made.
*/
func TestAChangeFromElsewhereNeedsTheRightServer(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	owner := enableFederation(t, handler)
	setSetting(t, handler, owner, "federation.allow_private_addresses", "true")
	peer := newFakePeer(t, "peer-key")
	project := startProject(t, handler, owner, "CHG", "private")
	mustCall(t, handler, http.MethodPut, "/api/projects/CHG/federation", map[string]any{"federated": true}, owner, http.StatusOK)
	member := "dalila@" + peer.host(t)
	// As the web client sends it: percent-encoded, and decoded by the router.
	mustCall(t, handler, http.MethodPut, "/api/projects/CHG/members/"+strings.ReplaceAll(member, "@", "%40"),
		map[string]any{"role": "member"}, owner, http.StatusNoContent)

	title := map[string]any{"kind": "file", "title": "From elsewhere"}
	ask := func(actor string) int {
		return peer.signed(t, handler, http.MethodPost, "/api/federation/projects/change",
			map[string]any{"project": project["id"], "actor": actor, "change": title}).Code
	}
	if code := ask(member); code != http.StatusNotFound {
		t.Errorf("a server holding no replica was answered %d", code)
	}
	if _, err := poolForTest.Exec(t.Context(),
		"INSERT INTO project_replicas (project_id, server, address, offered_at) VALUES ($1, $2, $3, NOW())",
		project["id"], peer.host(t), peer.address); err != nil {
		t.Fatal(err)
	}
	if code := ask("esi@somewhere.else"); code != http.StatusForbidden {
		t.Errorf("a server speaking for somebody it does not host was answered %d", code)
	}
	if code := ask(member); code != http.StatusOK {
		t.Fatalf("its own member's filing was answered %d", code)
	}
	issue := decode(t, mustCall(t, handler, http.MethodGet, "/api/issues/CHG-1", nil, owner, http.StatusOK))
	if issue["title"] != "From elsewhere" {
		t.Errorf("the issue filed is %v", issue["title"])
	}
}

/*
A replica asks its home for what it missed, and records from the first failure
when the home stopped answering — clearing it once the home answers again.
*/
func TestAReplicaSaysWhenItsHomeStoppedAnswering(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	owner := enableFederation(t, handler)
	setSetting(t, handler, owner, "federation.allow_private_addresses", "true")
	registerMember(t, handler, "dalila")
	home := newFakePeer(t, "home-key")
	server := serverBehind(t, handler)
	project := uuid.New()
	offered := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer",
		map[string]any{"project": project, "name": "Website", "visibility": "private", "members": []string{"dalila@tdc.test"}})
	if offered.Code != http.StatusOK {
		t.Fatalf("the offer was answered %d: %s", offered.Code, offered.Body)
	}
	// As the first delivery would have made it.
	if _, err := poolForTest.Exec(t.Context(), "INSERT INTO projects (id, key, key_folded, name) VALUES ($1, 'WEB@home', 'web@home', 'Website')", project); err != nil {
		t.Fatal(err)
	}
	since := func() *time.Time {
		t.Helper()
		var at *time.Time
		if err := poolForTest.QueryRow(t.Context(), "SELECT home_unreachable_since FROM projects WHERE id = $1", project).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	// The fake home knows no events path yet, so it does not answer.
	server.CheckReplicaHomesForTest(t.Context())
	first := since()
	if first == nil {
		t.Fatal("a home that did not answer was not recorded")
	}
	server.CheckReplicaHomesForTest(t.Context())
	if again := since(); again == nil || !again.Equal(*first) {
		t.Errorf("a second failure moved the time it stopped answering from %v to %v", first, again)
	}

	home.serve("events", []byte(`{"events":[]}`))
	server.CheckReplicaHomesForTest(t.Context())
	if at := since(); at != nil {
		t.Errorf("a home that answers again is still recorded as silent since %v", at)
	}

	// An operator's rebuild names the replica by its key, and only a replica.
	if summary, err := server.RebuildReplica(t.Context(), "web@home"); err != nil {
		t.Errorf("rebuilding the replica: %v", err)
	} else if !strings.Contains(summary, "replayed from") {
		t.Errorf("the rebuild said %q", summary)
	}
	startProject(t, handler, owner, "OWN", "private")
	if _, err := server.RebuildReplica(t.Context(), "OWN"); err == nil {
		t.Error("a project of this server's own was rebuilt as a replica")
	}
}

/*
A replica far behind its home catches up in one go, a batch at a time, rather
than a batch each time it asks.
*/
func TestAReplicaFarBehindCatchesUpInOneGo(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	owner := enableFederation(t, handler)
	setSetting(t, handler, owner, "federation.allow_private_addresses", "true")
	registerMember(t, handler, "dalila")
	server := serverBehind(t, handler)
	home := newFakePeer(t, "home-key")
	project := uuid.New()
	if offered := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer",
		map[string]any{"project": project, "name": "Website", "visibility": "private", "members": []string{"dalila@tdc.test"}}); offered.Code != http.StatusOK {
		t.Fatalf("the offer was answered %d: %s", offered.Code, offered.Body)
	}

	host := home.host(t)
	for sequence := int64(1); sequence <= 250; sequence++ {
		nonce, _ := events.NewNonce()
		event := events.Event{
			Room: project.String() + "@" + host, Sender: "amina@" + host, Origin: host,
			Type: events.TypeMessageSent, Sequence: sequence, CreatedAt: events.Now(), Nonce: nonce,
		}
		event.Content, _ = json.Marshal(map[string]any{"body": fmt.Sprint("message ", sequence)})
		if err := home.keyring.Sign(&event); err != nil {
			t.Fatal(err)
		}
		home.stream = append(home.stream, event)
	}

	server.CheckReplicaHomesForTest(t.Context())
	var held int
	if err := poolForTest.QueryRow(t.Context(), "SELECT count(*) FROM events WHERE room_id = $1", project).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held != 250 {
		t.Errorf("one catch-up stored %d of the home's 250 events", held)
	}
}
