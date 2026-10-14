package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

/*
Revising an agreement that is in force.

The agreement's own principles, applied again: a revision is a sequence of
versions nobody can rewrite, it is in force only once both parties accept the
same version, nobody accepts for anybody else, and until it is in force the
agreement already recorded is the one that holds.
*/

// agreed starts an engagement between a client and juma and has both accept
// one version, returning the engagement's identifier.
func agreed(t *testing.T, handler http.Handler, client, juma string) string {
	t.Helper()
	id := engagementID(t, handler, client, juma)
	version := propose(t, handler, juma, id, "A booking app", 300000_00, 150000_00)
	respond(t, handler, client, id, version, "accept", "", "")
	respond(t, handler, juma, id, version, "accept", "", "")
	return id
}

func termsBody(scope string, amounts ...int64) map[string]any {
	items := []map[string]any{}
	for index, amount := range amounts {
		items = append(items, map[string]any{"description": "Deliverable " + string(rune('A'+index)), "amountMinor": amount})
	}
	return map[string]any{"scope": scope, "timeline": "Six weeks from agreement", "currency": "TZS", "lineItems": items}
}

// openRevision proposes a revision; returns the status and, on success, the
// identifier of its first version.
func openRevision(t *testing.T, handler http.Handler, token, id, reason, scope string, amounts ...int64) (int, string) {
	t.Helper()
	body := termsBody(scope, amounts...)
	body["reason"] = reason
	recorder := call(t, handler, http.MethodPost, "/api/directory/engagements/"+id+"/revisions", body, token)
	if recorder.Code != http.StatusCreated {
		return recorder.Code, ""
	}
	return recorder.Code, decode(t, recorder)["version"].(map[string]any)["id"].(string)
}

func inForce(t *testing.T, handler http.Handler, token, id string) map[string]any {
	t.Helper()
	agreement, _ := negotiation(t, handler, token, id)["agreement"].(map[string]any)
	if agreement == nil {
		t.Fatal("there is no agreement in force")
	}
	return agreement
}

func postAs(t *testing.T, handler http.Handler, token, path string, body any) int {
	t.Helper()
	return call(t, handler, http.MethodPost, path, body, token).Code
}

/*
A revision is in force only when both parties accept the same version of it.
One acceptance changes nothing; a version with changes asked for cannot be
accepted; a superseded one cannot be either; and the work carries on in the
state it was in throughout.
*/
func TestARevisionIsInForceOnlyWhenBothAcceptTheSameVersion(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := agreed(t, handler, client, juma)
	if code := postAs(t, handler, client, "/api/directory/engagements/"+id+"/start", nil); code != http.StatusOK {
		t.Fatalf("starting the work returned %d", code)
	}

	code, second := openRevision(t, handler, client, id, "We need SMS reminders too", "A booking app with SMS", 300000_00, 200000_00)
	if code != http.StatusCreated {
		t.Fatalf("proposing a revision returned %d", code)
	}
	if agreement := inForce(t, handler, juma, id); agreement["version"] != float64(1) || agreement["revision"] != float64(0) {
		t.Fatalf("proposing a revision changed the agreement in force: %v", agreement)
	}

	respond(t, handler, client, id, second, "accept", "", "")
	if agreement := inForce(t, handler, juma, id); agreement["version"] != float64(1) {
		t.Fatal("one party's acceptance put the revision in force")
	}

	if code := respond(t, handler, juma, id, second, "request_changes", "item:2", "SMS costs more than that"); code != http.StatusCreated {
		t.Fatalf("asking for a change to a revision returned %d", code)
	}
	if code := respond(t, handler, juma, id, second, "accept", "", ""); code != http.StatusConflict {
		t.Errorf("a revised version with changes asked for was accepted: %d", code)
	}

	third := propose(t, handler, juma, id, "A booking app with SMS", 300000_00, 250000_00)
	if code := respond(t, handler, client, id, second, "accept", "", ""); code != http.StatusConflict {
		t.Errorf("a superseded version of the revision was accepted: %d", code)
	}
	respond(t, handler, client, id, third, "accept", "", "")
	if agreement := inForce(t, handler, juma, id); agreement["version"] != float64(1) {
		t.Fatal("the counter-version went into force with only the client's acceptance")
	}
	respond(t, handler, juma, id, third, "accept", "", "")

	for name, token := range map[string]string{"client": client, "developer": juma} {
		read := negotiation(t, handler, token, id)
		agreement := read["agreement"].(map[string]any)
		if agreement["version"] != float64(3) || agreement["revision"] != float64(1) ||
			agreement["terms"].(map[string]any)["totalMinor"] != float64(550000_00) {
			t.Errorf("the %s does not see the revised agreement in force: %v", name, agreement)
		}
		if agreements := read["agreements"].([]any); len(agreements) != 2 ||
			agreements[0].(map[string]any)["version"] != float64(1) {
			t.Errorf("the %s does not see both agreements: %v", name, agreements)
		}
		revision := read["revisions"].([]any)[0].(map[string]any)
		if revision["outcome"] != "accepted" || revision["reason"] != "We need SMS reminders too" || revision["openedBy"] != "client" {
			t.Errorf("the %s sees the revision as %v", name, revision)
		}
	}
	if state := stateOf(t, handler, client, id); state != "active" {
		t.Errorf("an accepted revision moved the engagement to %s", state)
	}
}

