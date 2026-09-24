package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
Verification: a moderator attesting to one specific claim, distinguishably from
the claims beside it, and in a way that can be wrong and withdrawn.
*/

// withVerifier sets up the directory with a verifier holding directory.verify.
func withVerifier(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	handler, founder := withDirectory(t)
	verifier := registerAndSignIn(t, handler, "kheri")
	grantRole(t, handler, founder, verifier, "directory.verify")
	return handler, founder, verifier
}

// claimsOf reads the verifiable claims on a profile, keyed by label.
func claimsOf(t *testing.T, handler http.Handler, verifier, username string) map[string]map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/directory/moderation/profiles/"+username+"/claims", nil, verifier)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading claims returned %d: %s", recorder.Code, recorder.Body.String())
	}
	claims := map[string]map[string]any{}
	for _, claim := range decode(t, recorder)["claims"].([]any) {
		entry := claim.(map[string]any)
		claims[entry["kind"].(string)+":"+entry["label"].(string)] = entry
	}
	return claims
}

// verify grants a verification of one claim and returns its identifier.
func verify(t *testing.T, handler http.Handler, verifier, username string, claim map[string]any, note string) string {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/profiles/"+username+"/verifications",
		map[string]any{"kind": claim["kind"], "claimId": claim["id"], "note": note}, verifier)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("verifying returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)["id"].(string)
}

// verifiedSkills reads which skills a member is shown as verified.
func verifiedSkills(t *testing.T, handler http.Handler, viewer, username string) map[string]bool {
	t.Helper()
	status, view := viewProfile(t, handler, viewer, username)
	if status != http.StatusOK {
		t.Fatalf("viewing the profile returned %d", status)
	}
	verified := map[string]bool{}
	for _, skill := range view["skills"].([]any) {
		entry := skill.(map[string]any)
		verified[entry["name"].(string)] = entry["verified"] != nil
	}
	return verified
}

func threeSkills(t *testing.T, handler http.Handler, token string) {
	t.Helper()
	saveProfile(t, handler, token, map[string]any{
		"visibility": "members",
		"skills": []map[string]any{
			{"name": "Go", "description": "eight years"},
			{"name": "PostgreSQL", "description": "six years"},
			{"name": "Docker", "description": "every day"},
		},
		"portfolio": []map[string]any{{"title": "Clinic", "url": "https://example.com/clinic"}},
	})
}

/*
A verification must name exactly which claim it covers.

Without a kind and an identifier it is refused, and naming something that is not
a claim on the profile is refused too — there is no way to attach one to the
profile as a whole.
*/
func TestAVerificationNamesItsClaim(t *testing.T) {
	handler, _, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	threeSkills(t, handler, juma)
	path := "/api/directory/moderation/profiles/juma/verifications"

	for description, body := range map[string]map[string]any{
		"no claim at all":        {"note": "looked at it"},
		"a kind but no claim":    {"kind": "skill", "note": "looked at it"},
		"a claim but no kind":    {"claimId": accountID(t, handler, juma), "note": "looked at it"},
		"a kind that is not one": {"kind": "profile", "claimId": accountID(t, handler, juma), "note": "x"},
		"a claim not on it":      {"kind": "skill", "claimId": "00000000-0000-0000-0000-000000000001", "note": "x"},
	} {
		recorder := call(t, handler, http.MethodPost, path, body, verifier)
		if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusNotFound {
			t.Errorf("a verification with %s was answered %d", description, recorder.Code)
		}
	}

	// And nothing was recorded along the way.
	for _, claim := range claimsOf(t, handler, verifier, "juma") {
		if claim["verification"] != nil {
			t.Errorf("a refused verification left %v verified", claim["label"])
		}
	}
}

/*
Only somebody holding directory.verify grants a verification, with a note on
what was checked, and the grant is recorded with who and why.
*/
func TestGrantingIsPermittedAndRecorded(t *testing.T) {
	handler, founder, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	threeSkills(t, handler, juma)
	claim := claimsOf(t, handler, verifier, "juma")["skill:Go"]

	body := map[string]any{"kind": "skill", "claimId": claim["id"], "note": "Reviewed their public repositories"}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/profiles/juma/verifications", body, amina); recorder.Code != http.StatusForbidden {
		t.Fatalf("somebody without directory.verify granted a verification: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodGet, "/api/directory/moderation/profiles/juma/claims", nil, amina); recorder.Code != http.StatusForbidden {
		t.Errorf("somebody without directory.verify read the claims: %d", recorder.Code)
	}

	withoutNote := map[string]any{"kind": "skill", "claimId": claim["id"], "note": "  "}
	if recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/profiles/juma/verifications", withoutNote, verifier); recorder.Code != http.StatusBadRequest {
		t.Errorf("a verification with no note on what was checked was accepted: %d", recorder.Code)
	}

	id := verify(t, handler, verifier, "juma", claim, "Reviewed their public repositories")

	if recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/profiles/juma/verifications", body, verifier); recorder.Code != http.StatusConflict {
		t.Errorf("verifying the same claim twice was answered %d", recorder.Code)
	}

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?action=directory.verification.granted", nil, founder))
	if len(entries) != 1 || entries[0].TargetID != id {
		t.Fatalf("the grant was not recorded once against the verification: %v", entries)
	}
	if !strings.Contains(entries[0].Detail, "Reviewed their public repositories") || !strings.Contains(entries[0].Detail, "Go") {
		t.Errorf("the record does not say what was verified and why: %s", entries[0].Detail)
	}
}

