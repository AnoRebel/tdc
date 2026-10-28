package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
A project on this server can list somebody from another server with a role,
once its administrators have opened it to federation — which only a server that
federates allows, which is audited, and which names nothing outside this server.
*/
func TestAProjectIsOpenedToOtherServersAndHoldsTheirPeople(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	member := registerMember(t, handler, "bola")
	startProject(t, handler, owner, "FED", "private")
	mustCall(t, handler, http.MethodPut, "/api/projects/FED/members/bola", map[string]any{"role": "member"}, owner, http.StatusNoContent)

	if lists(advertised(t, handler), "projects.federated") {
		t.Error("a server that does not federate offers to share projects")
	}
	if recorder := call(t, handler, http.MethodPut, "/api/projects/FED/federation", map[string]any{"federated": true}, owner); recorder.Code != http.StatusConflict {
		t.Fatalf("a server that does not federate opened a project: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, handler, http.MethodPut, "/api/projects/FED/members/dalila@elsewhere.example", map[string]any{"role": "member"}, owner); recorder.Code != http.StatusConflict {
		t.Fatalf("a server that does not federate took a member from elsewhere: %d", recorder.Code)
	}

	mustCall(t, handler, http.MethodPut, "/api/admin/settings/federation.enabled", map[string]any{"value": "true"}, owner, http.StatusNoContent)
	if !lists(advertised(t, handler), "projects.federated") {
		t.Error("a server that federates does not offer to share projects")
	}

	if recorder := call(t, handler, http.MethodPut, "/api/projects/FED/members/dalila@elsewhere.example", map[string]any{"role": "member"}, owner); recorder.Code != http.StatusConflict {
		t.Fatalf("a project not opened took a member from elsewhere: %d", recorder.Code)
	}
	refused := call(t, handler, http.MethodPut, "/api/projects/FED/federation", map[string]any{"federated": true}, member)
	if refused.Code != http.StatusForbidden || !strings.Contains(refused.Body.String(), "project.administer") {
		t.Fatalf("a member opened the project: %d %s", refused.Code, refused.Body.String())
	}

	opened := decode(t, mustCall(t, handler, http.MethodPut, "/api/projects/FED/federation", map[string]any{"federated": true}, owner, http.StatusOK))
	if opened["federated"] != true {
		t.Fatalf("the project is not shown as opened: %v", opened)
	}
	if project := decode(t, mustCall(t, handler, http.MethodGet, "/api/projects/FED", nil, member, http.StatusOK)); project["federated"] != true {
		t.Errorf("a member does not see that the project is opened: %v", project["federated"])
	}

	mustCall(t, handler, http.MethodPut, "/api/projects/FED/members/dalila@elsewhere.example", map[string]any{"role": "member"}, owner, http.StatusNoContent)
	mustCall(t, handler, http.MethodPut, "/api/projects/FED/members/dalila@elsewhere.example", map[string]any{"role": "admin"}, owner, http.StatusNoContent)
	if recorder := call(t, handler, http.MethodPut, "/api/projects/FED/members/@elsewhere.example", map[string]any{"role": "member"}, owner); recorder.Code != http.StatusBadRequest {
		t.Errorf("a malformed address was accepted: %d", recorder.Code)
	}

	mustCall(t, handler, http.MethodPut, "/api/admin/settings/federation.mode", map[string]any{"value": "blocklist"}, owner, http.StatusNoContent)
	mustCall(t, handler, http.MethodPut, "/api/admin/settings/federation.blocked_servers", map[string]any{"value": "blocked.example"}, owner, http.StatusNoContent)
	if recorder := call(t, handler, http.MethodPut, "/api/projects/FED/members/esi@blocked.example", map[string]any{"role": "member"}, owner); recorder.Code != http.StatusForbidden {
		t.Errorf("somebody on a blocked server was added: %d", recorder.Code)
	}

	members := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/projects/FED/members", nil, member, http.StatusOK).Body.Bytes())
	var remote map[string]any
	for _, row := range members {
		if row["address"] == "dalila@elsewhere.example" {
			remote = row
		}
		if row["address"] == "esi@blocked.example" {
			t.Error("the refused member is listed")
		}
	}
	if remote == nil || remote["role"] != "admin" || remote["remote"] != true || remote["username"] != "dalila@elsewhere.example" || remote["displayName"] != "dalila" {
		t.Fatalf("the member from elsewhere is listed as %v", remote)
	}

	history := decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/projects/FED/history", nil, member, http.StatusOK).Body.Bytes())
	var recorded bool
	for _, entry := range history {
		if entry["type"] == "project.federation_changed" {
			recorded = true
		}
	}
	if !recorded {
		t.Error("opening the project is not in its history")
	}

	audit := mustCall(t, handler, http.MethodGet, "/api/admin/audit", nil, owner, http.StatusOK).Body.String()
	if !strings.Contains(audit, "project.federation.changed") {
		t.Error("opening the project was not audited")
	}

	mustCall(t, handler, http.MethodDelete, "/api/projects/FED/members/dalila@elsewhere.example", nil, owner, http.StatusNoContent)
	members = decodeRows(t, mustCall(t, handler, http.MethodGet, "/api/projects/FED/members", nil, member, http.StatusOK).Body.Bytes())
	for _, row := range members {
		if row["remote"] == true {
			t.Errorf("a removed member from elsewhere is still listed: %v", row)
		}
	}
}