/*
One revision at a time; it can be withdrawn only by whoever opened it and
declined by either; and while one is open the work cannot be recorded as
complete. Once the work is confirmed complete nothing more can be revised.
*/
func TestRevisionsAreOneAtATimeAndSettledBeforeCompletion(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := agreed(t, handler, client, juma)
	base := "/api/directory/engagements/" + id

	if code, _ := openRevision(t, handler, juma, id, "Hosting costs rose", "A booking app", 300000_00, 180000_00); code != http.StatusCreated {
		t.Fatalf("revising in agreement returned %d", code)
	}
	if code, _ := openRevision(t, handler, client, id, "Something else", "Another app", 1); code != http.StatusConflict {
		t.Errorf("a second revision opened while one was open: %d", code)
	}
	postAs(t, handler, juma, base+"/start", nil)
	if code := postAs(t, handler, juma, base+"/complete", nil); code != http.StatusConflict {
		t.Errorf("completion was asked for while a revision was open: %d", code)
	}
	if code := postAs(t, handler, client, base+"/revisions/1/withdraw", nil); code != http.StatusForbidden {
		t.Errorf("the other party withdrew a revision they did not open: %d", code)
	}
	if code := postAs(t, handler, juma, base+"/revisions/1/withdraw", map[string]string{"reason": "Absorbing it"}); code != http.StatusOK {
		t.Fatalf("withdrawing a revision returned %d", code)
	}
	if code := postAs(t, handler, juma, base+"/revisions/1/withdraw", nil); code != http.StatusNotFound {
		t.Errorf("a withdrawn revision was withdrawn again: %d", code)
	}
	if agreement := inForce(t, handler, client, id); agreement["version"] != float64(1) {
		t.Errorf("a withdrawn revision changed the agreement: %v", agreement)
	}

	if code := postAs(t, handler, juma, base+"/complete", nil); code != http.StatusOK {
		t.Fatalf("asking for completion returned %d", code)
	}
	code, version := openRevision(t, handler, client, id, "One deliverable was dropped", "A booking app", 300000_00)
	if code != http.StatusCreated {
		t.Fatalf("revising while completion awaits confirmation returned %d", code)
	}
	if code := postAs(t, handler, client, base+"/confirm-completion", nil); code != http.StatusConflict {
		t.Errorf("completion was confirmed while a revision was open: %d", code)
	}
	if code := respond(t, handler, juma, id, version, "decline", "", "It was delivered"); code != http.StatusCreated {
		t.Fatalf("declining a revision returned %d", code)
	}
	revision := negotiation(t, handler, client, id)["revisions"].([]any)[1].(map[string]any)
	if revision["outcome"] != "declined" || revision["outcomeBy"] != "juma" || revision["outcomeNote"] != "It was delivered" {
		t.Errorf("the declined revision reads %v", revision)
	}
	if code := respond(t, handler, client, id, version, "accept", "", ""); code != http.StatusConflict {
		t.Errorf("a declined revision was accepted: %d", code)
	}
	if code := postAs(t, handler, client, base+"/confirm-completion", nil); code != http.StatusOK {
		t.Fatalf("confirming completion returned %d", code)
	}
	if code, _ := openRevision(t, handler, client, id, "Late change", "A booking app", 1); code != http.StatusConflict {
		t.Errorf("a completed engagement was revised: %d", code)
	}
}

