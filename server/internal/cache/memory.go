package cache

import (
	"container/list"
	"context"
	"strings"
	"sync"
	"time"
)

// DefaultMaxEntries bounds the in-process cache.
//
// A bound is not optional: an unbounded map keyed by user or by client address
// is a memory leak that an unauthenticated caller controls the size of, and
// this server has a 1 GB budget to stay within.
const DefaultMaxEntries = 10_000

// DefaultTTL applies when a caller does not specify one.
const DefaultTTL = 5 * time.Minute

// Memory is an in-process cache with least-recently-used eviction.
//
// It is the default implementation, so a single-server deployment needs no
// cache service. Its contents are per-process, which is correct for a single
// process and wrong for several — see the package documentation.
type Memory struct {
	mu         sync.Mutex
	maxEntries int
	defaultTTL time.Duration
	entries    map[string]*list.Element
	// order holds entries most-recently-used first, so eviction takes the
	// back of the list without scanning.
	order *list.List
	now   func() time.Time
}

type entry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

// MemoryOption configures a Memory cache.
type MemoryOption func(*Memory)

// WithMaxEntries sets how many entries are kept before eviction.
func WithMaxEntries(limit int) MemoryOption {
	return func(m *Memory) {
		if limit > 0 {
			m.maxEntries = limit
		}
	}
}

// WithDefaultTTL sets the lifetime used when a caller does not give one.
func WithDefaultTTL(ttl time.Duration) MemoryOption {
	return func(m *Memory) {
		if ttl > 0 {
			m.defaultTTL = ttl
		}
	}
}

// withClock replaces the time source, so expiry can be tested without
// sleeping.
func withClock(now func() time.Time) MemoryOption {
	return func(m *Memory) { m.now = now }
}

// NewMemory builds an in-process cache.
func NewMemory(options ...MemoryOption) *Memory {
	m := &Memory{
		maxEntries: DefaultMaxEntries,
		defaultTTL: DefaultTTL,
		entries:    make(map[string]*list.Element),
		order:      list.New(),
		now:        time.Now,
	}
	for _, option := range options {
		option(m)
	}
	return m
}

// Get returns the value stored under key.
func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	element, ok := m.entries[key]
	if !ok {
		return nil, false, nil
	}

	held := element.Value.(*entry)
	if m.now().After(held.expiresAt) {
		m.removeElement(element)
		return nil, false, nil
	}

	m.order.MoveToFront(element)

	// Copy on the way out: the caller must not be able to mutate what is
	// still stored, and a shared implementation would have returned a fresh
	// slice anyway.
	value := make([]byte, len(held.value))
	copy(value, held.value)
	return value, true, nil
}

// Set stores a value, evicting the least recently used entry if the cache is
// full.
func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = m.defaultTTL
	}

	stored := make([]byte, len(value))
	copy(stored, value)

	m.mu.Lock()
	defer m.mu.Unlock()

	expiresAt := m.now().Add(ttl)

	if element, ok := m.entries[key]; ok {
		held := element.Value.(*entry)
		held.value = stored
		held.expiresAt = expiresAt
		m.order.MoveToFront(element)
		return nil
	}

	element := m.order.PushFront(&entry{key: key, value: stored, expiresAt: expiresAt})
	m.entries[key] = element

	for m.order.Len() > m.maxEntries {
		if oldest := m.order.Back(); oldest != nil {
			m.removeElement(oldest)
		}
	}

	return nil
}

// Delete removes a key.
func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if element, ok := m.entries[key]; ok {
		m.removeElement(element)
	}
	return nil
}

// DeletePrefix removes every key under a prefix.
func (m *Memory) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, element := range m.entries {
		if strings.HasPrefix(key, prefix) {
			m.removeElement(element)
		}
	}
	return nil
}

// Close releases resources. The in-process cache holds none.
func (m *Memory) Close() error { return nil }

// Len reports how many entries are held, for tests and for reporting memory
// use against the server's budget.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.order.Len()
}

// removeElement drops an entry. The caller holds the lock.
func (m *Memory) removeElement(element *list.Element) {
	m.order.Remove(element)
	delete(m.entries, element.Value.(*entry).key)
}
