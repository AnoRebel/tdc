package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
)

/*
Outgoing webhooks, end to end: the real worker pool, against receivers that
answer, fail, stall and go away.
*/

// receivedDelivery is one request a test receiver was sent.
type receivedDelivery struct {
	at      time.Time
	headers http.Header
	body    []byte
}

// receiver is an endpoint whose answer a test decides, recording every request.
type receiver struct {
	*httptest.Server
	mu        sync.Mutex
	got       []receivedDelivery
	inFlight  atomic.Int32
	mostAtOne atomic.Int32
	answer    func(count int) (status int, delay time.Duration)
}

func newReceiver(t *testing.T, answer func(count int) (int, time.Duration)) *receiver {
	t.Helper()
	r := &receiver{answer: answer}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		now := r.inFlight.Add(1)
		defer r.inFlight.Add(-1)
		for {
			most := r.mostAtOne.Load()
			if now <= most || r.mostAtOne.CompareAndSwap(most, now) {
				break
			}
		}

		body, _ := io.ReadAll(request.Body)
		r.mu.Lock()
		r.got = append(r.got, receivedDelivery{at: time.Now(), headers: request.Header.Clone(), body: body})
		count := len(r.got)
		r.mu.Unlock()

		status, delay := r.answer(count)
		time.Sleep(delay)
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) deliveries() []receivedDelivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]receivedDelivery(nil), r.got...)
}

func answering(status int) func(int) (int, time.Duration) {
	return func(int) (int, time.Duration) { return status, 0 }
}

/*
startDeliveries runs the server's own worker pool behind a test handler, for as
long as the test lasts.

The same workers Run starts, not a stand-in, so what these tests see is what a
deployed server does. Stopped before the harness closes the database.
*/
func startDeliveries(t *testing.T, handler http.Handler) {
	t.Helper()
	server := handler.(interface{ Server() *httpapi.Server }).Server()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.RunDeliveries(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// webhookFixture is a server with deliveries running, private addresses
// allowed for the test receivers, and a channel to subscribe to.
func webhookFixture(t *testing.T, settings map[string]string) (http.Handler, string, string) {
	t.Helper()
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	setSetting(t, handler, owner, "integrations.allow_private_addresses", "true")
	for key, value := range settings {
		setSetting(t, handler, owner, key, value)
	}
	roomID := makeRoom(t, handler, owner, "builds", "public")
	startDeliveries(t, handler)
	return handler, owner, roomID
}

func subscribe(t *testing.T, handler http.Handler, token, roomID, url string, types ...string) map[string]any {
	t.Helper()
	if len(types) == 0 {
		types = []string{"message.sent"}
	}
	recorder := call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"roomId": roomID, "label": "ci", "url": url, "eventTypes": types,
	}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("subscribing returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

func webhookHistory(t *testing.T, handler http.Handler, token, id string) map[string]any {
	t.Helper()
	recorder := call(t, handler, http.MethodGet, "/api/me/webhooks/"+id, nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reading the webhook returned %d: %s", recorder.Code, recorder.Body.String())
	}
	return decode(t, recorder)
}

func eventually(t *testing.T, within time.Duration, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("gave up waiting for %s", what)
}

func postMessage(t *testing.T, handler http.Handler, token, roomID, body string) {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages",
		map[string]any{"body": body}, token)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("posting returned %d: %s", recorder.Code, recorder.Body.String())
	}
}

// A subscription to a channel its maker cannot read is refused — as missing,
// so the refusal does not confirm the channel exists.
func TestASubscriptionCannotExceedItsOwnersAccess(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	secretRoom := makeRoom(t, handler, owner, "board", "private")
	outsider := registerAndSignIn(t, handler, "amina")

	recorder := call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"roomId": secretRoom, "label": "spy", "url": "https://example.org/hook",
		"eventTypes": []string{"message.sent"},
	}, outsider)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("subscribing to a channel you cannot read returned %d: %s", recorder.Code, recorder.Body.String())
	}

	// And a type nobody reading a channel sees is refused too.
	recorder = call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
		"roomId": makeRoom(t, handler, owner, "open", "public"), "label": "x",
		"url": "https://example.org/hook", "eventTypes": []string{"user.suspended"},
	}, owner)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown event type returned %d", recorder.Code)
	}
}