// A revision needs a reason and a change, an agreement to revise, and a party
// to propose it.
func TestARevisionNeedsAReasonAChangeAndAnAgreement(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	other := registerAndSignIn(t, handler, "other")

	early := engagementID(t, handler, client, juma)
	if code, _ := openRevision(t, handler, client, early, "Already?", "A booking app", 1); code != http.StatusConflict {
		t.Errorf("a revision was opened before any agreement: %d", code)
	}

	id := agreed(t, handler, client, juma)
	if code, _ := openRevision(t, handler, client, id, "", "A booking app", 2); code != http.StatusBadRequest {
		t.Errorf("a revision without a reason was answered %d", code)
	}
	if code, _ := openRevision(t, handler, client, id, "No change", "A booking app", 300000_00, 150000_00); code != http.StatusBadRequest {
		t.Errorf("a revision changing nothing was answered %d", code)
	}
	if code, _ := openRevision(t, handler, other, id, "Not mine", "A booking app", 2); code != http.StatusNotFound {
		t.Errorf("somebody outside the engagement proposed a revision: %d", code)
	}
}

/*
Nobody accepts for anybody else, nothing recorded about a revision can be
rewritten, and an agreement cannot be recorded without both acceptances — by
the application, and by the database for any other path.
*/
func TestRevisionRecordsCannotBeForgedOrChanged(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	registerAndSignIn(t, handler, "other")
	id := agreed(t, handler, client, juma)
	ctx := context.Background()

	_, version := openRevision(t, handler, juma, id, "More screens", "A booking app with admin", 300000_00, 150000_00, 90000_00)
	respond(t, handler, juma, id, version, "accept", "", "")
	if code := respond(t, handler, juma, id, version, "accept", "", ""); code != http.StatusConflict {
		t.Errorf("one party accepted the same version twice: %d", code)
	}
	if agreement := inForce(t, handler, client, id); agreement["version"] != float64(1) {
		t.Fatal("one party's acceptances put a revision in force")
	}

	// Somebody outside the engagement cannot be recorded as answering it.
	if _, err := poolForTest.Exec(ctx, `
		INSERT INTO proposal_responses (id, version_id, engagement_id, responder_id, responder_label, kind)
		SELECT $1, $2, $3, id, username, 'accept' FROM users WHERE username = 'other'`,
		uuid.New(), version, id); err == nil {
		t.Error("the database recorded an acceptance by somebody who is not a party")
	}
	// Nor an agreement recorded with only one party's acceptance.
	if _, err := poolForTest.Exec(ctx, `
		INSERT INTO engagement_agreements (id, engagement_id, version_id, version, client_id, developer_id,
		       client_label, developer_label, client_accepted_at, developer_accepted_at, terms, revision_id)
		SELECT $1, e.id, v.id, v.version, e.client_id, e.developer_id, e.client_label, e.developer_label,
		       NOW(), NOW(), '{}', v.revision_id
		FROM engagements e JOIN proposal_versions v ON v.engagement_id = e.id
		WHERE e.id = $2 AND v.id = $3`, uuid.New(), id, version); err == nil {
		t.Error("the database recorded a revised agreement only one party accepted")
	}

	respond(t, handler, client, id, version, "accept", "", "")
	if agreement := inForce(t, handler, client, id); agreement["version"] != float64(2) {
		t.Fatalf("both acceptances did not put the revision in force: %v", agreement)
	}

	for table, statement := range map[string]string{
		"agreement_revisions":         "UPDATE agreement_revisions SET reason = 'rewritten' WHERE engagement_id = $1",
		"agreement_revision_outcomes": "UPDATE agreement_revision_outcomes SET outcome = 'declined' WHERE engagement_id = $1",
		"engagement_agreements":       "UPDATE engagement_agreements SET terms = '{}' WHERE engagement_id = $1",
		"proposal_versions":           "UPDATE proposal_versions SET total_minor = 1 WHERE engagement_id = $1",
	} {
		if _, err := poolForTest.Exec(ctx, statement, id); err == nil {
			t.Errorf("the database let %s be rewritten", table)
		}
	}

	if _, err := poolForTest.Exec(ctx, "DELETE FROM users WHERE username = 'juma'"); err != nil {
		t.Fatalf("deleting an account that opened a revision failed: %v", err)
	}
	if revisions := negotiation(t, handler, client, id)["revisions"].([]any); len(revisions) != 1 {
		t.Error("deleting one party's account took the revision from the other")
	}
}

