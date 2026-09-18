package cache_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/config"
)

/*
The shared cache, against a real Valkey.

Not a fake. What is being checked is that a limit is one limit across processes,
and the only thing that makes that true is Valkey running the decision to
completion before anything else touches the counter. A substitute that took a
mutex would prove that mutexes work.
*/

// valkeyForTest connects, or skips where there is none to connect to.
func valkeyForTest(t *testing.T) *cache.Valkey {
	t.Helper()

	address := os.Getenv("TDC_TEST_CACHE_ADDRESS")
	if address == "" {
		t.Skip("set TDC_TEST_CACHE_ADDRESS to run the shared cache tests")
	}

	connected, err := cache.NewValkey(config.CacheConfig{Address: address})
	if err != nil {
		t.Fatalf("connecting to %s: %v", address, err)
	}
	t.Cleanup(func() { _ = connected.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := connected.Ping(ctx); err != nil {
		t.Fatalf("the cache did not answer: %v", err)
	}

	return connected
}

// unique keeps one test's keys away from another's, since they share a Valkey.
func unique(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test:%s:%d:", t.Name(), time.Now().UnixNano())
}

/*
TestTwoNodesShareOneLimit is what this whole implementation is for.

Two limiters over one Valkey are two processes: separate clients, separate
counters in memory, and nothing between them but the cache. If the count were
per-process, a limit of five would let ten through — which is what the in-memory
cache does the moment somebody runs a second node, and why it cannot be the
answer for one.
*/
func TestTwoNodesShareOneLimit(t *testing.T) {
	shared := valkeyForTest(t)

	nodeOne := cache.NewSharedLimiter(shared)
	nodeTwo := cache.NewSharedLimiter(shared)

	key := unique(t) + "signin"
	limit := cache.RateLimit{Attempts: 5, Window: time.Minute}

	ctx := context.Background()
	allowed := 0

	// Alternating, as a load balancer would.
	for attempt := 0; attempt < 10; attempt++ {
		node := nodeOne
		if attempt%2 == 1 {
			node = nodeTwo
		}

		decision, err := node.Allow(ctx, key, limit)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
		if decision.Allowed {
			allowed++
		}
	}

	if allowed != 5 {
		t.Errorf("%d attempts allowed across two nodes, want 5 — the limit is per node rather than shared", allowed)
	}
}

/*
TestConcurrentAttemptsDoNotOvershoot.

The read-then-write race, which is the reason the decision runs inside Valkey
rather than around it. Twenty attempts at once against a limit of five: a
limiter that read, decided and wrote back would let several through on the same
stale count.
*/
func TestConcurrentAttemptsDoNotOvershoot(t *testing.T) {
	shared := valkeyForTest(t)

	key := unique(t) + "concurrent"
	limit := cache.RateLimit{Attempts: 5, Window: time.Minute}

	const attempts = 20
	results := make(chan bool, attempts)

	for index := 0; index < attempts; index++ {
		// A limiter each, as separate processes would have.
		go func() {
			decision, err := cache.NewSharedLimiter(shared).
				Allow(context.Background(), key, limit)
			if err != nil {
				results <- false
				return
			}
			results <- decision.Allowed
		}()
	}

	allowed := 0
	for index := 0; index < attempts; index++ {
		if <-results {
			allowed++
		}
	}

	if allowed != 5 {
		t.Errorf("%d of %d concurrent attempts allowed, want exactly 5", allowed, attempts)
	}
}

// TestALockoutOutlastsTheWindow, so repeated guessing does not resume the
// moment the window slides.
func TestALockoutOutlastsTheWindow(t *testing.T) {
	shared := valkeyForTest(t)
	limiter := cache.NewSharedLimiter(shared)

	key := unique(t) + "lockout"
	limit := cache.RateLimit{
		Attempts:     2,
		Window:       50 * time.Millisecond,
		LockoutAfter: 2 * time.Second,
	}

	ctx := context.Background()
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := limiter.Allow(ctx, key, limit); err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
	}

	// The window has passed; the lockout has not.
	time.Sleep(100 * time.Millisecond)

	decision, err := limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("after the window: %v", err)
	}
	if decision.Allowed {
		t.Error("the lockout ended with the window, so guessing simply resumes")
	}
	if decision.RetryAfter <= 0 {
		t.Error("no indication of how long to wait")
	}
}

