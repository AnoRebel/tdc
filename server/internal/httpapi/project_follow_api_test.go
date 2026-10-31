package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
	"uuid"
)

/*
A trusted peer sees and may follow the public projects opened here, and no
others; its copy goes once the project is no longer public. Somebody there
asks to join through their own server, which must speak for them and hold the
copy; the project tells its administrators somebody is waiting, and letting
them in makes them a member.
*/
func TestAPublicProjectIsFollowedAndJoinedFromElsewhere(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	owner := enableFederation(t, handler)
	setSetting(t, handler, owner, "federation.allow_private_addresses", "true")
	server := serverBehind(t, handler)
	peer := newFakePeer(t, "peer-key")
	public := startProject(t, handler, owner, "PUB", "public")
	private := startProject(t, handler, owner, "PRI", "private")
	for _, key := range []string{"PUB", "PRI"} {
		mustCall(t, handler, http.MethodPut, "/api/projects/"+key+"/federation", map[string]any{"federated": true}, owner, http.StatusOK)
	}

	listed := peer.signed(t, handler, http.MethodGet, "/api/federation/projects", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"PUB"`) || strings.Contains(listed.Body.String(), `"PRI"`) {
		t.Fatalf("a peer browsing was shown %d %s; want PUB and not PRI", listed.Code, listed.Body)
	}
	if code := peer.signed(t, handler, http.MethodPost, "/api/federation/projects/follow", map[string]any{"project": private["id"]}).Code; code != http.StatusNotFound {
		t.Errorf("following a private project was answered %d", code)
	}
	if code := peer.signed(t, handler, http.MethodPost, "/api/federation/projects/follow", map[string]any{"project": public["id"]}).Code; code != http.StatusOK {
		t.Fatalf("following a public project was answered %d", code)
	}
	replicas := func() (count int, discarding bool) {
		t.Helper()
		_ = poolForTest.QueryRow(t.Context(),
			"SELECT count(*), bool_or(discarding) FROM project_replicas WHERE project_id = $1 AND server = $2 AND followed",
			public["id"], peer.host(t)).Scan(&count, &discarding)
		return count, discarding
	}
	server.DeliverDueForTest(t.Context())
	if count, discarding := replicas(); count != 1 || discarding {
		t.Fatalf("a follower with no member there is to hold %d copies, discarding %v; want one kept", count, discarding)
	}

	// Asking to join: only for somebody the peer hosts, from a server holding
	// the copy.
	if _, err := poolForTest.Exec(t.Context(), "UPDATE project_replicas SET offered_at = NOW() WHERE project_id = $1", public["id"]); err != nil {
		t.Fatal(err)
	}
	dalila := "dalila@" + peer.host(t)
	ask := func(actor string) int {
		return peer.signed(t, handler, http.MethodPost, "/api/federation/projects/join",
			map[string]any{"project": public["id"], "actor": actor}).Code
	}
	if code := ask("esi@somewhere.else"); code != http.StatusForbidden {
		t.Errorf("asking for somebody the peer does not host was answered %d", code)
	}
	if code := ask(dalila); code != http.StatusAccepted {
		t.Fatalf("asking to join was answered %d", code)
	}
	if code := ask(dalila); code != http.StatusAccepted {
		t.Errorf("asking again was answered %d", code)
	}
	if waiting := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/PUB", nil, owner, http.StatusOK))["joinRequests"]; waiting != float64(1) {
		t.Errorf("the project tells its administrator %v are waiting, want 1", waiting)
	}
	requests := mustCall(t, handler, http.MethodGet, "/api/projects/PUB/join-requests", nil, owner, http.StatusOK)
	if !strings.Contains(requests.Body.String(), dalila) {
		t.Fatalf("the requests are %s", requests.Body)
	}
	encoded := strings.ReplaceAll(dalila, "@", "%40")
	mustCall(t, handler, http.MethodPost, "/api/projects/PUB/join-requests/"+encoded, nil, owner, http.StatusNoContent)
	members := mustCall(t, handler, http.MethodGet, "/api/projects/PUB/members", nil, owner, http.StatusOK)
	if !strings.Contains(members.Body.String(), dalila) {
		t.Errorf("letting her in did not make her a member: %s", members.Body)
	}
	mustCall(t, handler, http.MethodPost, "/api/projects/PUB/join-requests/"+encoded, nil, owner, http.StatusNotFound)
	if code := ask(dalila); code != http.StatusConflict {
		t.Errorf("a member asking to join was answered %d", code)
	}

	// No longer public: the copy goes, even though it was followed — her
	// membership aside, which keeps a replica for her.
	mustCall(t, handler, http.MethodDelete, "/api/projects/PUB/members/"+encoded, nil, owner, http.StatusNoContent)
	mustCall(t, handler, http.MethodPatch, "/api/projects/PUB", map[string]any{"visibility": "private"}, owner, http.StatusOK)
	server.DeliverDueForTest(t.Context())
	if _, discarding := replicas(); !discarding {
		t.Error("a follower kept its copy of a project no longer public")
	}
}

/*
An offer naming nobody here is accepted only for a public project this server
asked to follow.
*/
func TestAFollowedProjectIsAcceptedWithNobodyNamed(t *testing.T) {
	handler := newTestAPIWithAddress(t, thisServer)
	owner := enableFederation(t, handler)
	setSetting(t, handler, owner, "federation.allow_private_addresses", "true")
	home := newFakePeer(t, "home-key")
	project := uuid.New()
	offer := map[string]any{"project": project, "name": "Website", "visibility": "public", "members": []string{}}
	if code := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer", offer).Code; code != http.StatusNotFound {
		t.Fatalf("an offer naming nobody, unasked, was answered %d", code)
	}
	// As following it does, once its home agreed.
	if _, err := poolForTest.Exec(t.Context(),
		"INSERT INTO rooms (id, name, kind, visibility, home_server, federated) VALUES ($1, 'Website', 'project', 'public', $2, TRUE)",
		project, home.host(t)); err != nil {
		t.Fatal(err)
	}
	if code := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer", offer).Code; code != http.StatusOK {
		t.Errorf("an offer for a project followed was answered %d", code)
	}
	offer["visibility"] = "private"
	if code := home.signed(t, handler, http.MethodPost, "/api/federation/projects/offer", offer).Code; code != http.StatusNotFound {
		t.Errorf("an offer for a private project with nobody here was answered %d", code)
	}
}
