package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// setSetting changes a server setting as an administrator.
func setSetting(t *testing.T, handler http.Handler, token, key, value string) {
	t.Helper()

	recorder := call(t, handler, http.MethodPut, "/api/admin/settings/"+key,
		map[string]any{"value": value}, token)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("setting %s to %q returned %d: %s",
			key, value, recorder.Code, truncate(recorder.Body.String()))
	}
}

// tryRegister attempts to create an account and returns the status.
func tryRegister(t *testing.T, handler http.Handler, body map[string]any) int {
	t.Helper()
	return call(t, handler, http.MethodPost, "/api/auth/register", body, "").Code
}

/*
TestRegistrationPolicyOpenByDefault is the default an operator gets without
choosing one.

A server that refused registration until somebody configured it would look
broken on first run, and the whole point of the default is that a fresh server
works.
*/
func TestRegistrationPolicyOpenByDefault(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	code := tryRegister(t, handler, map[string]any{
		"username": "amina", "password": testPassword,
	})
	if code != http.StatusCreated {
		t.Errorf("registering under the default policy returned %d, expected 201", code)
	}
}

// TestClosedRegistrationRefusesEverybody covers the setting an operator
// reaches for during a flood of spam accounts.
func TestClosedRegistrationRefusesEverybody(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "registration.policy", "closed")

	code := tryRegister(t, handler, map[string]any{
		"username": "amina", "password": testPassword,
	})
	if code != http.StatusForbidden {
		t.Errorf("registering under a closed policy returned %d, expected 403", code)
	}
}

/*
TestExistingAccountsSurviveAPolicyChange is what makes a policy safe to change.

Closing registration governs creating an account. Somebody who registered while
it was open keeps theirs and can still sign in — otherwise an operator closing
registration during an incident would lock out everybody who already belongs.
*/
func TestExistingAccountsSurviveAPolicyChange(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// Registered while it was open.
	existing := registerAndSignIn(t, handler, "amina")

	setSetting(t, handler, owner, "registration.policy", "closed")

	// Still signed in.
	recorder := call(t, handler, http.MethodGet, "/api/me", nil, existing)
	if recorder.Code != http.StatusOK {
		t.Errorf("an existing session stopped working after the policy changed: %d",
			recorder.Code)
	}

	// And can still sign in afresh.
	again := call(t, handler, http.MethodPost, "/api/auth/signin", map[string]any{
		"username": "amina", "password": testPassword,
	}, "")
	if again.Code != http.StatusOK {
		t.Errorf("an existing account could not sign in after registration closed: %d",
			again.Code)
	}
}

// TestDomainPolicyAdmitsOnlyItsDomains covers the policy a workplace server
// uses.
func TestDomainPolicyAdmitsOnlyItsDomains(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "registration.policy", "domain")
	setSetting(t, handler, owner, "registration.domains", "tdc.or.tz, example.com")

	// A slice with an explicit username each: deriving one from the case's own
	// text collided, and a name already taken is refused for a reason that has
	// nothing to do with the policy under test.
	tests := []struct {
		name     string
		username string
		email    string
		allow    bool
	}{
		{"a permitted domain", "amina", "amina@tdc.or.tz", true},
		{"another permitted domain", "juma", "juma@example.com", true},
		{"different case", "neema", "neema@TDC.OR.TZ", true},
		{"a domain not on the list", "stranger", "stranger@elsewhere.net", false},
		{"a subdomain of a permitted one", "baraka", "baraka@mail.tdc.or.tz", false},
		{"no address at all", "zawadi", "", false},
		{"not an address", "kito", "just-a-name", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code := tryRegister(t, handler, map[string]any{
				"username": test.username,
				"email":    test.email,
				"password": testPassword,
			})

			if test.allow && code != http.StatusCreated {
				t.Errorf("%s was refused with %d", test.email, code)
			}
			if !test.allow && code == http.StatusCreated {
				t.Errorf("%s was admitted under a domain policy that does not list it",
					test.email)
			}
		})
	}
}

/*
TestDomainPolicyWithNoDomainsAdmitsNobody covers a half-finished configuration.

An operator who chose the domain policy and has not yet listed a domain has
closed registration, and it should behave that way rather than admitting
everybody — the failure that would silently undo the decision they just made.
*/
func TestDomainPolicyWithNoDomainsAdmitsNobody(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	setSetting(t, handler, owner, "registration.policy", "domain")

	code := tryRegister(t, handler, map[string]any{
		"username": "amina", "email": "amina@anywhere.test", "password": testPassword,
	})
	if code == http.StatusCreated {
		t.Error("a domain policy with no domains admitted somebody")
	}
}

