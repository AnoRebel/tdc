package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// serverLegal reads the policy links a client would show, and whether the
// field was sent at all.
func serverLegal(t *testing.T, handler http.Handler) (map[string]any, bool) {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading server info returned %d", recorder.Code)
	}

	var info map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	legal, present := info["legal"].(map[string]any)
	return legal, present
}

/*
TestAnOperatorsPoliciesReachEveryClient is the point of the settings.

An operator says where their privacy policy and terms are published, and the
unauthenticated server description carries both — read with no session, because
the person who most needs them is the one deciding whether to make an account.
*/
func TestAnOperatorsPoliciesReachEveryClient(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "legal.privacy_policy_url", "https://tdc.or.tz/privacy")
	setSetting(t, handler, owner, "legal.terms_url", "https://tdc.or.tz/terms")

	legal, present := serverLegal(t, handler)
	if !present {
		t.Fatal("the server description says nothing about the policies an operator set")
	}
	if legal["privacyPolicyUrl"] != "https://tdc.or.tz/privacy" {
		t.Errorf("the privacy policy is %v", legal["privacyPolicyUrl"])
	}
	if legal["termsUrl"] != "https://tdc.or.tz/terms" {
		t.Errorf("the terms are %v", legal["termsUrl"])
	}
}

/*
TestAServerWithNoPoliciesLinksToNone keeps the templates templates.

Absent rather than an empty object or a link to the repository's templates: a
template is not something this operator has promised, and a client that could
not tell "none published" from "here they are" might show one as if it were.
*/
func TestAServerWithNoPoliciesLinksToNone(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	if legal, present := serverLegal(t, handler); present {
		t.Errorf("a server with no policies set describes some: %v", legal)
	}

	// One without the other sends only the one.
	setSetting(t, handler, owner, "legal.terms_url", "https://tdc.or.tz/terms")
	legal, present := serverLegal(t, handler)
	if !present || legal["termsUrl"] != "https://tdc.or.tz/terms" {
		t.Fatalf("the terms alone did not arrive: %v", legal)
	}
	if _, sent := legal["privacyPolicyUrl"]; sent {
		t.Errorf("an unset privacy policy was sent as %v", legal["privacyPolicyUrl"])
	}
}

/*
TestAPolicyMustBeAFetchableLink is what stops a setting becoming a script.

Every client renders these as links somebody clicks, so a javascript: value
would run in their session. The payloads carrying a host matter for the reason
the logo's test gives: without one, a check requiring a host passes alone.
*/
func TestAPolicyMustBeAFetchableLink(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	for _, key := range []string{"legal.privacy_policy_url", "legal.terms_url"} {
		for _, refused := range []string{
			"javascript:alert(1)",
			"javascript://tdc.or.tz/%0aalert(1)",
			"data:text/html,<script>alert(1)</script>",
			"ftp://tdc.or.tz/terms",
			"see the noticeboard",
		} {
			recorder := call(t, handler, http.MethodPut, "/api/admin/settings/"+key,
				map[string]any{"value": refused}, owner)
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("%s accepted %q (%d)", key, refused, recorder.Code)
			}
		}
	}

	if legal, present := serverLegal(t, handler); present {
		t.Errorf("a refused value reached the description: %v", legal)
	}
}

// TestOnlyAnAdministratorSetsThePolicies keeps a member from repointing them.
func TestOnlyAnAdministratorSetsThePolicies(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)
	member := registerAndSignIn(t, handler, "member")

	recorder := call(t, handler, http.MethodPut, "/api/admin/settings/legal.terms_url",
		map[string]any{"value": "https://elsewhere.test/terms"}, member)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("a member setting the terms got %d", recorder.Code)
	}
	if legal, present := serverLegal(t, handler); present {
		t.Errorf("a member's value reached the description: %v", legal)
	}
}
