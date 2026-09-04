package unfurl_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestDisabledMeansNoOutboundRequest is the requirement, and the reason it is
// worded as "no fetch" rather than "no preview shown".
//
// Someone who turns previews off is asking this server not to tell a third
// party that they were sent a link. Fetching it and then hiding the result
// would leak precisely what they were avoiding — so the test counts requests at
// the far end rather than checking what came back.
func TestDisabledMeansNoOutboundRequest(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Reached</title></head></html>`)
	}))
	defer server.Close()

	memory := cache.NewMemory()
	defer memory.Close()

	for _, tc := range []struct {
		name     string
		operator bool
		person   bool
	}{
		{"operator has disabled previews", false, true},
		{"the person has disabled previews", true, false},
		{"both are disabled", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests.Store(0)

			service := unfurl.NewService(memory, unfurl.Settings{Enabled: tc.operator}, quietLogger())
			_, err := service.Preview(context.Background(), server.URL,
				unfurl.UserPreference{Enabled: tc.person})

			if !errors.Is(err, unfurl.ErrNotPreviewable) {
				t.Errorf("gave %v, expected ErrNotPreviewable", err)
			}
			if count := requests.Load(); count != 0 {
				t.Errorf("%d outbound requests were made; the link must not be fetched at all", count)
			}
		})
	}
}

// TestResultsAreCached checks that one link does not cost one request per
// reader.
//
// The service refuses loopback, which is correct and tested elsewhere, so this
// asserts the weaker but still meaningful property: whatever the first call
// decided, the calls after it cost no further outbound request.
func TestResultsAreCached(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><meta property="og:title" content="Mkutano"></head></html>`)
	}))
	defer server.Close()

	memory := cache.NewMemory()
	defer memory.Close()

	service := unfurl.NewService(memory, unfurl.Settings{Enabled: true}, quietLogger())
	allowed := unfurl.UserPreference{Enabled: true}

	_, _ = service.Preview(context.Background(), server.URL, allowed)
	afterFirst := requests.Load()

	for range 5 {
		_, _ = service.Preview(context.Background(), server.URL, allowed)
	}

	if after := requests.Load(); after != afterFirst {
		t.Errorf("%d requests after one call, %d after six; the result was not cached",
			afterFirst, after)
	}
}

// TestFailuresAreCachedToo checks that a dead link is not retried on every
// render.
//
// Without this, a message linking to a site that is down would send an outbound
// request every time anybody scrolled past it — slow for the reader and, in
// aggregate, rude to whatever is on the other end.
func TestFailuresAreCachedToo(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "gone", http.StatusGone)
	}))
	defer server.Close()

	memory := cache.NewMemory()
	defer memory.Close()

	service := unfurl.NewService(memory, unfurl.Settings{Enabled: true}, quietLogger())
	allowed := unfurl.UserPreference{Enabled: true}

	_, _ = service.Preview(context.Background(), server.URL, allowed)
	afterFirst := requests.Load()

	for range 3 {
		_, err := service.Preview(context.Background(), server.URL, allowed)
		if !errors.Is(err, unfurl.ErrNotPreviewable) {
			t.Errorf("a failed link gave %v, expected ErrNotPreviewable", err)
		}
	}

	if after := requests.Load(); after != afterFirst {
		t.Errorf("a failing link was retried: %d requests then %d", afterFirst, after)
	}
}

// TestOperatorSwitchIsReported so a client can hide the setting entirely rather
// than offering a choice that does nothing.
func TestOperatorSwitchIsReported(t *testing.T) {
	memory := cache.NewMemory()
	defer memory.Close()

	if unfurl.NewService(memory, unfurl.Settings{Enabled: false}, quietLogger()).Enabled() {
		t.Error("a disabled service reports itself as enabled")
	}
	if !unfurl.NewService(memory, unfurl.Settings{Enabled: true}, quietLogger()).Enabled() {
		t.Error("an enabled service reports itself as disabled")
	}
}
