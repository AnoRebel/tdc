package cache

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

// RateLimit describes how many attempts are allowed over what period.
type RateLimit struct {
	// Attempts allowed within Window.
	Attempts int
	Window   time.Duration
	// LockoutAfter, when non-zero, extends the refusal for this long once the
	// limit is exceeded, rather than letting attempts resume as the window
	// slides. Progressive delay is what makes repeated guessing expensive.
	LockoutAfter time.Duration
}

// Decision is the outcome of a rate-limit check.
type Decision struct {
	Allowed bool
	// Remaining attempts before the limit is reached.
	Remaining int
	// RetryAfter says how long to wait, when refused.
	RetryAfter time.Duration
}

/*
Rates is what a caller needs of a rate limiter.

An interface because there are two: one counting in this process, right for the
single-node deployment almost everybody runs, and one counting in Valkey, which
is the only way several nodes can share a limit. A caller is written against
this and the deployment decides which does the counting.
*/
type Rates interface {
	Allow(ctx context.Context, key string, limit RateLimit) (Decision, error)
	Reset(ctx context.Context, key string) error
}

// Limiter counts attempts against a limit.
//
// It is built on the Cache interface so that a single process and a multi-node
// deployment share one implementation. This matters more here than for
// ordinary caching: per-process counters would let a caller be served N times
// the configured allowance across N processes, which is a security failure
// rather than a performance one.
type Limiter struct {
	cache Cache
	now   func() time.Time
}

// NewLimiter builds a limiter over a cache.
func NewLimiter(c Cache) *Limiter {
	return &Limiter{cache: c, now: time.Now}
}

// counter is the stored state for one limited key.
type counter struct {
	attempts   int64
	windowEnds int64 // Unix nanoseconds
	lockedTill int64 // Unix nanoseconds, zero when not locked out
}

func (c counter) encode() []byte {
	buffer := make([]byte, 24)
	binary.BigEndian.PutUint64(buffer[0:8], uint64(c.attempts))
	binary.BigEndian.PutUint64(buffer[8:16], uint64(c.windowEnds))
	binary.BigEndian.PutUint64(buffer[16:24], uint64(c.lockedTill))
	return buffer
}

func decodeCounter(buffer []byte) (counter, bool) {
	if len(buffer) != 24 {
		return counter{}, false
	}
	return counter{
		attempts:   int64(binary.BigEndian.Uint64(buffer[0:8])),
		windowEnds: int64(binary.BigEndian.Uint64(buffer[8:16])),
		lockedTill: int64(binary.BigEndian.Uint64(buffer[16:24])),
	}, true
}

// Allow records an attempt and reports whether it may proceed.
//
// The key should already be normalised by the caller: rate limiting keyed on
// a raw identifier could be sidestepped by varying its case.
func (l *Limiter) Allow(ctx context.Context, key string, limit RateLimit) (Decision, error) {
	if limit.Attempts <= 0 || limit.Window <= 0 {
		return Decision{Allowed: true}, nil
	}

	full := KeyRateLimit + key
	now := l.now()

	stored, found, err := l.cache.Get(ctx, full)
	if err != nil {
		return Decision{}, fmt.Errorf("read rate limit counter: %w", err)
	}

	state := counter{windowEnds: now.Add(limit.Window).UnixNano()}
	if found {
		if decoded, ok := decodeCounter(stored); ok {
			state = decoded
		}
	}

	// A lockout outlasts the window, so repeated guessing does not simply
	// resume the moment the window slides.
	if state.lockedTill > now.UnixNano() {
		return Decision{
			Allowed:    false,
			RetryAfter: time.Duration(state.lockedTill - now.UnixNano()),
		}, nil
	}

	// A window that has passed starts a fresh count.
	if state.windowEnds <= now.UnixNano() {
		state = counter{windowEnds: now.Add(limit.Window).UnixNano()}
	}

	state.attempts++

	if state.attempts > int64(limit.Attempts) {
		retryAfter := time.Duration(state.windowEnds - now.UnixNano())
		if limit.LockoutAfter > 0 {
			state.lockedTill = now.Add(limit.LockoutAfter).UnixNano()
			retryAfter = limit.LockoutAfter
		}

		if err := l.store(ctx, full, state, limit); err != nil {
			return Decision{}, err
		}
		return Decision{Allowed: false, RetryAfter: retryAfter}, nil
	}

	if err := l.store(ctx, full, state, limit); err != nil {
		return Decision{}, err
	}

	return Decision{
		Allowed:   true,
		Remaining: limit.Attempts - int(state.attempts),
	}, nil
}

// Reset clears the counter for a key, which a successful sign-in does so that
// earlier failures do not count against a legitimate user.
func (l *Limiter) Reset(ctx context.Context, key string) error {
	if err := l.cache.Delete(ctx, KeyRateLimit+key); err != nil {
		return fmt.Errorf("reset rate limit counter: %w", err)
	}
	return nil
}

// store writes the counter, keeping it at least as long as the window or
// lockout it describes.
func (l *Limiter) store(ctx context.Context, key string, state counter, limit RateLimit) error {
	ttl := limit.Window
	if limit.LockoutAfter > ttl {
		ttl = limit.LockoutAfter
	}
	if err := l.cache.Set(ctx, key, state.encode(), ttl); err != nil {
		return fmt.Errorf("store rate limit counter: %w", err)
	}
	return nil
}

// Both implementations satisfy the interface, checked here rather than at a
// call site that happens to use one of them.
var (
	_ Rates = (*Limiter)(nil)
	_ Rates = (*SharedLimiter)(nil)
)
