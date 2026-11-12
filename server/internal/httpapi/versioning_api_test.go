package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The same API answers under /api/v1 and /api, and says which version it is;
// a version this server does not serve is refused by name.
func TestTheAPIIsServedUnderItsVersion(t *testing.T) {
	handler := newTestAPI(t)

	for _, path := range []string{"/api/version", "/api/v1/version"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"apiVersion":"1"`) {
			t.Errorf("%s answered %d: %s", path, recorder.Code, recorder.Body)
		}
		if recorder.Header().Get("TDC-API-Version") != "1" {
			t.Errorf("%s did not say which API version answered", path)
		}
	}

	token := completeSetup(t, handler)
	mustCall(t, handler, http.MethodGet, "/api/v1/me", nil, token, http.StatusOK)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/version", nil))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "API version 1") {
		t.Errorf("an unknown version answered %d: %s", recorder.Code, recorder.Body)
	}
}
