package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

/*
Counting attempts across processes.

Sharing a cache is not enough on its own. The limiter beside this reads a
counter, decides, and writes it back — three steps, and between the read and the
write another process can do the same. Two nodes handling two attempts at once
both read "four used", both allow, and both write "five": six attempts against a
limit of five, and the more nodes there are the worse it gets.

So the whole decision happens inside Valkey, in one script. A script runs to
completion before anything else touches those keys, which is what makes the
count one count rather than one per process.
*/

/*
allowScript is the limiter's logic, run where the counter lives.

It mirrors the in-memory limiter deliberately — same window, same lockout, same
answers — because two implementations that disagree are a server that behaves
differently depending on how it is deployed.

KEYS[1]  the counter
ARGV[1]  attempts permitted in the window
ARGV[2]  the window, in milliseconds
ARGV[3]  the lockout, in milliseconds; zero for none
ARGV[4]  now, in milliseconds

Returns { allowed, retryAfterMillis, remaining }.
*/
var allowScript = valkey.NewLuaScript(`
local key       = KEYS[1]
local permitted = tonumber(ARGV[1])
local window    = tonumber(ARGV[2])
local lockout   = tonumber(ARGV[3])
local now       = tonumber(ARGV[4])

local attempts    = tonumber(redis.call("HGET", key, "attempts")) or 0
local window_ends = tonumber(redis.call("HGET", key, "window_ends")) or 0
local locked_till = tonumber(redis.call("HGET", key, "locked_till")) or 0

-- A lockout outlasts the window, so repeated guessing does not resume the
-- moment the window slides.
if locked_till > now then
  return { 0, locked_till - now, 0 }
end

-- A window that has passed starts a fresh count.
if window_ends <= now then
  attempts = 0
  window_ends = now + window
end

attempts = attempts + 1

local allowed = 1
local retry_after = 0

if attempts > permitted then
  allowed = 0
  retry_after = window_ends - now
  if lockout > 0 then
    locked_till = now + lockout
    retry_after = lockout
  end
end

redis.call("HSET", key,
  "attempts", attempts,
  "window_ends", window_ends,
  "locked_till", locked_till)

-- Kept at least as long as the thing it describes, so a counter cannot outlive
-- its own meaning or vanish before it.
local ttl = window
if lockout > ttl then ttl = lockout end
redis.call("PEXPIRE", key, ttl + 1000)

local remaining = permitted - attempts
if remaining < 0 then remaining = 0 end

return { allowed, retry_after, remaining }
`)

/*
SharedLimiter counts attempts in Valkey, where every node sees the same count.

Interchangeable with Limiter: same methods, same answers. A caller is written
against one shape and the deployment decides which does the counting.
*/
type SharedLimiter struct {
	cache *Valkey
	now   func() time.Time
}

// NewSharedLimiter builds a limiter over a Valkey.
func NewSharedLimiter(cache *Valkey) *SharedLimiter {
	return &SharedLimiter{cache: cache, now: time.Now}
}

// Allow records an attempt and says whether it is permitted.
func (l *SharedLimiter) Allow(ctx context.Context, key string, limit RateLimit) (Decision, error) {
	if limit.Attempts <= 0 || limit.Window <= 0 {
		return Decision{Allowed: true}, nil
	}

	result, err := allowScript.Exec(ctx, l.cache.client,
		[]string{KeyRateLimit + key},
		[]string{
			strconv.Itoa(limit.Attempts),
			strconv.FormatInt(limit.Window.Milliseconds(), 10),
			strconv.FormatInt(limit.LockoutAfter.Milliseconds(), 10),
			strconv.FormatInt(l.now().UnixMilli(), 10),
		},
	).ToArray()
	if err != nil {
		return Decision{}, fmt.Errorf("count the attempt: %w", err)
	}
	if len(result) != 3 {
		return Decision{}, fmt.Errorf("the cache answered with %d values, expected 3", len(result))
	}

	allowed, err := result[0].AsInt64()
	if err != nil {
		return Decision{}, fmt.Errorf("reading whether it was allowed: %w", err)
	}
	retryAfter, err := result[1].AsInt64()
	if err != nil {
		return Decision{}, fmt.Errorf("reading how long to wait: %w", err)
	}
	remaining, err := result[2].AsInt64()
	if err != nil {
		return Decision{}, fmt.Errorf("reading what is left: %w", err)
	}

	return Decision{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(retryAfter) * time.Millisecond,
		Remaining:  int(remaining),
	}, nil
}

// Reset clears the counter, which a successful sign-in does so earlier failures
// do not count against somebody who then signs in correctly.
func (l *SharedLimiter) Reset(ctx context.Context, key string) error {
	return l.cache.Delete(ctx, KeyRateLimit+key)
}