// An endpoint on loopback or a private network is refused when it is named.
func TestASubscriptionToAPrivateAddressIsRefused(t *testing.T) {
	handler := newTestAPI(t)
	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "builds", "public")

	for _, url := range []string{"http://127.0.0.1:9/hook", "http://localhost:9/hook", "http://10.1.2.3/hook"} {
		recorder := call(t, handler, http.MethodPost, "/api/me/webhooks", map[string]any{
			"roomId": roomID, "label": "ci", "url": url, "eventTypes": []string{"message.sent"},
		}, owner)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s returned %d, want 403", url, recorder.Code)
		}
	}
}

/*
A subscribed event arrives, signed, and verifies from the published keys and
by the secret its owner was given.
*/
func TestASubscribedEventIsDeliveredSigned(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, nil)
	endpoint := newReceiver(t, answering(http.StatusNoContent))
	created := subscribe(t, handler, owner, roomID, endpoint.URL)

	postMessage(t, handler, owner, roomID, "Build 41 passed")
	eventually(t, 5*time.Second, "the delivery", func() bool { return len(endpoint.deliveries()) == 1 })
	got := endpoint.deliveries()[0]

	var payload struct {
		Version int    `json:"version"`
		ID      string `json:"id"`
		Type    string `json:"type"`
		Event   struct {
			Content struct {
				Body string `json:"body"`
			} `json:"content"`
		} `json:"event"`
	}
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}
	if payload.Version != 1 || payload.Type != "message.sent" || payload.Event.Content.Body != "Build 41 passed" {
		t.Errorf("the payload was %s", got.body)
	}
	if payload.ID != got.headers.Get(delivery.HeaderDelivery) {
		t.Errorf("the payload names delivery %q and the header %q", payload.ID, got.headers.Get(delivery.HeaderDelivery))
	}

	keys := call(t, handler, http.MethodGet, httpapi.DeliveryKeysPath, nil, "")
	var published struct {
		Keys []events.PublishedKey `json:"keys"`
	}
	if err := json.Unmarshal(keys.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	ring, err := events.KeyringFromPublished(published.Keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := delivery.Verify(got.headers, got.body, ring, time.Now()); err != nil {
		t.Errorf("the delivery did not verify from the published keys: %v", err)
	}
	if err := delivery.VerifyHMAC(got.headers, got.body, []byte(created["secret"].(string)), time.Now()); err != nil {
		t.Errorf("the delivery did not verify by its secret: %v", err)
	}

	// The secret was shown once, at creation, and never again.
	if strings.Contains(call(t, handler, http.MethodGet, "/api/me/webhooks", nil, owner).Body.String(),
		created["secret"].(string)) {
		t.Error("listing webhooks shows the secret")
	}
}

/*
A receiver that fails twice and then succeeds is retried with a growing delay,
and the history records all three attempts with their status codes.
*/
func TestAFailingReceiverIsRetriedWithIncreasingDelay(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, map[string]string{
		"integrations.webhook_retry_base_seconds": "1",
	})
	endpoint := newReceiver(t, func(count int) (int, time.Duration) {
		if count <= 2 {
			return http.StatusServiceUnavailable, 0
		}
		return http.StatusOK, 0
	})
	created := subscribe(t, handler, owner, roomID, endpoint.URL)
	postMessage(t, handler, owner, roomID, "flaky")

	eventually(t, 15*time.Second, "the third attempt", func() bool { return len(endpoint.deliveries()) == 3 })

	got := endpoint.deliveries()
	first, second := got[1].at.Sub(got[0].at), got[2].at.Sub(got[1].at)
	if first < 700*time.Millisecond || second <= first {
		t.Errorf("the delays were %s then %s; each retry should wait longer than the last", first, second)
	}
	// The same delivery each time, signed afresh.
	if got[0].headers.Get(delivery.HeaderDelivery) != got[2].headers.Get(delivery.HeaderDelivery) {
		t.Error("a retry carried a different delivery identifier")
	}

	var attempts []struct {
		Outcome string `json:"outcome"`
		Status  int    `json:"status"`
		Attempt int    `json:"attempt"`
	}
	eventually(t, 5*time.Second, "the history", func() bool {
		raw, _ := json.Marshal(webhookHistory(t, handler, owner, created["id"].(string))["attempts"])
		_ = json.Unmarshal(raw, &attempts)
		return len(attempts) == 3 && attempts[0].Outcome == "delivered"
	})
	// Newest first: the success, then the two failures before it.
	want := []struct {
		outcome string
		status  int
	}{{"delivered", 200}, {"failed", 503}, {"failed", 503}}
	for i, attempt := range attempts {
		if attempt.Outcome != want[i].outcome || attempt.Status != want[i].status || attempt.Attempt != 3-i {
			t.Errorf("attempt %d recorded as %+v", 3-i, attempt)
		}
	}
}