/*
One verified claim among three is shown as one verified claim among three.

There is no badge for the profile as a whole to stand for more than was checked.
*/
func TestAVerifiedClaimIsDistinguishableFromTheRest(t *testing.T) {
	handler, _, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	threeSkills(t, handler, juma)

	verify(t, handler, verifier, "juma", claimsOf(t, handler, verifier, "juma")["skill:Go"], "Checked their repositories")

	verified := verifiedSkills(t, handler, client, "juma")
	if !verified["Go"] || verified["PostgreSQL"] || verified["Docker"] || len(verified) != 3 {
		t.Errorf("the client sees %v; want Go verified and the other two self-asserted", verified)
	}
	_, view := viewProfile(t, handler, client, "juma")
	if view["identityVerified"] != nil {
		t.Error("verifying a skill made the person's identity look verified")
	}
	for _, item := range view["portfolio"].([]any) {
		if item.(map[string]any)["verified"] != nil {
			t.Error("verifying a skill made a portfolio item look verified")
		}
	}
}

/*
What the moderator wrote is for the owner, not for everybody.

A note may mention something the owner hides from other viewers — the link that
was checked, say — so a viewer is told who verified and when, and the owner is
told what was checked.
*/
func TestTheVerificationNoteIsShownOnlyToTheOwner(t *testing.T) {
	handler, _, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	threeSkills(t, handler, juma)
	verify(t, handler, verifier, "juma", claimsOf(t, handler, verifier, "juma")["skill:Go"], "Checked github.com/juma-private")

	recorder := call(t, handler, http.MethodGet, "/api/directory/profiles/juma", nil, client)
	if strings.Contains(recorder.Body.String(), "juma-private") {
		t.Error("the moderator's note reached another viewer")
	}
	own := call(t, handler, http.MethodGet, "/api/directory/me", nil, juma)
	if !strings.Contains(own.Body.String(), "juma-private") {
		t.Error("the owner is not told what was checked")
	}
}

/*
Editing a verified claim clears its verification, and editing it back does not
restore it: what is on the profile afterwards is not what anybody checked.

Editing a different claim leaves the verification alone.
*/
func TestEditingAVerifiedClaimClearsIt(t *testing.T) {
	handler, founder, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	threeSkills(t, handler, juma)
	claims := claimsOf(t, handler, verifier, "juma")
	verify(t, handler, verifier, "juma", claims["skill:Go"], "Checked their repositories")
	verify(t, handler, verifier, "juma", claims["portfolio:Clinic"], "Visited the site")

	edit := func(goDescription, clinicURL, dockerDescription string) {
		saveProfile(t, handler, juma, map[string]any{
			"visibility": "members",
			"skills": []map[string]any{
				{"name": "Go", "description": goDescription},
				{"name": "PostgreSQL", "description": "six years"},
				{"name": "Docker", "description": dockerDescription},
			},
			"portfolio": []map[string]any{{"id": claims["portfolio:Clinic"]["id"], "title": "Clinic", "url": clinicURL}},
		})
	}

	// A different claim edited: both verifications stand.
	edit("eight years", "https://example.com/clinic", "since 2015")
	if verified := verifiedSkills(t, handler, client, "juma"); !verified["Go"] {
		t.Fatal("editing another skill cleared Go's verification")
	}

	// The verified claims edited: both are cleared.
	edit("twelve years", "https://example.com/other", "since 2015")
	if verified := verifiedSkills(t, handler, client, "juma"); verified["Go"] {
		t.Error("editing a verified skill left it shown as verified")
	}
	_, view := viewProfile(t, handler, client, "juma")
	if view["portfolio"].([]any)[0].(map[string]any)["verified"] != nil {
		t.Error("editing a verified portfolio item left it shown as verified")
	}

	// Edited back to what was checked: still cleared.
	edit("eight years", "https://example.com/clinic", "since 2015")
	if verified := verifiedSkills(t, handler, client, "juma"); verified["Go"] {
		t.Error("editing a claim back restored a verification that had been cleared")
	}

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?action=directory.verification.cleared", nil, founder))
	if len(entries) != 2 {
		t.Errorf("clearing was recorded %d times, want twice", len(entries))
	}
}

