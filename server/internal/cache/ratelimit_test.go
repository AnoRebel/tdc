package cache

import (
	"context"
	"testing"
	"time"
)

func TestAttemptsWithinLimitAreAllowed(t *testing.T) {
	limiter := NewLimiter(NewMemory())
	limit := RateLimit{Attempts: 3, Window: time.Minute}
	ctx := context.Background()

	for attempt := 1; attempt <= 3; attempt++ {
		decision, err := limiter.Allow(ctx, "key", limit)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("attempt %d was refused while within the limit", attempt)
		}
		if want := 3 - attempt; decision.Remaining != want {
			t.Errorf("attempt %d: remaining = %d, want %d", attempt, decision.Remaining, want)
		}
	}
}

func TestAttemptsBeyondLimitAreRefused(t *testing.T) {
	limiter := NewLimiter(NewMemory())
	limit := RateLimit{Attempts: 2, Window: time.Minute}
	ctx := context.Background()

	for range 2 {
		if _, err := limiter.Allow(ctx, "key", limit); err != nil {
			t.Fatalf("Allow: %v", err)
		}
	}

	decision, err := limiter.Allow(ctx, "key", limit)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if decision.Allowed {
		t.Fatal("an attempt beyond the limit was allowed")
	}
	// A refusal has to say when to try again, or a client can only guess.
	if decision.RetryAfter <= 0 {
		t.Error("the refusal gives no retry time")
	}
}

// Limits must be per key, so one person exhausting theirs does not lock out
// everyone else.
func TestLimitsAreIndependentPerKey(t *testing.T) {
	limiter := NewLimiter(NewMemory())
	limit := RateLimit{Attempts: 1, Window: time.Minute}
	ctx := context.Background()

	if _, err := limiter.Allow(ctx, "first", limit); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if decision, _ := limiter.Allow(ctx, "first", limit); decision.Allowed {
		t.Fatal("the first key was not limited")
	}

	decision, err := limiter.Allow(ctx, "second", limit)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !decision.Allowed {
		t.Error("exhausting one key's limit refused another key")
	}
}

func TestCountResetsAfterWindow(t *testing.T) {
	current := time.Now()
	memory := NewMemory(withClock(func() time.Time { return current }))
	limiter := NewLimiter(memory)
	limiter.now = func() time.Time { return current }

	limit := RateLimit{Attempts: 1, Window: time.Minute}
	ctx := context.Background()

	if _, err := limiter.Allow(ctx, "key", limit); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if decision, _ := limiter.Allow(ctx, "key", limit); decision.Allowed {
		t.Fatal("the limit was not applied")
	}

	current = current.Add(2 * time.Minute)

	decision, err := limiter.Allow(ctx, "key", limit)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !decision.Allowed {
		t.Error("attempts did not resume after the window passed")
	}
}

// A lockout must outlast the window, so repeated guessing does not simply
// resume the moment the window slides.
func TestLockoutOutlastsTheWindow(t *testing.T) {
	current := time.Now()
	memory := NewMemory(withClock(func() time.Time { return current }))
	limiter := NewLimiter(memory)
	limiter.now = func() time.Time { return current }

	limit := RateLimit{Attempts: 1, Window: time.Minute, LockoutAfter: 10 * time.Minute}
	ctx := context.Background()

	if _, err := limiter.Allow(ctx, "key", limit); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	decision, err := limiter.Allow(ctx, "key", limit)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if decision.Allowed {
		t.Fatal("the limit was not applied")
	}
	if decision.RetryAfter != 10*time.Minute {
		t.Errorf("retryAfter = %v, want the lockout duration", decision.RetryAfter)
	}

	// Past the window but still inside the lockout.
	current = current.Add(2 * time.Minute)
	if decision, _ := limiter.Allow(ctx, "key", limit); decision.Allowed {
		t.Error("attempts resumed while still locked out")
	}

	// Past the lockout.
	current = current.Add(10 * time.Minute)
	if decision, _ := limiter.Allow(ctx, "key", limit); !decision.Allowed {
		t.Error("attempts did not resume after the lockout expired")
	}
}

// A successful sign-in clears the count, so earlier failures do not count
// against a legitimate user.
func TestResetClearsTheCount(t *testing.T) {
	limiter := NewLimiter(NewMemory())
	limit := RateLimit{Attempts: 1, Window: time.Minute}
	ctx := context.Background()

	if _, err := limiter.Allow(ctx, "key", limit); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if err := limiter.Reset(ctx, "key"); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if decision, _ := limiter.Allow(ctx, "key", limit); !decision.Allowed {
		t.Error("the count was not cleared by Reset")
	}
}

// An unconfigured limit must not silently block everything.
func TestZeroLimitAllowsEverything(t *testing.T) {
	limiter := NewLimiter(NewMemory())
	ctx := context.Background()

	for range 100 {
		decision, err := limiter.Allow(ctx, "key", RateLimit{})
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if !decision.Allowed {
			t.Fatal("an unconfigured limit refused an attempt")
		}
	}
}