// TestASuccessfulSignInClearsTheCount, so earlier failures do not count against
// somebody who then signs in correctly.
func TestASuccessfulSignInClearsTheCount(t *testing.T) {
	shared := valkeyForTest(t)
	limiter := cache.NewSharedLimiter(shared)

	key := unique(t) + "reset"
	limit := cache.RateLimit{Attempts: 3, Window: time.Minute}

	ctx := context.Background()
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := limiter.Allow(ctx, key, limit); err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
	}

	if err := limiter.Reset(ctx, key); err != nil {
		t.Fatalf("resetting: %v", err)
	}

	decision, err := limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("after the reset: %v", err)
	}
	if !decision.Allowed {
		t.Error("a reset did not clear the count")
	}
}

/*
TestTheSharedAndInProcessLimitersAgree.

Two implementations of one rule, and a server that behaved differently depending
on how it was deployed would be worse than one that only worked one way. They
are compared over the same sequence rather than asserted about separately.
*/
func TestTheSharedAndInProcessLimitersAgree(t *testing.T) {
	shared := cache.NewSharedLimiter(valkeyForTest(t))
	inProcess := cache.NewLimiter(cache.NewMemory())

	limit := cache.RateLimit{Attempts: 3, Window: time.Minute, LockoutAfter: time.Minute}
	sharedKey := unique(t) + "agree"
	ctx := context.Background()

	for attempt := 1; attempt <= 6; attempt++ {
		fromShared, err := shared.Allow(ctx, sharedKey, limit)
		if err != nil {
			t.Fatalf("shared, attempt %d: %v", attempt, err)
		}
		fromMemory, err := inProcess.Allow(ctx, "agree", limit)
		if err != nil {
			t.Fatalf("in-process, attempt %d: %v", attempt, err)
		}

		if fromShared.Allowed != fromMemory.Allowed {
			t.Errorf("attempt %d: shared says allowed=%v, in-process says %v",
				attempt, fromShared.Allowed, fromMemory.Allowed)
		}
		if fromShared.Remaining != fromMemory.Remaining {
			t.Errorf("attempt %d: shared says %d left, in-process says %d",
				attempt, fromShared.Remaining, fromMemory.Remaining)
		}
	}
}

// TestValuesExpire, because a cache that kept everything would be a database
// nobody meant to run.
func TestValuesExpire(t *testing.T) {
	shared := valkeyForTest(t)
	ctx := context.Background()
	key := unique(t) + "expiring"

	if err := shared.Set(ctx, key, []byte("here"), 100*time.Millisecond); err != nil {
		t.Fatalf("setting: %v", err)
	}
	if _, found, _ := shared.Get(ctx, key); !found {
		t.Fatal("the value was not there immediately after being set")
	}

	time.Sleep(200 * time.Millisecond)

	if _, found, _ := shared.Get(ctx, key); found {
		t.Error("the value outlived its expiry")
	}
}

/*
TestDeletingAPrefixTakesOnlyThatPrefix.

Used when somebody's roles change, to drop everything derived from them. The
prefixes carry identifiers, which are somebody else's choice of characters —
so a prefix containing a glob metacharacter must not take a neighbour's keys
with it.
*/
func TestDeletingAPrefixTakesOnlyThatPrefix(t *testing.T) {
	shared := valkeyForTest(t)
	ctx := context.Background()
	base := unique(t)

	for _, key := range []string{base + "a:1", base + "a:2", base + "b:1"} {
		if err := shared.Set(ctx, key, []byte("x"), time.Minute); err != nil {
			t.Fatalf("setting %s: %v", key, err)
		}
	}

	if err := shared.DeletePrefix(ctx, base+"a:"); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	for _, gone := range []string{base + "a:1", base + "a:2"} {
		if _, found, _ := shared.Get(ctx, gone); found {
			t.Errorf("%s survived", gone)
		}
	}
	if _, found, _ := shared.Get(ctx, base+"b:1"); !found {
		t.Error("a neighbouring prefix was deleted too")
	}
}