/*
A revoked verification is gone at once, and the revocation is recorded with its
reason. A revocation needs a reason, and there is nothing to revoke twice.
*/
func TestARevokedVerificationIsGone(t *testing.T) {
	handler, founder, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	client := registerAndSignIn(t, handler, "client")
	threeSkills(t, handler, juma)
	id := verify(t, handler, verifier, "juma", claimsOf(t, handler, verifier, "juma")["skill:Go"], "Checked their repositories")

	path := "/api/directory/moderation/verifications/" + id + "/revoke"
	if recorder := call(t, handler, http.MethodPost, path, map[string]string{"reason": ""}, verifier); recorder.Code != http.StatusBadRequest {
		t.Errorf("a revocation without a reason was answered %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, path, map[string]string{"reason": "the repositories were not theirs"}, client); recorder.Code != http.StatusForbidden {
		t.Errorf("somebody without directory.verify revoked: %d", recorder.Code)
	}
	if recorder := call(t, handler, http.MethodPost, path, map[string]string{"reason": "the repositories were not theirs"}, verifier); recorder.Code != http.StatusNoContent {
		t.Fatalf("revoking returned %d", recorder.Code)
	}

	if verified := verifiedSkills(t, handler, client, "juma"); verified["Go"] {
		t.Error("a revoked verification is still shown")
	}
	if recorder := call(t, handler, http.MethodPost, path, map[string]string{"reason": "again"}, verifier); recorder.Code != http.StatusNotFound {
		t.Errorf("revoking twice was answered %d", recorder.Code)
	}

	entries := decodeAudit(t, call(t, handler, http.MethodGet,
		"/api/admin/audit?action=directory.verification.revoked", nil, founder))
	if len(entries) != 1 || entries[0].TargetID != id || !strings.Contains(entries[0].Detail, "the repositories were not theirs") {
		t.Errorf("the revocation was not recorded with its reason: %v", entries)
	}
}

// A hidden profile's claims are not a verifier's to read.
func TestAHiddenProfileIsNotVerifiable(t *testing.T) {
	handler, _, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	saveProfile(t, handler, juma, map[string]any{"visibility": "hidden", "skills": []map[string]any{{"name": "Go"}}})

	if recorder := call(t, handler, http.MethodGet, "/api/directory/moderation/profiles/juma/claims", nil, verifier); recorder.Code != http.StatusNotFound {
		t.Errorf("a verifier read a hidden profile's claims: %d", recorder.Code)
	}
}

// Nobody verifies their own claims, whatever permissions they hold.
func TestNobodyVerifiesTheirOwnClaims(t *testing.T) {
	handler, _, verifier := withVerifier(t)
	threeSkills(t, handler, verifier)
	claim := claimsOf(t, handler, verifier, "kheri")["skill:Go"]

	recorder := call(t, handler, http.MethodPost, "/api/directory/moderation/profiles/kheri/verifications",
		map[string]any{"kind": "skill", "claimId": claim["id"], "note": "I know myself"}, verifier)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("a verifier verified their own claim: %d", recorder.Code)
	}
}

/*
A verified association with a company ends when the association does, and
joining again does not bring the attestation back.
*/
func TestAVerifiedCompanyAssociationEndsWithIt(t *testing.T) {
	handler, _, verifier := withVerifier(t)
	juma := registerAndSignIn(t, handler, "juma")
	owner := registerAndSignIn(t, handler, "owner")
	client := registerAndSignIn(t, handler, "client")
	saveProfile(t, handler, juma, map[string]any{"visibility": "members"})

	join := func() {
		call(t, handler, http.MethodPost, "/api/directory/companies/kilima/members",
			map[string]string{"username": "juma", "role": "Engineer"}, owner)
		if recorder := call(t, handler, http.MethodPost, "/api/directory/me/companies/kilima/accept", nil, juma); recorder.Code != http.StatusNoContent {
			t.Fatalf("accepting returned %d", recorder.Code)
		}
	}
	companyVerified := func() bool {
		_, view := viewProfile(t, handler, client, "juma")
		companies, _ := view["companies"].([]any)
		return len(companies) == 1 && companies[0].(map[string]any)["verified"] != nil
	}

	call(t, handler, http.MethodPost, "/api/directory/companies", map[string]any{"slug": "kilima", "name": "Kilima"}, owner)
	join()
	verify(t, handler, verifier, "juma", claimsOf(t, handler, verifier, "juma")["company:Kilima"], "Saw the contract")
	if !companyVerified() {
		t.Fatal("the verified association is not shown as verified")
	}

	call(t, handler, http.MethodDelete, "/api/directory/me/companies/kilima", nil, juma)
	join()
	if companyVerified() {
		t.Error("leaving and joining again brought back a verification of the association that ended")
	}
}
