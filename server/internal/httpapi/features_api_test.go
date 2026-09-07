package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// whenSoon is a send time far enough ahead that the scheduler will not have
// fired it while the test is still running.
func whenSoon() string {
	return time.Now().Add(time.Hour).Format(time.RFC3339)
}

// setFeature turns a feature on or off as an administrator.
func setFeature(t *testing.T, handler http.Handler, token, feature string, on bool) {
	t.Helper()

	recorder := call(t, handler, http.MethodPut, "/api/admin/features/"+feature,
		map[string]any{"enabled": on}, token)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("setting %s to %v returned %d: %s",
			feature, on, recorder.Code, truncate(recorder.Body.String()))
	}
}

// advertised reads the capabilities this server tells an unauthenticated
// caller it has.
func advertised(t *testing.T, handler http.Handler) []string {
	t.Helper()

	recorder := call(t, handler, http.MethodGet, "/api/server", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading server info returned %d", recorder.Code)
	}

	var info struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding server info: %v", err)
	}
	return info.Capabilities
}

func lists(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}

/*
TestADisabledFeatureIsRefusedAndUnadvertised is the rule 7.5 turns on.

Both halves matter, and the second is the one that is easy to get wrong. A
client hides a control it is told is absent — but hiding a control is not
enforcement, and anybody can still make the request. A server that advertised a
feature as off and served it anyway would have turned a disabled capability
into an undocumented one.
*/
func TestADisabledFeatureIsRefusedAndUnadvertised(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	// On by default, so there is something to turn off.
	if !lists(advertised(t, handler), "scheduled-messages") {
		t.Fatal("scheduled messages are not advertised by default; " +
			"this test cannot show that disabling removes them")
	}

	before := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
		map[string]any{"body": "Baadaye", "sendAt": whenSoon()}, owner)
	if before.Code != http.StatusCreated && before.Code != http.StatusOK {
		t.Fatalf("scheduling while enabled returned %d: %s",
			before.Code, truncate(before.Body.String()))
	}

	setFeature(t, handler, owner, "scheduled-messages", false)

	// Refused.
	after := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
		map[string]any{"body": "Baadaye tena", "sendAt": whenSoon()}, owner)
	if after.Code == http.StatusCreated || after.Code == http.StatusOK {
		t.Error("a disabled feature still accepted a request; hiding the " +
			"control in a client is not enforcement")
	}

	// And no longer advertised.
	if lists(advertised(t, handler), "scheduled-messages") {
		t.Error("a disabled feature is still advertised, so a client would " +
			"show a control that cannot work")
	}
}

/*
TestTheAdvertisedSetAndTheEnforcedSetAgree is the property that makes one
toggle worth having rather than two booleans.

Checked across every feature and in both directions, because a drift in either
is a bug: a capability advertised but refused shows a broken control, and one
refused but advertised is the dangerous direction — a feature an operator
believes is off.
*/
func TestTheAdvertisedSetAndTheEnforcedSetAgree(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	// Each feature paired with a request that exercises it.
	probes := []struct {
		feature string
		attempt func() int
	}{
		{"scheduled-messages", func() int {
			return call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
				map[string]any{"body": "Baadaye", "sendAt": whenSoon()}, owner).Code
		}},
		{"invitations", func() int {
			return call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/invitations",
				map[string]any{"kind": "link"}, owner).Code
		}},
	}

	for _, probe := range probes {
		for _, on := range []bool{true, false} {
			setFeature(t, handler, owner, probe.feature, on)

			isAdvertised := lists(advertised(t, handler), probe.feature)
			code := probe.attempt()
			accepted := code < 400

			if isAdvertised != accepted {
				t.Errorf("%s with the toggle %v: advertised=%v but the request "+
					"returned %d; the advertised set and what the server "+
					"accepts must agree",
					probe.feature, on, isAdvertised, code)
			}
		}
	}
}

/*
TestADisabledFeatureIsNotFound covers what the refusal says.

404 rather than 403: a disabled feature is not something the caller lacks
permission for, it is something this server does not offer. 403 would invite
somebody to go and ask an administrator for a permission that would not help.
*/
func TestADisabledFeatureIsNotFound(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	setFeature(t, handler, owner, "invitations", false)

	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/invitations",
		map[string]any{"kind": "link"}, owner)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("a disabled feature returned %d, expected 404", recorder.Code)
	}
}

/*
TestATurnedOffFeatureStaysOffForEverybody checks the toggle is the server's
rather than the caller's.

An administrator has every permission, so if anything were going to bypass a
gate it would be them.
*/
func TestATurnedOffFeatureStaysOffForEverybody(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	member := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, member, roomID)

	setFeature(t, handler, owner, "scheduled-messages", false)

	for name, token := range map[string]string{"the administrator": owner, "a member": member} {
		recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/scheduled",
			map[string]any{"body": "Baadaye", "sendAt": whenSoon()}, token)

		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s reached a disabled feature: %d", name, recorder.Code)
		}
	}
}

// TestUnknownFeaturesAreRefused keeps a typo from looking like a change.
func TestUnknownFeaturesAreRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut, "/api/admin/features/schedulded-messages",
		map[string]any{"enabled": false}, owner)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("a misspelt feature returned %d, expected 404 rather than "+
			"silently creating a toggle nothing reads", recorder.Code)
	}
}

// TestFeatureTogglesNeedPermission keeps capabilities out of ordinary hands.
func TestFeatureTogglesNeedPermission(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	ordinary := registerAndSignIn(t, handler, "amina")

	listed := call(t, handler, http.MethodGet, "/api/admin/features", nil, ordinary)
	if listed.Code == http.StatusOK {
		t.Error("an ordinary account could read the feature toggles")
	}

	changed := call(t, handler, http.MethodPut, "/api/admin/features/attachments",
		map[string]any{"enabled": false}, ordinary)
	if changed.Code == http.StatusNoContent {
		t.Error("an ordinary account could turn a feature off for everybody")
	}
}

/*
TestAFeatureNeedingAProviderIsNotAdvertisedWithoutOne covers the case where the
operator's intent cannot be honoured.

GIF search switched on with no provider configured would advertise a control
that always fails. The toggle says what the operator wants; whether it can work
is a separate question, and the advertisement answers both.
*/
func TestAFeatureNeedingAProviderIsNotAdvertisedWithoutOne(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// The test server has no GIF provider configured.
	setFeature(t, handler, owner, "gif-search", true)

	if lists(advertised(t, handler), "gif-search") {
		t.Error("gif search is advertised with no provider configured; a " +
			"client would show a control that always fails")
	}
}