/*
A receiver that never succeeds is turned off rather than retried for ever, and
its owner is told through their notifications.
*/
func TestAPersistentlyFailingSubscriptionIsDisabledAndItsOwnerTold(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, map[string]string{
		"integrations.webhook_retry_base_seconds": "1",
		"integrations.webhook_max_attempts":       "3",
	})
	endpoint := newReceiver(t, answering(http.StatusInternalServerError))
	created := subscribe(t, handler, owner, roomID, endpoint.URL)
	id := created["id"].(string)

	postMessage(t, handler, owner, roomID, "nobody is listening")
	eventually(t, 15*time.Second, "the subscription to be turned off", func() bool {
		return webhookHistory(t, handler, owner, id)["state"] == "disabled"
	})
	if tried := len(endpoint.deliveries()); tried != 3 {
		t.Errorf("the receiver was tried %d times, want exactly the limit of 3", tried)
	}

	eventually(t, 5*time.Second, "the owner's notification", func() bool {
		return strings.Contains(call(t, handler, http.MethodGet, "/api/me/notifications", nil, owner).Body.String(),
			"webhook_disabled")
	})

	// Nothing more is queued for it, however much happens.
	postMessage(t, handler, owner, roomID, "still nobody")
	time.Sleep(1500 * time.Millisecond)
	if tried := len(endpoint.deliveries()); tried != 3 {
		t.Errorf("a disabled subscription was tried again: %d attempts", tried)
	}
}

/*
However slow a receiver is and however many events arrive, each subscription
has one delivery in flight and the server as a whole no more than its workers.
*/
func TestASlowReceiverCannotOpenUnboundedConnections(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, map[string]string{"integrations.delivery_workers": "2"})

	/*
		Each subscription's deliveries counted separately, by the subscription
		the payload names, so "one in flight per subscription" is checked on
		its own rather than hidden inside the pool's total.
	*/
	var perSubscription sync.Map
	var mostForOne atomic.Int32
	slow := newReceiver(t, func(int) (int, time.Duration) { return http.StatusOK, 400 * time.Millisecond })
	counting := slow.Config.Handler
	slow.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var payload struct {
			Subscription string `json:"subscription"`
		}
		_ = json.Unmarshal(body, &payload)
		value, _ := perSubscription.LoadOrStore(payload.Subscription, new(atomic.Int32))
		inFlight := value.(*atomic.Int32).Add(1)
		defer value.(*atomic.Int32).Add(-1)
		for {
			most := mostForOne.Load()
			if inFlight <= most || mostForOne.CompareAndSwap(most, inFlight) {
				break
			}
		}
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		counting.ServeHTTP(w, request)
	})
	for range 3 {
		subscribe(t, handler, owner, roomID, slow.URL)
	}

	started := time.Now()
	for i := range 4 {
		postMessage(t, handler, owner, roomID, "burst "+string(rune('a'+i)))
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("posting four messages took %s beside a slow receiver", elapsed)
	}

	eventually(t, 20*time.Second, "all twelve deliveries", func() bool { return len(slow.deliveries()) == 12 })
	if most := slow.mostAtOne.Load(); most > 2 {
		t.Errorf("the receiver saw %d connections at once; the pool is two", most)
	}
	if most := mostForOne.Load(); most != 1 {
		t.Errorf("one subscription had %d deliveries in flight at once; it may have one", most)
	}
}

