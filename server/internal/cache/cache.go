// Package cache holds short-lived derived values.
//
// A single-server deployment must need nothing beyond the server and its
// database, so the default implementation is in-process. An operator running
// several server processes configures a shared implementation instead, which
// is the only way per-process state stops being correct — most importantly for
// rate limiting, where per-process counters let a caller be served N times the
// configured allowance.
package cache

import (
	"context"
	"time"
)

// Cache stores values under a key for a bounded time.
//
// Values are opaque bytes rather than a generic type parameter: the shared
// implementation has to serialise them anyway, and keeping one interface for
// both avoids two divergent shapes.
type Cache interface {
	// Get returns the value stored under key, and whether it was present.
	Get(ctx context.Context, key string) ([]byte, bool, error)

	// Set stores value under key for ttl. A ttl of zero uses the
	// implementation's default.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a key.
	//
	// This is what makes a revocation take effect at once. Cached authority
	// must be invalidated rather than left to expire: waiting out a ttl would
	// mean a removed permission still applied for a while, which the
	// specification forbids.
	Delete(ctx context.Context, key string) error

	// DeletePrefix removes every key under a prefix, so that all of a user's
	// derived state can be dropped in one call when their roles change.
	DeletePrefix(ctx context.Context, prefix string) error

	// Close releases any resources held.
	Close() error
}

// Key namespaces, so that one subsystem's keys cannot collide with another's
// and a prefix delete cannot reach further than intended.
const (
	// KeyPermissions holds a user's resolved permissions.
	KeyPermissions = "perm:"
	// KeySession holds a resolved session.
	KeySession = "sess:"
	// KeyRateLimit holds rate-limiter counters.
	KeyRateLimit = "rate:"
)
