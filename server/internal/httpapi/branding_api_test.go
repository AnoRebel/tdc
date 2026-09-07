package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// serverBranding reads what a client would apply.
func serverBranding(t *testing.T, handler http.Handler) (map[string]any, map[string]any) {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading server info returned %d", recorder.Code)
	}

	var info map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	branding, _ := info["branding"].(map[string]any)
	return info, branding
}

/*
TestBrandingAppliesWithoutARebuild is the scenario the requirement names.

An operator sets a name, a logo and a colour, and clients show them. Nothing is
compiled, nothing is redeployed — the whole point is that a community can look
like itself without maintaining a fork.
*/
func TestBrandingAppliesWithoutARebuild(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "brand.name", "Jumuiya ya Watengenezaji")
	setSetting(t, handler, owner, "brand.initials", "JW")
	setSetting(t, handler, owner, "brand.logo_url", "https://tdc.or.tz/logo.png")
	setSetting(t, handler, owner, "brand.accent", "#2563eb")

	_, branding := serverBranding(t, handler)

	if branding["name"] != "Jumuiya ya Watengenezaji" {
		t.Errorf("the name is %v", branding["name"])
	}
	if branding["initials"] != "JW" {
		t.Errorf("the initials are %v", branding["initials"])
	}
	if branding["logoUrl"] != "https://tdc.or.tz/logo.png" {
		t.Errorf("the logo is %v", branding["logoUrl"])
	}
	if branding["accent"] != "#2563eb" {
		t.Errorf("the accent is %v", branding["accent"])
	}
}

/*
TestBrandingCannotHideTheServerAddress is the security property.

An operator may decide what their server is called and never what it is.
Branding that could remove or rewrite the address would let a server present
itself as another one, which is the whole reason the address is not part of
what branding governs.
*/
func TestBrandingCannotHideTheServerAddress(t *testing.T) {
	handler := newTestAPIConfiguredForBranding(t)
	owner := completeSetup(t, handler)

	// Everything an operator can set, including values that try to be the
	// address field rather than a name.
	setSetting(t, handler, owner, "brand.name", "A Different Server Entirely")
	setSetting(t, handler, owner, "brand.initials", "XY")

	info, branding := serverBranding(t, handler)

	address, _ := info["canonicalAddress"].(string)
	if address != brandingTestAddress {
		t.Errorf("the canonical address is %q after branding, expected %q",
			address, brandingTestAddress)
	}

	// And the name an operator chose did not become the address.
	if address == branding["name"] {
		t.Error("branding replaced the address with the chosen name")
	}
}

/*
TestAnUnbrandedServerStillCallsItselfSomething keeps the default sensible.

An operator who sets nothing should see the name they configured, and initials
derived from it, rather than an empty badge.
*/
func TestAnUnbrandedServerStillCallsItselfSomething(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	_, branding := serverBranding(t, handler)

	if branding["name"] == "" || branding["name"] == nil {
		t.Error("an unbranded server has no name at all")
	}
	if branding["initials"] == "" || branding["initials"] == nil {
		t.Error("an unbranded server has no initials, so its badge is empty")
	}
}

/*
TestALogoMustBeAFetchableLink is what stops a setting becoming a script.

A javascript: link would run in every client that loaded the interface, and a
data: one would let an operator's setting carry arbitrary bytes into it.
*/
func TestALogoMustBeAFetchableLink(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	/*
		The last two carry a host, which matters.

		Every obvious payload — javascript:, data:, file: — happens to parse
		with an empty host, so a test using only those passes with the scheme
		check removed entirely and proves only that a host is required.
		"javascript://tdc.or.tz/x" has both a scheme and a host, so it isolates
		the rule this test is named for.
	*/
	for _, refused := range []string{
		"javascript:alert(1)",
		"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=",
		"file:///etc/passwd",
		"not a link at all",
		"javascript://tdc.or.tz/%0aalert(1)",
		"ftp://tdc.or.tz/logo.png",
	} {
		recorder := call(t, handler, http.MethodPut, "/api/admin/settings/brand.logo_url",
			map[string]any{"value": refused}, owner)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%q was accepted as a logo (%d)", refused, recorder.Code)
		}
	}

	// An ordinary link is fine.
	accepted := call(t, handler, http.MethodPut, "/api/admin/settings/brand.logo_url",
		map[string]any{"value": "https://tdc.or.tz/logo.png"}, owner)
	if accepted.Code != http.StatusNoContent {
		t.Errorf("an ordinary link was refused: %d", accepted.Code)
	}
}

/*
TestAnAccentMustBeAColour is the same rule for the value that reaches a
stylesheet.

The accent becomes a custom property, so a value carrying a semicolon or a
brace would escape the property it is set on and become declarations of its
own.
*/
func TestAnAccentMustBeAColour(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	for _, refused := range []string{
		"red; position: fixed; top: 0",
		"blue} body { display: none",
		"url(https://elsewhere.test/x)",
		"</style><script>alert(1)</script>",
	} {
		recorder := call(t, handler, http.MethodPut, "/api/admin/settings/brand.accent",
			map[string]any{"value": refused}, owner)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%q was accepted as a colour (%d)", refused, recorder.Code)
		}
	}

	for _, accepted := range []string{
		"#2563eb",
		"#fff",
		"rebeccapurple",
		"oklch(0.6 0.2 260)",
		"rgb(37 99 235)",
	} {
		recorder := call(t, handler, http.MethodPut, "/api/admin/settings/brand.accent",
			map[string]any{"value": accepted}, owner)

		if recorder.Code != http.StatusNoContent {
			t.Errorf("%q was refused as a colour (%d)", accepted, recorder.Code)
		}
	}
}

// TestInitialsAreLimited keeps a badge from holding a sentence.
func TestInitialsAreLimited(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/admin/settings/brand.initials",
		map[string]any{"value": "far too many letters"}, owner)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("a long value was accepted as initials: %d", recorder.Code)
	}
}

/*
brandingTestAddress is the address the branding tests check survives.

A real value rather than the harness's empty default: a test comparing an empty
address to an empty address passes whatever branding does to it, which is the
opposite of what it claims to check.
*/
const brandingTestAddress = "tdc.or.tz"

// newTestAPIConfiguredForBranding builds a server that knows its own address,
// so branding can be shown not to change it.
func newTestAPIConfiguredForBranding(t *testing.T) http.Handler {
	t.Helper()
	return newTestAPIWithAddress(t, brandingTestAddress)
}
