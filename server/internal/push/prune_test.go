package push_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/push"
)

/*
A device that stopped answering is eventually let go.

Its push service never said it was gone — one that says so has it removed at
once — it simply stopped working. Without this pass it is retried on every
notification for as long as the account exists: a request per notification,
forever, for a phone that was thrown away.
*/
func TestSubscriptionsThatStoppedWorkingAreRetired(t *testing.T) {
	pool, queries := pushTestPool(t)
	ctx := context.Background()

	owner := createPushUser(t, queries, "amina")
	failing := subscribe(t, queries, owner, "https://push.example/gone")
	healthy := subscribe(t, queries, owner, "https://push.example/fine")

	service := push.NewService(pool, push.Keys{
		Public: "public", Private: "private", Subject: "mailto:you@example.org",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// One failure short of the limit, which must not be enough: a service that
	// is briefly unwell recovers, and dropping devices for that would lose
	// somebody their notifications over an afternoon.
	for range 9 {
		if err := queries.RecordPushFailure(ctx, failing); err != nil {
			t.Fatalf("RecordPushFailure: %v", err)
		}
	}

	if _, err := service.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if remaining := count(t, queries, owner); remaining != 2 {
		t.Fatalf("a device was retired after nine failures: %d remain", remaining)
	}

	// The tenth.
	if err := queries.RecordPushFailure(ctx, failing); err != nil {
		t.Fatalf("RecordPushFailure: %v", err)
	}

	removed, err := service.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 1 {
		t.Errorf("pruning removed %d subscriptions, want 1", removed)
	}

	// And the working one is untouched, which is the half that would go
	// unnoticed: somebody whose notifications quietly stopped does not file a
	// report about it.
	remaining, err := service.Subscriptions(ctx, owner)
	if err != nil {
		t.Fatalf("Subscriptions: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Endpoint != healthy {
		t.Errorf("pruning left %v, want the working device alone", remaining)
	}
}

/*
A success clears the count.

A device that failed a few times and came back is working. Without this, ten
failures spread over a year would retire a subscription that has been
delivering the whole time.
*/
func TestASuccessForgivesEarlierFailures(t *testing.T) {
	pool, queries := pushTestPool(t)
	ctx := context.Background()

	owner := createPushUser(t, queries, "amina")
	endpoint := subscribe(t, queries, owner, "https://push.example/flaky")

	service := push.NewService(pool, push.Keys{
		Public: "public", Private: "private", Subject: "mailto:you@example.org",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for range 9 {
		if err := queries.RecordPushFailure(ctx, endpoint); err != nil {
			t.Fatalf("RecordPushFailure: %v", err)
		}
	}
	if err := queries.RecordPushSuccess(ctx, endpoint); err != nil {
		t.Fatalf("RecordPushSuccess: %v", err)
	}

	// Nine more. Were the earlier ones still counted, this would be eighteen
	// and the subscription would go.
	for range 9 {
		if err := queries.RecordPushFailure(ctx, endpoint); err != nil {
			t.Fatalf("RecordPushFailure: %v", err)
		}
	}

	if _, err := service.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if remaining := count(t, queries, owner); remaining != 1 {
		t.Error("a device that came back was retired for failures it recovered from")
	}
}

func pushTestPool(t *testing.T) (*db.Pool, *dbgen.Queries) {
	t.Helper()

	url := dbtest.URL(t, "push")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		if _, err := pool.Exec(cleanCtx, "DELETE FROM users"); err != nil {
			t.Errorf("clean: %v", err)
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	return pool, dbgen.New(pool)
}

func createPushUser(t *testing.T, queries *dbgen.Queries, username string) uuid.UUID {
	t.Helper()

	user, err := queries.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       username,
		UsernameFolded: username,
		DisplayName:    username,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user.ID
}

func subscribe(t *testing.T, queries *dbgen.Queries, owner uuid.UUID, endpoint string) string {
	t.Helper()

	if _, err := queries.SavePushSubscription(context.Background(),
		dbgen.SavePushSubscriptionParams{
			ID:        uuid.New(),
			UserID:    owner,
			Endpoint:  endpoint,
			P256dhKey: "key",
			AuthKey:   "secret",
			UserAgent: "Test",
		}); err != nil {
		t.Fatalf("SavePushSubscription: %v", err)
	}
	return endpoint
}

func count(t *testing.T, queries *dbgen.Queries, owner uuid.UUID) int {
	t.Helper()

	rows, err := queries.ListPushSubscriptions(context.Background(), owner)
	if err != nil {
		t.Fatalf("ListPushSubscriptions: %v", err)
	}
	return len(rows)
}

/*
An unreadable account is treated as one that asked for hiding.

If the preference cannot be read, whether somebody wanted message content kept
off their lock screen is unknown — and the harm is not symmetric. Withholding a
message from somebody who wanted to see it costs them a tap. Showing one they
asked to hide, to whoever is standing beside them, cannot be undone.
*/
func TestAnUnreadableAccountHidesContent(t *testing.T) {
	pool, queries := pushTestPool(t)
	ctx := context.Background()

	secret := "Nitakutumia pesa kesho asubuhi"

	sent := make(chan push.Payload, 1)
	service := push.NewService(pool, push.Keys{
		Public: "public", Private: "private", Subject: "mailto:you@example.org",
	}, slog.New(slog.NewTextHandler(io.Discard, nil))).WithSending(
		func(_ context.Context, _ uuid.UUID, payload push.Payload) error {
			sent <- payload
			return nil
		})

	// An account that does not exist, which is what an unreadable one looks
	// like from here.
	push.Deliveries(service).Deliver(ctx, uuid.New(), notify.Delivery{
		Sender: "Amina", Room: "general", Body: secret,
	})

	select {
	case payload := <-sent:
		if strings.Contains(payload.Body, secret) || strings.Contains(payload.Title, secret) {
			t.Errorf("an unreadable account was sent the message: %+v", payload)
		}
	default:
		t.Fatal("nothing was sent at all")
	}

	// The ordinary case still carries it, or the test above would pass on a
	// service that sends nothing to anybody.
	owner := createPushUser(t, queries, "juma")
	push.Deliveries(service).Deliver(ctx, owner, notify.Delivery{
		Sender: "Amina", Room: "general", Body: secret,
	})

	select {
	case payload := <-sent:
		if !strings.Contains(payload.Body, secret) {
			t.Errorf("a readable account lost its message: %+v", payload)
		}
	default:
		t.Fatal("nothing was sent to a readable account")
	}
}
