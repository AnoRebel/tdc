package federation

import (
	"testing"
	"time"
)

/*
Each retry waits longer than the one before, up to a ceiling.

The point is not the exact numbers but the shape: a peer that is down stops
being hammered, and a peer that is down for hours is still tried occasionally
rather than at intervals that eventually mean never.
*/
func TestRetriesBackOffTowardsACeiling(t *testing.T) {
	// Jitter makes any single sample uninformative, so each attempt is
	// sampled repeatedly and compared by its range.
	lowest := func(attempt int) time.Duration {
		least := time.Hour
		for range 200 {
			if delay := RetryDelay(attempt); delay < least {
				least = delay
			}
		}
		return least
	}

	previous := time.Duration(0)
	for attempt := range 6 {
		floor := lowest(attempt)
		if floor <= previous {
			t.Errorf("attempt %d waits no longer than attempt %d (%v vs %v)",
				attempt, attempt-1, floor, previous)
		}
		previous = floor
	}

	// And it stops growing rather than doubling forever.
	for _, attempt := range []int{20, 100, 1000} {
		if delay := RetryDelay(attempt); delay > MaxRetryDelay {
			t.Errorf("attempt %d waits %v, past the ceiling of %v", attempt, delay, MaxRetryDelay)
		}
	}
}

/*
Two peers failing at the same moment do not retry in the same instant.

Without jitter, every delivery failing when a peer goes down would retry
together and arrive as a thundering herd the moment it came back — which is
the failure the jitter exists to prevent, and which a fixed delay would
reproduce exactly.
*/
func TestRetriesAreJittered(t *testing.T) {
	seen := map[time.Duration]int{}
	for range 100 {
		seen[RetryDelay(3)]++
	}

	if len(seen) < 50 {
		t.Fatalf("100 retries produced only %d distinct delays; they are not jittered", len(seen))
	}
}

// Jitter stays within half the exponential value, so the average delay is
// sensible rather than arbitrarily short.
func TestJitterStaysWithinBounds(t *testing.T) {
	for attempt := range 4 {
		ceiling := time.Duration(float64(FirstRetryDelay) * float64(int(1)<<attempt))
		if ceiling > MaxRetryDelay {
			ceiling = MaxRetryDelay
		}

		for range 200 {
			delay := RetryDelay(attempt)
			if delay < ceiling/2 || delay > ceiling {
				t.Fatalf("attempt %d gave %v, outside [%v, %v]", attempt, delay, ceiling/2, ceiling)
			}
		}
	}
}

/*
A peer is abandoned past the ceiling rather than retried forever.

The spec's own requirement. A peer unreachable for a day is not coming back on
the next attempt, and continuing to try costs this server work it could spend
on peers that answer.
*/
func TestAPeerIsAbandonedPastTheCeiling(t *testing.T) {
	if Abandoned(0) || Abandoned(MaxDeliveryAttempts-1) {
		t.Error("a peer was abandoned before the ceiling")
	}
	if !Abandoned(MaxDeliveryAttempts) || !Abandoned(MaxDeliveryAttempts+5) {
		t.Error("a peer past the ceiling was not abandoned")
	}
}

// A negative attempt count is a bug somewhere, and must not produce a negative
// delay that would retry instantly and forever.
func TestANegativeAttemptStillWaits(t *testing.T) {
	if delay := RetryDelay(-3); delay <= 0 {
		t.Fatalf("RetryDelay(-3) = %v, which would retry without waiting", delay)
	}
}
