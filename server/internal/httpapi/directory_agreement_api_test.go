package httpapi_test

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

/*
Proposals, negotiation and the agreement.

What both people said is evidence, and these test that it stays evidence: a
sent version and a recorded agreement cannot be changed from the API or the
database, an agreement needs both people on the same version, and it says
plainly that the server is not a party to it.
*/

// engagementID starts an engagement between a client and juma, returning its
// identifier.
func engagementID(t *testing.T, handler http.Handler, client, juma string) string {
	t.Helper()
	return engagementBetween(t, handler, client, juma)["id"].(string)
}

// propose sends a proposal version and returns its identifier.
func propose(t *testing.T, handler http.Handler, token, id, scope string, amounts ...int64) string {
	t.Helper()
	items := []map[string]any{}
	for index, amount := range amounts {
		items = append(items, map[string]any{"description": "Deliverable " + string(rune('A'+index)), "amountMinor": amount})
	}
	recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/proposals", map[string]any{
		"scope": scope, "timeline": "Six weeks from agreement", "currency": "TZS", "lineItems": items,
	}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("proposing returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)["id"].(string)
}

// respond answers a version; returns the status.
func respond(t *testing.T, handler http.Handler, token, id, version, kind, part, comment string) int {
	t.Helper()
	return call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/proposals/"+version+"/responses",
		map[string]string{"kind": kind, "part": part, "comment": comment}, token).Code
}

func negotiation(t *testing.T, handler http.Handler, token, id string) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id+"/proposals", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the negotiation returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

func stateOf(t *testing.T, handler http.Handler, token, id string) string {
	t.Helper()
	_, engagement, body := engagementAs(t, handler, token, id)
	if engagement == nil {
		t.Fatalf("reading the engagement: %s", body)
	}
	return engagement["state"].(string)
}

/*
A proposal states scope, deliverables, timeline and price, and a revision is a
new version: every earlier one stays, for both people.
*/
func TestARevisionIsANewVersionAndEveryVersionStays(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := engagementID(t, handler, client, juma)

	propose(t, handler, juma, id, "A booking app", 300000_00, 150000_00)
	if state := stateOf(t, handler, client, id); state != "proposal" {
		t.Errorf("after a proposal the engagement is %s", state)
	}
	propose(t, handler, juma, id, "A booking app, with SMS reminders", 300000_00, 200000_00)

	for name, token := range map[string]string{"client": client, "developer": juma} {
		versions := negotiation(t, handler, token, id)["versions"].([]any)
		if len(versions) != 2 {
			t.Fatalf("the %s sees %d versions, want both", name, len(versions))
		}
		first, second := versions[0].(map[string]any), versions[1].(map[string]any)
		if first["scope"] != "A booking app" || first["totalMinor"] != float64(450000_00) ||
			second["scope"] != "A booking app, with SMS reminders" || second["totalMinor"] != float64(500000_00) {
			t.Errorf("the %s sees versions %v and %v", name, first, second)
		}
		if first["currency"] != "TZS" || first["timeline"] != "Six weeks from agreement" || len(first["lineItems"].([]any)) != 2 {
			t.Errorf("a version lost part of what it stated: %v", first)
		}
	}
}

/*
Nothing sent can be rewritten: there is no route that would, and the database
refuses an update from anywhere — while still letting an account be deleted,
which only blanks the reference to it.
*/
func TestSentRecordsCannotBeChanged(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := engagementID(t, handler, client, juma)
	version := propose(t, handler, juma, id, "A booking app", 450000_00)
	respond(t, handler, juma, id, version, "accept", "", "")
	respond(t, handler, client, id, version, "accept", "", "")

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, path := range []string{
			"/api/directory/engagements/" + id + "/proposals/" + version,
			"/api/directory/engagements/" + id + "/agreement",
		} {
			if recorder := call(t, handler, method, path, map[string]any{"scope": "Something else"}, juma); recorder.Code < 400 {
				t.Errorf("%s %s was answered %d", method, path, recorder.Code)
			}
		}
	}

	ctx := context.Background()
	for table, statement := range map[string]string{
		"proposal_versions":     "UPDATE proposal_versions SET scope = 'rewritten', total_minor = 1 WHERE engagement_id = $1",
		"proposal_responses":    "UPDATE proposal_responses SET kind = 'decline' WHERE engagement_id = $1",
		"engagement_agreements": "UPDATE engagement_agreements SET terms = '{}' WHERE engagement_id = $1",
	} {
		if _, err := poolForTest.Exec(ctx, statement, id); err == nil {
			t.Errorf("the database let %s be rewritten", table)
		}
	}
	terms := negotiation(t, handler, client, id)["agreement"].(map[string]any)["terms"].(map[string]any)
	if terms["scope"] != "A booking app" {
		t.Errorf("the agreed terms changed: %v", terms)
	}

	// An account can still be deleted; the record stays for the other party.
	if _, err := poolForTest.Exec(ctx, "DELETE FROM users WHERE username = 'juma'"); err != nil {
		t.Fatalf("deleting an account with a recorded agreement failed: %v", err)
	}
	if agreement := negotiation(t, handler, client, id)["agreement"]; agreement == nil {
		t.Error("deleting one party's account took the agreement from the other")
	}
}

