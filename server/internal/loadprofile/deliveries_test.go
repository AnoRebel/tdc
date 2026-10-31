package loadprofile_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
)

/*
What the webhook delivery pool costs to keep running under sustained load.

A queue is a classic source of memory growth — an unbounded backlog held in
process, a goroutine per stalled receiver, response bodies read in full. This
one is built so none of that happens: the backlog is rows in Postgres, the pool
is fixed, a receiver's answer is read up to a kilobyte. This measures that
rather than asserting it: a thousand deliveries to prompt receivers while a
stalled one holds a worker for the whole run, and what the process retains
afterwards.

Reported, like the server's own figure, rather than gated. CI prints it with
its change from the base commit, so an increase is something somebody chose.
*/

const (
	deliverySubscriptions = 10
	deliveryEvents        = 100
)

func TestDeliveryPoolFitsItsMemoryTarget(t *testing.T) {
	url := dbtest.URL(t, "loaddeliveries")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Open(ctx, db.Config{URL: url}, quiet)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	clean := func() {
		for _, statement := range []string{"DELETE FROM rooms", "DELETE FROM users"} {
			if _, err := pool.Exec(context.Background(), statement); err != nil {
				t.Errorf("clean: %v", err)
			}
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	queries := dbgen.New(pool)
	key, err := events.NewKeyPair("loaddeliveries")
	if err != nil {
		t.Fatal(err)
	}
	keyring := events.NewKeyring(key)
	conversation := rooms.NewService(queries, keyring, "tdc.test", quiet)

	owner, err := identity.NewService(queries, quiet).CreateUser(ctx, identity.NewUser{
		Username: "owner", Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatal(err)
	}
	room, err := conversation.Create(ctx, rooms.NewRoom{Slug: "busy", Name: "busy", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}

	var delivered atomic.Int64
	prompt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delivered.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer prompt.Close()
	stalled := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		/*
			The body is read first: until it is, net/http does not watch the
			connection, so the request's context is never cancelled when the
			sender gives up — and a handler waiting on it waits for ever.
		*/
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer stalled.Close()

	signer := delivery.NewSigner(keyring)
	client := delivery.NewClient(time.Second, func() bool { return true })
	service := webhooks.New(queries, webhooks.Config{
		Server:       "tdc.test",
		CanRead:      func(context.Context, uuid.UUID, uuid.UUID) bool { return true },
		AllowPrivate: func(context.Context) bool { return true },
		Send: func(ctx context.Context, endpoint, id, kind string, body, secret []byte) delivery.Outcome {
			headers, err := signer.Sign(id, kind, body, secret)
			if err != nil {
				return delivery.Outcome{Error: err.Error()}
			}
			attempt, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			return client.Send(attempt, endpoint, headers, body)
		},
	}, quiet)

	for range deliverySubscriptions {
		if _, err := service.Subscribe(ctx, webhooks.NewSubscription{
			OwnerID: owner.ID, RoomID: room.ID, Label: "prompt", URL: prompt.URL,
			EventTypes: []string{"message.sent"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Subscribe(ctx, webhooks.NewSubscription{
		OwnerID: owner.ID, RoomID: room.ID, Label: "stalled", URL: stalled.URL,
		EventTypes: []string{"message.sent"},
	}); err != nil {
		t.Fatal(err)
	}

	running, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(running)
	}()
	defer func() {
		stop()
		<-done
	}()

	// Measured with the pool running and idle, so the figure below is what the
	// load cost rather than what the workers cost to exist.
	before := retained()

	for index := range deliveryEvents {
		event, err := conversation.SendMessage(ctx, room.ID, owner.ID, fmt.Sprintf("build %d", index), "")
		if err != nil {
			t.Fatal(err)
		}
		service.Enqueue(ctx, event)
	}

	want := int64(deliverySubscriptions * deliveryEvents)
	deadline := time.Now().Add(2 * time.Minute)
	for delivered.Load() < want && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := delivered.Load(); got < want {
		t.Fatalf("delivered %d of %d", got, want)
	}

	after := retained()

	// The contract with CI, as MEMORY_BYTES is for the server's own figure.
	fmt.Printf("DELIVERY_MEMORY_BYTES=%d\n", after)
	t.Logf("held from the system: %.1f MiB with the pool idle, %.1f MiB after %d deliveries beside a stalled receiver",
		float64(before)/1024/1024, float64(after)/1024/1024, want)

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	t.Logf("goroutines afterwards: %d; heap in use: %.1f MiB",
		runtime.NumGoroutine(), float64(stats.HeapInuse)/1024/1024)
}
