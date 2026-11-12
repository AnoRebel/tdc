package cache

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestStoreAndRetrieve(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()

	if err := c.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	value, found, err := c.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("the stored value was not found")
	}
	if string(value) != "value" {
		t.Errorf("value = %q, want %q", value, "value")
	}
}

func TestMissingKeyIsReportedNotFound(t *testing.T) {
	_, found, err := NewMemory().Get(context.Background(), "absent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Error("a key that was never stored was found")
	}
}

// The cache must be bounded: an unbounded map keyed by user or client address
// is a memory leak whose size an unauthenticated caller controls.
func TestCacheEvictsBeyondItsLimit(t *testing.T) {
	const limit = 50
	c := NewMemory(WithMaxEntries(limit))
	ctx := context.Background()

	for i := range limit * 4 {
		if err := c.Set(ctx, strconv.Itoa(i), []byte("v"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	if held := c.Len(); held > limit {
		t.Errorf("cache holds %d entries, above its limit of %d", held, limit)
	}
}

// Eviction must drop the least recently used entry, so that a value still in
// use is not discarded ahead of one that is not.
func TestEvictionDropsLeastRecentlyUsed(t *testing.T) {
	c := NewMemory(WithMaxEntries(3))
	ctx := context.Background()

	for _, key := range []string{"a", "b", "c"} {
		if err := c.Set(ctx, key, []byte(key), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	// Touching "a" makes "b" the least recently used.
	if _, _, err := c.Get(ctx, "a"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := c.Set(ctx, "d", []byte("d"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, found, _ := c.Get(ctx, "b"); found {
		t.Error("the least recently used entry survived eviction")
	}
	if _, found, _ := c.Get(ctx, "a"); !found {
		t.Error("a recently used entry was evicted")
	}
}

func TestExpiredEntryIsNotReturned(t *testing.T) {
	current := time.Now()
	c := NewMemory(withClock(func() time.Time { return current }))
	ctx := context.Background()

	if err := c.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	current = current.Add(2 * time.Minute)

	if _, found, _ := c.Get(ctx, "key"); found {
		t.Error("an expired entry was returned")
	}
}

// Deleting must take effect at once. Cached authority has to be invalidated
// rather than left to expire, or a removed permission would still apply.
func TestDeleteTakesEffectImmediately(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()

	if err := c.Set(ctx, "key", []byte("value"), time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, found, _ := c.Get(ctx, "key"); found {
		t.Error("a deleted entry was still returned")
	}
}

// A role change should drop everything derived for that user in one call,
// without touching anyone else's entries.
func TestDeletePrefixIsScoped(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()

	for _, key := range []string{"perm:alice", "perm:alice:channel", "perm:bob", "sess:alice"} {
		if err := c.Set(ctx, key, []byte("v"), time.Hour); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	if err := c.DeletePrefix(ctx, "perm:alice"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}

	for _, gone := range []string{"perm:alice", "perm:alice:channel"} {
		if _, found, _ := c.Get(ctx, gone); found {
			t.Errorf("%s survived the prefix delete", gone)
		}
	}
	for _, kept := range []string{"perm:bob", "sess:alice"} {
		if _, found, _ := c.Get(ctx, kept); !found {
			t.Errorf("%s was removed by an unrelated prefix delete", kept)
		}
	}
}

// A caller must not be able to mutate what remains stored.
func TestStoredValuesAreCopied(t *testing.T) {
	c := NewMemory()
	ctx := context.Background()

	original := []byte("value")
	if err := c.Set(ctx, "key", original, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	original[0] = 'X'

	stored, _, _ := c.Get(ctx, "key")
	if string(stored) != "value" {
		t.Errorf("mutating the caller's slice changed the stored value: %q", stored)
	}

	stored[0] = 'Y'
	again, _, _ := c.Get(ctx, "key")
	if string(again) != "value" {
		t.Errorf("mutating a returned slice changed the stored value: %q", again)
	}
}

// The cache is shared across every request, so concurrent use must be safe.
func TestConcurrentUseIsSafe(t *testing.T) {
	c := NewMemory(WithMaxEntries(100))
	ctx := context.Background()

	var wait sync.WaitGroup
	for worker := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range 200 {
				key := fmt.Sprintf("w%d-k%d", worker, i)
				_ = c.Set(ctx, key, []byte("v"), time.Minute)
				_, _, _ = c.Get(ctx, key)
				if i%3 == 0 {
					_ = c.Delete(ctx, key)
				}
			}
		}()
	}
	wait.Wait()

	if held := c.Len(); held > 100 {
		t.Errorf("cache holds %d entries under concurrent use, above its limit", held)
	}
}
