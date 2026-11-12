package cache

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/AnoRebel/tdc/server/internal/config"
)

/*
A cache several processes share.

The in-memory one beside this is right for the deployment almost every operator
runs: one process, nothing else to install. What it cannot do is be shared, and
the moment there are two processes that matters — each would hold its own
rate-limit counters, so a limit of five attempts would in practice be five per
node, which is not the limit anybody configured.

Valkey rather than Redis because Redis is no longer open source under a licence
this project can depend on, and Valkey is the fork that stayed. Nothing here
uses anything the two do not share, so an operator who already runs a Redis can
point this at it.
*/
type Valkey struct {
	client valkey.Client
	// defaultTTL is what a value gets when the caller asks for nothing, so a
	// forgotten expiry is a short life rather than a permanent one.
	defaultTTL time.Duration
}

// NewValkey connects to a Valkey, or says why it could not.
func NewValkey(settings config.CacheConfig) (*Valkey, error) {
	if strings.TrimSpace(settings.Address) == "" {
		return nil, errors.New("no cache address is configured")
	}

	option := valkey.ClientOption{
		InitAddress: []string{settings.Address},
		Username:    settings.Username,
		Password:    settings.Password,
		SelectDB:    int(settings.Database),
	}
	if settings.TLS {
		option.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client, err := valkey.NewClient(option)
	if err != nil {
		return nil, fmt.Errorf("connect to the cache: %w", err)
	}

	return &Valkey{client: client, defaultTTL: DefaultTTL}, nil
}

// Get returns the value stored under key.
func (v *Valkey) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := v.client.Do(ctx, v.client.B().Get().Key(key).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read from the cache: %w", err)
	}
	return value, true, nil
}

// Set stores a value for a bounded time.
func (v *Valkey) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = v.defaultTTL
	}

	err := v.client.Do(ctx, v.client.B().Set().
		Key(key).Value(valkey.BinaryString(value)).
		// Milliseconds rather than seconds: a lockout is expressed as a
		// duration and rounding one down to the second would end it early.
		Px(ttl).
		Build()).Error()
	if err != nil {
		return fmt.Errorf("write to the cache: %w", err)
	}
	return nil
}

// Delete removes a key, which is what makes a revocation take effect at once.
func (v *Valkey) Delete(ctx context.Context, key string) error {
	if err := v.client.Do(ctx, v.client.B().Del().Key(key).Build()).Error(); err != nil {
		return fmt.Errorf("delete from the cache: %w", err)
	}
	return nil
}

/*
DeletePrefix removes every key under a prefix, so all of somebody's derived
state can be dropped at once when their roles change.

SCAN rather than KEYS. KEYS walks the whole keyspace in one blocking call, which
on a server doing anything else is a pause everybody notices; SCAN returns a
cursor and lets other work through between batches. The cost is that it may see
a key added during the walk, or miss one deleted — neither matters here, where
the point is to invalidate rather than to enumerate.
*/
func (v *Valkey) DeletePrefix(ctx context.Context, prefix string) error {
	var cursor uint64

	for {
		entry, err := v.client.Do(ctx, v.client.B().Scan().
			Cursor(cursor).
			Match(escapeGlob(prefix)+"*").
			Count(scanBatch).
			Build()).AsScanEntry()
		if err != nil {
			return fmt.Errorf("scan the cache: %w", err)
		}

		if len(entry.Elements) > 0 {
			if err := v.client.Do(ctx,
				v.client.B().Del().Key(entry.Elements...).Build()).Error(); err != nil {
				return fmt.Errorf("delete from the cache: %w", err)
			}
		}

		cursor = entry.Cursor
		if cursor == 0 {
			return nil
		}
	}
}

// Close releases the connections.
func (v *Valkey) Close() error {
	v.client.Close()
	return nil
}

// Ping says whether the cache is reachable, so a server can refuse to start
// pointing at one that is not rather than discovering it on the first request.
func (v *Valkey) Ping(ctx context.Context) error {
	if err := v.client.Do(ctx, v.client.B().Ping().Build()).Error(); err != nil {
		return fmt.Errorf("the cache did not answer: %w", err)
	}
	return nil
}

// scanBatch is how many keys a single SCAN asks for. A hint rather than a
// guarantee; large enough that a prefix of a few hundred takes one or two
// rounds, small enough not to block.
const scanBatch = 256

/*
escapeGlob makes a prefix literal.

SCAN's MATCH is a glob, so a key namespace containing one of its metacharacters
would match more than intended — and these prefixes carry user identifiers,
which is somebody else's choice of characters.
*/
func escapeGlob(prefix string) string {
	var escaped strings.Builder
	for _, character := range prefix {
		switch character {
		case '*', '?', '[', ']', '\\':
			escaped.WriteRune('\\')
		}
		escaped.WriteRune(character)
	}
	return escaped.String()
}
