package delivery_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/delivery"
)

func always(allowed bool) func() bool { return func() bool { return allowed } }

// An endpoint on a loopback, private or link-local address is refused when a
// webhook is set up, unless the operator has said otherwise.
func TestPrivateDestinationsAreRefused(t *testing.T) {
	ctx := context.Background()
	for _, endpoint := range []string{
		"http://127.0.0.1:9/hook",
		"http://localhost:9/hook",
		"http://10.0.0.7/hook",
		"http://192.168.1.1/hook",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]:9/hook",
		"http://[::ffff:127.0.0.1]/hook",
	} {
		if err := delivery.CheckDestination(ctx, endpoint, false); !errors.Is(err, delivery.ErrPrivateDestination) {
			t.Errorf("%s: got %v, want ErrPrivateDestination", endpoint, err)
		}
		if err := delivery.CheckDestination(ctx, endpoint, true); err != nil {
			t.Errorf("%s with private addresses permitted: %v", endpoint, err)
		}
	}

	for _, endpoint := range []string{"ftp://example.org/", "http:///nohost", "https://user:pass@example.org/"} {
		if err := delivery.CheckDestination(ctx, endpoint, true); !errors.Is(err, delivery.ErrUnusableURL) {
			t.Errorf("%s: got %v, want ErrUnusableURL", endpoint, err)
		}
	}
}

/*
The guard holds at connection time too, not only when the webhook was set up.

What a name resolved to when somebody created the subscription says nothing
about what it resolves to when a delivery is made; the dialer is what refuses,
on the address actually being connected to.
*/
func TestTheDialerRefusesAPrivateAddressAtDeliveryTime(t *testing.T) {
	var reached atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached.Add(1)
	}))
	defer receiver.Close()

	outcome := delivery.NewClient(2*time.Second, always(false)).
		Send(context.Background(), receiver.URL, http.Header{}, []byte(`{}`))

	if outcome.Delivered || reached.Load() != 0 {
		t.Fatalf("a delivery reached a loopback receiver with private addresses forbidden: %+v", outcome)
	}
	if outcome.Error == "" {
		t.Error("the refusal recorded no reason an owner could act on")
	}
}

/*
A redirect is not followed, even to an address the guard would allow.

Private addresses are permitted here so the only thing that can stop the
second request is the client refusing to follow; the receiver's 3xx is the
answer, and it is recorded as a failure.
*/
func TestARedirectIsNotFollowed(t *testing.T) {
	var followed atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		followed.Add(1)
	}))
	defer elsewhere.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()

	outcome := delivery.NewClient(2*time.Second, always(true)).
		Send(context.Background(), redirecting.URL, http.Header{}, []byte(`{}`))

	if followed.Load() != 0 {
		t.Fatal("the redirect was followed")
	}
	if outcome.Delivered || outcome.Status != http.StatusTemporaryRedirect {
		t.Errorf("a redirect was recorded as %+v, want an undelivered 307", outcome)
	}
}

// A receiver that never answers costs a bounded wait, recorded as a timeout.
func TestASilentReceiverTimesOut(t *testing.T) {
	release := make(chan struct{})
	silent := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer silent.Close()
	defer close(release)

	started := time.Now()
	outcome := delivery.NewClient(300*time.Millisecond, always(true)).
		Send(context.Background(), silent.URL, http.Header{}, []byte(`{}`))

	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("waited %s for a receiver that never answers", elapsed)
	}
	if outcome.Delivered || outcome.Error == "" {
		t.Errorf("a silent receiver was recorded as %+v", outcome)
	}
}