/*
One subscription never has two deliveries in flight, however many workers are
idle.

A single subscription and more workers than it could use, so the only thing
keeping a second worker off its next delivery is the rule itself — with several
subscriptions, the workers spread across them and would pass without it.
*/
func TestASubscriptionHasOneDeliveryInFlight(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, map[string]string{"integrations.delivery_workers": "4"})

	slow := newReceiver(t, func(int) (int, time.Duration) { return http.StatusOK, 300 * time.Millisecond })
	subscribe(t, handler, owner, roomID, slow.URL)
	for i := range 4 {
		postMessage(t, handler, owner, roomID, "in order "+string(rune('a'+i)))
	}

	eventually(t, 10*time.Second, "all four deliveries", func() bool { return len(slow.deliveries()) == 4 })
	if most := slow.mostAtOne.Load(); most != 1 {
		t.Errorf("one subscription had %d deliveries in flight at once", most)
	}

	// Which also keeps them in the order the events happened.
	for i, got := range slow.deliveries() {
		if !strings.Contains(string(got.body), "in order "+string(rune('a'+i))) {
			t.Errorf("delivery %d was %s", i, got.body)
		}
	}
}

/*
The server starts no more deliveries a second than it is set to, however many
are waiting and however promptly receivers answer.

Unpaced, these twenty-four go to a receiver that answers at once in a few tens
of milliseconds. At twenty a second, with the four workers' worth allowed
straight away, the other twenty take a second.
*/
func TestDeliveriesAreStartedNoFasterThanThePace(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, map[string]string{
		"integrations.delivery_workers":      "4",
		"integrations.deliveries_per_second": "20",
	})
	endpoint := newReceiver(t, answering(http.StatusOK))
	for range 4 {
		subscribe(t, handler, owner, roomID, endpoint.URL)
	}

	started := time.Now()
	for i := range 6 {
		postMessage(t, handler, owner, roomID, "paced "+string(rune('a'+i)))
	}
	eventually(t, 10*time.Second, "all twenty-four deliveries", func() bool { return len(endpoint.deliveries()) == 24 })
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Errorf("twenty-four deliveries took %s at twenty a second; the pace allows no less than a second", elapsed)
	}
}

// A subscription over its rate is held back, not dropped, and says so.
func TestARateLimitIsVisibleAndDropsNothing(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, map[string]string{
		"integrations.webhook_deliveries_per_minute": "2",
	})
	endpoint := newReceiver(t, answering(http.StatusOK))
	created := subscribe(t, handler, owner, roomID, endpoint.URL)
	id := created["id"].(string)

	for _, body := range []string{"one", "two", "three", "four"} {
		postMessage(t, handler, owner, roomID, body)
	}

	// Settled: both held-back deliveries have been claimed, found over the
	// limit, and put back to wait for it.
	waitingOf := func() (int, int) {
		deliveries := webhookHistory(t, handler, owner, id)["deliveries"].([]any)
		waiting := 0
		for _, entry := range deliveries {
			if entry.(map[string]any)["state"] == "pending" {
				waiting++
			}
		}
		return len(deliveries), waiting
	}
	eventually(t, 5*time.Second, "two deliveries held back", func() bool {
		total, waiting := waitingOf()
		return total == 4 && waiting == 2 &&
			webhookHistory(t, handler, owner, id)["rateLimited"].(float64) >= 2
	})
	if got := len(endpoint.deliveries()); got != 2 {
		t.Errorf("%d deliveries got through a limit of two a minute", got)
	}
	if !strings.Contains(call(t, handler, http.MethodGet, "/api/me/webhooks/"+id, nil, owner).Body.String(), "rate_limited") {
		t.Error("the history does not show the attempt that was held back")
	}
}

