package federation

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

/*
A signature presented twice inside its window is refused the second time.

This is the case the timestamp window alone cannot refuse: every field is
valid, the clock agrees, and the only thing wrong with the request is that it
has already been answered.
*/
func TestASignatureSeenTwiceIsRefused(t *testing.T) {
	cache := NewReplayCache()
	expires := time.Now().Add(SignatureLifetime)

	if !cache.Remember([]byte("signature-bytes"), expires) {
		t.Fatal("the first presentation must be accepted")
	}
	if cache.Remember([]byte("signature-bytes"), expires) {
		t.Fatal("the second presentation of the same signature must be refused")
	}
}

// Different signatures do not collide, or every second request from a busy
// peer would be refused as a replay.
func TestDistinctSignaturesAreIndependent(t *testing.T) {
	cache := NewReplayCache()
	expires := time.Now().Add(SignatureLifetime)

	for i := range 100 {
		if !cache.Remember([]byte(fmt.Sprintf("signature-%d", i)), expires) {
			t.Fatalf("distinct signature %d was refused as a replay", i)
		}
	}
	if cache.Size() != 100 {
		t.Fatalf("cache holds %d, want 100", cache.Size())
	}
}

/*
Entries are dropped once their window has passed.

Without this the cache grows for as long as the server runs, which is the
unbounded-memory failure the design forbids. The clock is moved rather than
slept on, so the test is fast and exact.
*/
func TestExpiredEntriesAreEvicted(t *testing.T) {
	cache := NewReplayCache()
	start := time.Now()
	cache.now = func() time.Time { return start }

	for i := range 50 {
		cache.Remember([]byte(fmt.Sprintf("old-%d", i)), start.Add(SignatureLifetime))
	}
	if cache.Size() != 50 {
		t.Fatalf("cache holds %d, want 50", cache.Size())
	}

	// Past every window, and one more write to trigger the sweep.
	later := start.Add(2 * SignatureLifetime)
	cache.now = func() time.Time { return later }
	cache.Remember([]byte("fresh"), later.Add(SignatureLifetime))

	if cache.Size() != 1 {
		t.Fatalf("after expiry the cache holds %d, want only the fresh entry", cache.Size())
	}
}

/*
The cache stays bounded even when nothing has expired.

A peer flooding distinct, valid, unexpired signatures would otherwise grow it
without limit — expiry-based eviction alone does not save a cache filling
faster than its entries age out.
*/
func TestTheCacheStaysBoundedUnderAFlood(t *testing.T) {
	cache := NewReplayCache()
	cache.capacity = 64
	now := time.Now()
	cache.now = func() time.Time { return now }

	// Ten times capacity, none of it expired.
	for i := range 640 {
		cache.Remember([]byte(fmt.Sprintf("flood-%d", i)), now.Add(SignatureLifetime))
	}

	if cache.Size() > cache.capacity {
		t.Fatalf("cache grew to %d, past its capacity of %d", cache.Size(), cache.capacity)
	}
}

/*
Two requests arriving together must not both be told they are the first.

The check and the record are one operation under one lock for exactly this: a
read-then-write would let two copies of a captured request both pass while each
was looking at a cache that did not yet contain the other.
*/
func TestConcurrentPresentationsOfOneSignatureAdmitExactlyOne(t *testing.T) {
	cache := NewReplayCache()
	expires := time.Now().Add(SignatureLifetime)

	const racers = 64
	var wg sync.WaitGroup
	results := make(chan bool, racers)

	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()
			results <- cache.Remember([]byte("the-same-signature"), expires)
		}()
	}
	wg.Wait()
	close(results)

	accepted := 0
	for first := range results {
		if first {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("%d of %d concurrent presentations were accepted, want exactly 1", accepted, racers)
	}
}