/*
A change request names the part of the proposal it is about, is recorded
against it, and the developer is notified; the engagement moves to
negotiation, where the open version can no longer be accepted.
*/
func TestAChangeRequestIsAttachedToItsPartAndNotifies(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := engagementID(t, handler, client, juma)
	version := propose(t, handler, juma, id, "A booking app", 300000_00, 150000_00)
	unread := func() float64 {
		return decode(t, call(t, handler, http.MethodGet, "/api/me/notifications/count", nil, juma))["unread"].(float64)
	}
	before := unread()

	if code := respond(t, handler, client, id, version, "request_changes", "", "Too dear"); code != http.StatusBadRequest {
		t.Errorf("a change request naming no part was answered %d", code)
	}
	if code := respond(t, handler, client, id, version, "request_changes", "item:9", "Too dear"); code != http.StatusBadRequest {
		t.Errorf("a change request naming a line that is not there was answered %d", code)
	}
	if code := respond(t, handler, client, id, version, "request_changes", "item:2", "Can the reminders cost less?"); code != http.StatusCreated {
		t.Fatalf("a change request was answered %d", code)
	}

	responses := negotiation(t, handler, juma, id)["responses"].([]any)
	last := responses[len(responses)-1].(map[string]any)
	if last["kind"] != "request_changes" || last["part"] != "item:2" || last["version"] != float64(1) ||
		last["responder"] != "client" || last["comment"] != "Can the reminders cost less?" {
		t.Errorf("the change request was not recorded against its part: %v", last)
	}
	if unread() != before+1 {
		t.Error("the developer was not notified of the change request")
	}
	if listed := notificationsOf(t, handler, juma); !strings.Contains(mustJSON(t, listed), "item:2") {
		t.Errorf("the notification does not say what was asked: %v", listed)
	}
	if state := stateOf(t, handler, juma, id); state != "negotiation" {
		t.Errorf("after a change request the engagement is %s", state)
	}
	if code := respond(t, handler, juma, id, version, "accept", "", ""); code != http.StatusConflict {
		t.Errorf("a version with changes asked for was accepted: %d", code)
	}
}

/*
The agreement needs both people's acceptance of the same version.

One acceptance moves nothing; acceptances of different versions are not an
agreement; and a superseded version can no longer be accepted at all.
*/
func TestAnAgreementNeedsBothOnTheSameVersion(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := engagementID(t, handler, client, juma)

	first := propose(t, handler, juma, id, "Version one", 400000_00)
	if code := respond(t, handler, juma, id, first, "accept", "", ""); code != http.StatusCreated {
		t.Fatalf("accepting returned %d", code)
	}
	if code := respond(t, handler, juma, id, first, "accept", "", ""); code != http.StatusConflict {
		t.Errorf("accepting twice was answered %d", code)
	}
	if state := stateOf(t, handler, client, id); state != "proposal" {
		t.Errorf("one acceptance moved the engagement to %s", state)
	}
	if negotiation(t, handler, client, id)["agreement"] != nil {
		t.Fatal("one acceptance recorded an agreement")
	}

	respond(t, handler, client, id, first, "request_changes", "scope", "Add reminders")
	second := propose(t, handler, juma, id, "Version two", 450000_00)
	if code := respond(t, handler, client, id, first, "accept", "", ""); code != http.StatusConflict {
		t.Errorf("a superseded version was accepted: %d", code)
	}
	respond(t, handler, client, id, second, "accept", "", "")
	if negotiation(t, handler, client, id)["agreement"] != nil {
		t.Fatal("acceptances of two different versions recorded an agreement")
	}

	respond(t, handler, juma, id, second, "accept", "", "")
	agreement, _ := negotiation(t, handler, juma, id)["agreement"].(map[string]any)
	if agreement == nil || agreement["version"] != float64(2) || agreement["terms"].(map[string]any)["scope"] != "Version two" {
		t.Fatalf("mutual acceptance of version two did not record it: %v", agreement)
	}
	if agreement["clientAcceptedAt"] == "" || agreement["developerAcceptedAt"] == "" {
		t.Error("the agreement does not hold both acceptance times")
	}
	if state := stateOf(t, handler, client, id); state != "agreement" {
		t.Errorf("after both accepted the engagement is %s", state)
	}
}

// A decline lets either person propose again, or withdraw.
func TestADeclineLetsEitherProposeAgainOrWithdraw(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := engagementID(t, handler, client, juma)

	version := propose(t, handler, juma, id, "First", 400000_00)
	if code := respond(t, handler, client, id, version, "decline", "", "Not what we need"); code != http.StatusCreated {
		t.Fatalf("declining returned %d", code)
	}
	if state := stateOf(t, handler, juma, id); state != "negotiation" {
		t.Errorf("after a decline the engagement is %s", state)
	}
	propose(t, handler, client, id, "What we need", 350000_00)
	if state := stateOf(t, handler, juma, id); state != "proposal" {
		t.Errorf("after the client proposed again the engagement is %s", state)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/withdraw", nil, juma); recorder.Code != http.StatusOK {
		t.Errorf("withdrawing during negotiation returned %d", recorder.Code)
	}
}