/*
Losing access to the channel stops delivery, and keeps the subscription and its
history.
*/
func TestLosingAccessStopsDeliveryAndKeepsHistory(t *testing.T) {
	handler, owner, _ := webhookFixture(t, nil)
	roomID := makeRoom(t, handler, owner, "board", "private")
	member := registerAndSignIn(t, handler, "amina")
	issued := issueInvitation(t, handler, owner, roomID, nil)
	if code := redeem(t, handler, member, map[string]any{"token": issued.Token}); code != http.StatusOK {
		t.Fatalf("joining the private channel returned %d", code)
	}

	/*
		The first delivery is held at the receiver, so the second is queued
		while the owner can still read the channel and is waiting behind it
		when they leave. That makes the check at delivery time the only thing
		that can stop it — the check when queueing has already passed.
	*/
	release := make(chan struct{})
	endpoint := newReceiver(t, func(count int) (int, time.Duration) {
		if count == 1 {
			<-release
		}
		return http.StatusOK, 0
	})
	created := subscribe(t, handler, member, roomID, endpoint.URL)
	id := created["id"].(string)

	postMessage(t, handler, owner, roomID, "before")
	eventually(t, 5*time.Second, "the first delivery to arrive", func() bool { return len(endpoint.deliveries()) == 1 })
	postMessage(t, handler, owner, roomID, "queued while she could still read")

	if code := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/leave", nil, member).Code; code >= 300 {
		t.Fatalf("leaving returned %d", code)
	}
	close(release)

	eventually(t, 5*time.Second, "the queued delivery to be cancelled", func() bool {
		for _, entry := range webhookHistory(t, handler, member, id)["deliveries"].([]any) {
			if entry.(map[string]any)["state"] == "cancelled" {
				return true
			}
		}
		return false
	})

	// And one after leaving, which the check when queueing refuses.
	postMessage(t, handler, owner, roomID, "after")
	time.Sleep(500 * time.Millisecond)

	if got := len(endpoint.deliveries()); got != 1 {
		t.Errorf("%d deliveries reached the endpoint; only the one before she left may", got)
	}
	history := webhookHistory(t, handler, member, id)
	if _, lost := history["accessLost"]; !lost {
		t.Error("the subscription does not show that access was lost")
	}
	if deliveries := history["deliveries"].([]any); len(deliveries) != 2 {
		t.Errorf("the history holds %d deliveries; losing access must not delete it", len(deliveries))
	}
}

/*
A subscription made before a new payload version keeps receiving the version it
was made against; one made after receives the new one.
*/
func TestAnExistingSubscriptionKeepsItsPayloadVersion(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, nil)
	before := newReceiver(t, answering(http.StatusOK))
	after := newReceiver(t, answering(http.StatusOK))

	subscribe(t, handler, owner, roomID, before.URL)
	restore := webhooks.AddVersionForTest(2, func(input webhooks.Input) ([]byte, error) {
		return json.Marshal(map[string]any{"version": 2, "id": input.Delivery, "kind": input.Event.Type})
	})
	defer restore()
	subscribe(t, handler, owner, roomID, after.URL)

	postMessage(t, handler, owner, roomID, "versions")
	eventually(t, 5*time.Second, "both deliveries", func() bool {
		return len(before.deliveries()) == 1 && len(after.deliveries()) == 1
	})

	var old, current struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(before.deliveries()[0].body, &old)
	_ = json.Unmarshal(after.deliveries()[0].body, &current)
	if old.Version != 1 || current.Version != 2 {
		t.Errorf("the older subscription received version %d and the newer %d", old.Version, current.Version)
	}
}

// Somebody else's subscription is neither visible nor removable.
func TestAWebhookBelongsToItsOwner(t *testing.T) {
	handler, owner, roomID := webhookFixture(t, nil)
	created := subscribe(t, handler, owner, roomID, "http://127.0.0.1:9/")
	other := registerAndSignIn(t, handler, "amina")

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if code := call(t, handler, method, "/api/me/webhooks/"+created["id"].(string), nil, other).Code; code != http.StatusNotFound {
			t.Errorf("%s by somebody else returned %d", method, code)
		}
	}
	if code := call(t, handler, http.MethodDelete, "/api/me/webhooks/"+created["id"].(string), nil, owner).Code; code != http.StatusNoContent {
		t.Errorf("the owner removing it returned %d", code)
	}
}