// TestUnknownSettingsAreRefused keeps a typo from looking like a change.
func TestUnknownSettingsAreRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPut,
		"/api/admin/settings/registration.polcy", // deliberately misspelt
		map[string]any{"value": "closed"}, owner)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("a misspelt setting returned %d, expected 404 rather than "+
			"silently creating a setting nothing reads", recorder.Code)
	}
}

// TestInvalidSettingValuesAreRefused keeps a setting meaning what it says.
func TestInvalidSettingValuesAreRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// A slice rather than a map: two cases share a key, and a map would
	// silently keep only one of them.
	for _, invalid := range []struct{ key, value string }{
		{"registration.policy", "whenever"},
		{"limits.max_users", "lots"},
		{"limits.max_users", "-5"},
		{"registration.domains", "@example.com"},
	} {
		key, value := invalid.key, invalid.value
		recorder := call(t, handler, http.MethodPut, "/api/admin/settings/"+key,
			map[string]any{"value": value}, owner)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("setting %s to %q returned %d, expected 400",
				key, value, recorder.Code)
		}
	}
}

/*
TestUserLimitRefusesCleanly is what 7.2 asks for.

A limit that refuses after the account exists but before its first channel
leaves somebody half-registered, which is worse than either outcome. The check
runs before anything is created.
*/
func TestUserLimitRefusesCleanly(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	// One account exists — the administrator. A limit of one is therefore
	// already reached.
	setSetting(t, handler, owner, "limits.max_users", "1")

	code := tryRegister(t, handler, map[string]any{
		"username": "amina", "password": testPassword,
	})
	if code != http.StatusForbidden {
		t.Errorf("registering past the account limit returned %d, expected 403", code)
	}

	// And nothing was created on the way to refusing.
	usage := decode(t, call(t, handler, http.MethodGet, "/api/admin/usage", nil, owner))
	users, _ := usage["users"].(map[string]any)
	if used, _ := users["used"].(float64); used != 1 {
		t.Errorf("the refused registration left %v accounts, expected 1", used)
	}
}

// TestChannelLimitRefusesCleanly covers the same rule for channels.
func TestChannelLimitRefusesCleanly(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	makeRoom(t, handler, owner, "general", "public")
	setSetting(t, handler, owner, "limits.max_channels", "1")

	recorder := call(t, handler, http.MethodPost, "/api/rooms", map[string]any{
		"slug": "another", "name": "Another", "visibility": "public",
	}, owner)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("creating past the channel limit returned %d, expected 403", recorder.Code)
	}
}

/*
TestNoLimitIsTheDefault keeps a server working without configuration.

Zero means no limit, and a server enforcing a number nobody chose would stop
working for reasons its operator cannot see.
*/
func TestNoLimitIsTheDefault(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/admin/usage", nil, owner)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading usage returned %d", recorder.Code)
	}

	var usage struct {
		Users    struct{ Used, Limit int64 }
		Channels struct{ Used, Limit int64 }
		Storage  struct{ Used, Limit int64 }
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &usage); err != nil {
		t.Fatalf("decoding usage: %v", err)
	}

	for name, limit := range map[string]int64{
		"users":    usage.Users.Limit,
		"channels": usage.Channels.Limit,
		"storage":  usage.Storage.Limit,
	} {
		if limit != 0 {
			t.Errorf("the default %s limit is %d; it should be 0, meaning none", name, limit)
		}
	}
}

// TestSettingsNeedPermission keeps server policy out of ordinary hands.
func TestSettingsNeedPermission(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	ordinary := registerAndSignIn(t, handler, "amina")

	for _, probe := range []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodGet, "/api/admin/settings", nil},
		{http.MethodGet, "/api/admin/usage", nil},
		{http.MethodPut, "/api/admin/settings/registration.policy",
			map[string]any{"value": "closed"}},
	} {
		recorder := call(t, handler, probe.method, probe.path, probe.body, ordinary)
		if recorder.Code == http.StatusOK || recorder.Code == http.StatusNoContent {
			t.Errorf("%s %s succeeded without the configure permission",
				probe.method, probe.path)
		}
	}
}