/*
Both parties export the same agreement, in full, with the same statement that
the server is not a party to it — the statement the in-app view carries too.
*/
func TestBothPartiesExportTheSameAgreement(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	other := registerAndSignIn(t, handler, "other")
	id := engagementID(t, handler, client, juma)

	if recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id+"/agreement/export", nil, client); recorder.Code != http.StatusNotFound {
		t.Errorf("exporting before an agreement was answered %d", recorder.Code)
	}
	version := propose(t, handler, juma, id, "A booking app", 300000_00, 150000_00)
	respond(t, handler, client, id, version, "accept", "", "")
	respond(t, handler, juma, id, version, "accept", "", "")

	exports := map[string]map[string]any{}
	for name, token := range map[string]string{"client": client, "developer": juma} {
		recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id+"/agreement/export", nil, token)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Disposition"), "attachment") {
			t.Fatalf("the %s's export returned %d", name, recorder.Code)
		}
		export := decode(t, recorder)
		delete(export, "exportedAt")
		exports[name] = export
	}
	if !reflect.DeepEqual(exports["client"], exports["developer"]) {
		t.Errorf("the two exports differ:\n%v\n%v", exports["client"], exports["developer"])
	}

	export := exports["client"]
	terms := export["terms"].(map[string]any)
	if export["format"] != "tdc.directory.agreement/1" || terms["totalMinor"] != float64(450000_00) ||
		terms["currency"] != "TZS" || len(terms["lineItems"].([]any)) != 2 ||
		export["client"].(map[string]any)["acceptedAt"] == "" || export["developer"].(map[string]any)["acceptedAt"] == "" {
		t.Errorf("the export is not the full agreement: %v", export)
	}

	disclosure, _ := export["disclosure"].(string)
	if disclosure != negotiation(t, handler, client, id)["disclosure"] {
		t.Error("the export and the in-app view carry different statements")
	}
	for _, says := range []string{"not a party to this agreement", "escrow", "guarantee", "resolve disputes"} {
		if !strings.Contains(disclosure, says) {
			t.Errorf("the statement does not cover %q: %s", says, disclosure)
		}
	}

	if recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id+"/agreement/export", nil, other); recorder.Code != http.StatusNotFound {
		t.Errorf("somebody outside the engagement exported its agreement: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id+"/proposals", nil, other); recorder.Code != http.StatusNotFound {
		t.Errorf("somebody outside the engagement read its terms: %d", recorder.Code)
	}
}

/*
Completion needs the other party's confirmation. Asked for and unconfirmed, it
stays outstanding — for both to see, however long it waits — and once confirmed
the record, its agreement and its history all remain.
*/
func TestCompletionNeedsTheOtherParty(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := engagementID(t, handler, client, juma)
	version := propose(t, handler, juma, id, "A booking app", 450000_00)
	respond(t, handler, client, id, version, "accept", "", "")
	respond(t, handler, juma, id, version, "accept", "", "")

	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/complete", nil, juma); recorder.Code != http.StatusConflict {
		t.Errorf("completion before the work started was answered %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/start", nil, client); recorder.Code != http.StatusOK {
		t.Fatalf("starting work returned %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/complete",
		map[string]string{"note": "Delivered and deployed"}, juma); recorder.Code != http.StatusOK {
		t.Fatalf("asking for completion returned %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/confirm-completion", nil, juma); recorder.Code != http.StatusForbidden {
		t.Errorf("the party who asked confirmed their own completion: %d", recorder.Code)
	}

	// A long while later, it is still outstanding.
	if _, err := poolForTest.Exec(context.Background(),
		"UPDATE engagements SET completion_requested_at = NOW() - interval '400 days' WHERE id = $1", id); err != nil {
		t.Fatalf("ageing the request: %v", err)
	}
	for name, token := range map[string]string{"client": client, "developer": juma} {
		_, seen, _ := engagementAs(t, handler, token, id)
		if seen["state"] != "completion_pending" || seen["completionRequestedBy"] != "juma" || seen["completionRequestedAt"] == nil {
			t.Errorf("the %s does not see the completion outstanding: %v", name, seen)
		}
	}

	if recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/confirm-completion", nil, client); recorder.Code != http.StatusOK {
		t.Fatalf("confirming returned %d", recorder.Code)
	}
	for name, token := range map[string]string{"client": client, "developer": juma} {
		_, seen, _ := engagementAs(t, handler, token, id)
		if seen["state"] != "completed" || len(seen["transitions"].([]any)) < 6 {
			t.Errorf("the %s does not see the completed record with its history: %v", name, seen)
		}
		if negotiation(t, handler, token, id)["agreement"] == nil {
			t.Errorf("the %s lost the agreement on completion", name)
		}
		if recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id+"/agreement/export", nil, token); recorder.Code != http.StatusOK {
			t.Errorf("the %s cannot export the agreement after completion: %d", name, recorder.Code)
		}
	}
}
