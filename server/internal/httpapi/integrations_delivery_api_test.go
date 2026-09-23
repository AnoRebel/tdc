package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
A test delivery is refused to a private address by default, and arrives once
the operator allows private addresses.

The same endpoint both times, so the setting is the only thing that differs —
and the refusal must happen without the receiver hearing anything at all.
*/
func TestATestDeliveryRespectsTheDestinationGuard(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)

	reached := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		reached++
	}))
	defer receiver.Close()

	refused := call(t, handler, http.MethodPost, "/api/integrations/test-delivery",
		map[string]any{"url": receiver.URL}, owner)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("a delivery to loopback returned %d, want 403: %s", refused.Code, refused.Body.String())
	}
	if reached != 0 {
		t.Fatal("the receiver was reached despite the refusal")
	}

	setSetting(t, handler, owner, "integrations.allow_private_addresses", "true")

	sent := call(t, handler, http.MethodPost, "/api/integrations/test-delivery",
		map[string]any{"url": receiver.URL, "secret": "shared"}, owner)
	if sent.Code != http.StatusOK || decode(t, sent)["delivered"] != true {
		t.Fatalf("with private addresses allowed the delivery returned %d: %s", sent.Code, sent.Body.String())
	}
	if reached != 1 {
		t.Errorf("the receiver was reached %d times", reached)
	}
}

// And nobody can make the server send requests on demand without signing in.
func TestATestDeliveryNeedsAnAccount(t *testing.T) {
	handler := newTestAPI(t)
	completeSetup(t, handler)

	recorder := call(t, handler, http.MethodPost, "/api/integrations/test-delivery",
		map[string]any{"url": "https://example.org/"}, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous test delivery returned %d", recorder.Code)
	}
}
