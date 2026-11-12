package httpapi_test

import (
	"net/http"
	"testing"
)

/*
The list of every account is user.read.all's, and still somewhere somebody who
suspends accounts can find the one to suspend.

Paged, so it is every account rather than the first hundred, and carrying no
email address whoever reads it.
*/
func TestListingEveryAccount(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)

	reader := registerAndSignIn(t, handler, "amina")
	suspender := registerAndSignIn(t, handler, "baraka")
	ordinary := registerAndSignIn(t, handler, "chausiku")
	grantRole(t, handler, admin, reader, "user.read.all")
	grantRole(t, handler, admin, suspender, "user.suspend")

	if code := call(t, handler, http.MethodGet, "/api/admin/users", nil, ordinary).Code; code != http.StatusForbidden {
		t.Errorf("an ordinary member listed every account: status %d, want 403", code)
	}
	mustCall(t, handler, http.MethodGet, "/api/admin/users", nil, suspender, http.StatusOK)

	everybody := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/admin/users", nil, reader, http.StatusOK))
	if len(everybody) != 4 {
		t.Errorf("listed %d accounts, want all 4", len(everybody))
	}
	for _, entry := range everybody {
		if person, _ := entry.(map[string]any); person["email"] != nil {
			t.Errorf("the list carries an email address: %v", person)
		}
	}

	page := decodeList(t, mustCall(t, handler, http.MethodGet, "/api/admin/users?limit=2&offset=2", nil, reader, http.StatusOK))
	if len(page) != 2 {
		t.Errorf("the second page of two holds %d accounts", len(page))
	}
}