/*
The export carries the agreement in force and how it came to be: every
agreement, and every revision with its versions and answers. Both parties get
the same document; the other party is told at each step.
*/
func TestTheExportCarriesEveryRevision(t *testing.T) {
	handler, _ := withDirectory(t)
	juma := publicDeveloper(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	id := agreed(t, handler, client, juma)
	unread := func(token string) float64 {
		return decode(t, call(t, handler, http.MethodGet, "/api/me/notifications/count", nil, token))["unread"].(float64)
	}

	before := unread(client)
	if code, _ := openRevision(t, handler, juma, id, "First thought", "A booking app", 1); code != http.StatusCreated {
		t.Fatalf("proposing a revision returned %d", code)
	}
	if unread(client) != before+1 {
		t.Error("the client was not notified of the proposed revision")
	}
	postAs(t, handler, juma, "/api/directory/engagements/"+id+"/revisions/1/withdraw", nil)

	_, version := openRevision(t, handler, juma, id, "Scope grew", "A booking app with payments", 300000_00, 150000_00, 120000_00)
	before = unread(juma)
	respond(t, handler, client, id, version, "accept", "", "")
	respond(t, handler, juma, id, version, "accept", "", "")
	if unread(juma) <= before {
		t.Error("the developer was not notified of the client's acceptance")
	}

	exports := map[string]map[string]any{}
	for name, token := range map[string]string{"client": client, "developer": juma} {
		recorder := call(t, handler, http.MethodGet, "/api/directory/engagements/"+id+"/agreement/export", nil, token)
		if recorder.Code != http.StatusOK {
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
	if export["version"] != float64(3) || export["revision"] != float64(2) ||
		export["terms"].(map[string]any)["totalMinor"] != float64(570000_00) {
		t.Errorf("the export does not lead with the agreement in force: %v", export)
	}
	if agreements := export["agreements"].([]any); len(agreements) != 2 {
		t.Errorf("the export holds %d agreements, want 2", len(agreements))
	}
	revisions := export["revisions"].([]any)
	if len(revisions) != 2 {
		t.Fatalf("the export holds %d revisions, want 2", len(revisions))
	}
	first, second := revisions[0].(map[string]any), revisions[1].(map[string]any)
	if first["outcome"] != "withdrawn" || second["outcome"] != "accepted" || second["reason"] != "Scope grew" {
		t.Errorf("the revisions export as %v and %v", first, second)
	}
	if versions := second["versions"].([]any); len(versions) != 1 || versions[0].(map[string]any)["version"] != float64(3) {
		t.Errorf("the accepted revision exports versions %v", versions)
	}
	if responses := second["responses"].([]any); len(responses) != 2 {
		t.Errorf("the accepted revision exports %d acceptances, want 2: %s", len(responses), fmt.Sprint(responses))
	}
}
