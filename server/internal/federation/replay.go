package federation

import (
	"encoding/base64"
	"sync"
	"time"
)

/*
Refusing a request that has already been seen.

The created/expires window shrinks the replay problem; it does not close it. A
captured request replayed to the same destination inside its window is still
perfectly valid by every check in signing.go — same method, same path, same
authority, same body, same signature, and a timestamp that has not yet passed.
Only remembering that we have seen it before refuses it.

Memory is bounded two ways, because either one alone fails. Eviction by expiry
alone is unbounded under a flood: a peer sending thousands of distinct signed
requests a second fills the map faster than entries expire. A fixed capacity
alone would let a flood evict genuine recent signatures and reopen the replay
window for them. So: entries expire, and when capacity is reached the oldest go
first.
*/

// defaultReplayCapacity bounds how many signatures are remembered at once.
//
// Generous relative to the traffic a federated server actually sees inside a
// thirty-second window, so that reaching it means something abnormal is
// happening rather than that the server is busy.
const defaultReplayCapacity = 16384

// seen is one remembered signature and when it stops mattering.
type seen struct {
	expires time.Time
}

/*
ReplayCache remembers signatures inside their validity window.

Keyed by the signature bytes themselves, which is the whole request condensed:
every covered field, the timestamps, and the nonce that distinguishes two
otherwise identical requests. That last part is load-bearing and was learned
the hard way — a timestamp alone does not make a GET with no body unique, so
two such requests in the same second signed identically and the second was
refused as a replay of the first.

Keying on the signature rather than on the nonce alone means a sender cannot
vary one field to get a fresh identity for the same request: change anything
covered and it is a different request, which is what it should be.
*/
type ReplayCache struct {
	mu       sync.Mutex
	entries  map[string]seen
	capacity int
	now      func() time.Time
}

// NewReplayCache builds a cache with the default capacity.
func NewReplayCache() *ReplayCache {
	return &ReplayCache{
		entries:  make(map[string]seen),
		capacity: defaultReplayCapacity,
		now:      time.Now,
	}
}

/*
Remember records a signature, reporting whether it had already been seen.

True means this is the first time and the request may proceed; false means it
is a replay. The check and the record are one operation under one lock, because
two requests arriving together must not both be told they are the first.
*/
func (c *ReplayCache) Remember(signature []byte, expires time.Time) bool {
	key := base64.StdEncoding.EncodeToString(signature)

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()

	if entry, found := c.entries[key]; found {
		// A previously seen signature whose window has passed is not a replay
		// worth refusing on these grounds — signing.go already refused it for
		// being expired. Treating it as unseen keeps the two checks from
		// disagreeing about the same request.
		if now.Before(entry.expires) {
			return false
		}
	}

	c.evictExpired(now)
	if len(c.entries) >= c.capacity {
		c.evictOldest()
	}

	c.entries[key] = seen{expires: expires}
	return true
}

// evictExpired drops everything whose window has passed. Called on write
// rather than on a timer: a cache nobody is writing to needs no sweeping, and
// a background goroutine would outlive the thing it serves.
func (c *ReplayCache) evictExpired(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
		}
	}
}

/*
evictOldest makes room when capacity is reached and nothing has expired.

Only reachable under a flood of distinct valid signatures, which is why it is
crude: it drops the entry closest to expiring, which is the one whose replay
window is shortest and so the least valuable to keep.
*/
func (c *ReplayCache) evictOldest() {
	var oldestKey string
	var oldest time.Time

	for key, entry := range c.entries {
		if oldestKey == "" || entry.expires.Before(oldest) {
			oldestKey, oldest = key, entry.expires
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// Size reports how many signatures are currently remembered.
func (c *ReplayCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
