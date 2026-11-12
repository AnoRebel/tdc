package federation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

/*
Fetching and caching what peers publish about themselves.

Every federated exchange needs the other server's keys, and fetching them on
every request would mean a network round trip to verify a signature — so they
are cached with a TTL. The interesting case is not the cache hit, it is the
miss that matters: when a signature arrives bearing a key ID we do not
recognise, that is what a rotation looks like from the outside, and the correct
response is to re-fetch rather than to refuse. A peer that rotated a key must
become verifiable again without waiting out a TTL, and without every peer
polling constantly on the chance that somebody rotated.
*/

const (
	// DefaultTTL is how long a fetched document is reused.
	//
	// An hour: long enough that verification is not a network operation, short
	// enough that a retired key stops being offered within a working day. A
	// rotation does not wait for it — Refresh exists for exactly that.
	DefaultTTL = time.Hour

	// fetchTimeout bounds a single discovery fetch. A peer that is slow to
	// answer must not hold a verification open.
	fetchTimeout = 10 * time.Second

	// maxDocumentBytes caps what will be read from a peer. A discovery
	// document is small; anything large is either broken or hostile, and
	// reading it into memory to find out is the mistake.
	maxDocumentBytes = 256 << 10
)

// ErrFetchFailed means a peer's document could not be retrieved.
var ErrFetchFailed = errors.New("could not fetch discovery document")

// cached is one peer's document and when it stops being trusted.
type cached struct {
	document Document
	expires  time.Time
}

/*
Client fetches peers' discovery documents and remembers them.

Bounded by the number of peers an operator has configured, which is the only
thing that ever populates it — this cache cannot be grown by a stranger, since
nothing fetches a document for a server nobody asked about.
*/
type Client struct {
	http *http.Client
	ttl  time.Duration
	now  func() time.Time

	mu        sync.RWMutex
	documents map[string]cached
}

/*
NewClient builds a discovery client.

allowPrivate decides whether peers on private networks may be reached. False is
the default everywhere and matches the connect-time guard on every other
outbound request; an operator federating servers inside one network sets it
true, having decided that deliberately.
*/
func NewClient(allowPrivate bool) *Client {
	return &Client{
		http:      newPeerHTTPClient(allowPrivate),
		ttl:       DefaultTTL,
		now:       time.Now,
		documents: make(map[string]cached),
	}
}

/*
newPeerHTTPClient builds the HTTP client used to reach peers.

One constructor for discovery and for delivery, so the two cannot end up with
different answers to "may this server reach a private address" — a drift that
would let a peer be discoverable but undeliverable, or the reverse.

Redirects are never followed: a redirect would let a server answer for a domain
it does not serve, which is the claim the domain check exists to refuse.
*/
func newPeerHTTPClient(allowPrivate bool) *http.Client {
	dial := unfurl.DialContext
	if allowPrivate {
		dial = (&net.Dialer{Timeout: fetchTimeout}).DialContext
	}

	return &http.Client{
		Timeout:   fetchTimeout,
		Transport: &http.Transport{DialContext: dial},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

/*
newPeerFileClient is newPeerHTTPClient for a file's bytes.

No limit on the whole exchange, because a hundred megabytes over a slow link
takes as long as it takes; instead the peer must connect and begin answering
promptly, and must keep sending — a peer that stalls mid-file is cut off by the
idle bound rather than holding a copy open for ever.
*/
func newPeerFileClient(allowPrivate bool) *http.Client {
	dial := unfurl.DialContext
	if allowPrivate {
		dial = (&net.Dialer{Timeout: fetchTimeout}).DialContext
	}

	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dial,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       time.Minute,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// AddressAllowed reports whether an address may be reached, without dialling.
// Exposed so a caller can refuse a peer at configuration time rather than at
// the first delivery.
func AddressAllowed(address netip.Addr) bool { return unfurl.AddressAllowed(address) }

/*
Discover returns a peer's document, from cache when it is still fresh.

The domain check runs on every fetch, never on a cache read: a document that
was verified when it was stored does not become unverified by being read again,
and re-deriving the same answer from the same bytes would only be a chance to
get it wrong twice.
*/
func (c *Client) Discover(ctx context.Context, address string) (Document, error) {
	host, err := Host(address)
	if err != nil {
		return Document{}, err
	}

	c.mu.RLock()
	entry, found := c.documents[host]
	c.mu.RUnlock()
	if found && c.now().Before(entry.expires) {
		return entry.document, nil
	}

	return c.Refresh(ctx, address)
}

/*
Refresh fetches a peer's document, ignoring anything cached.

What a caller does on seeing a key ID it does not recognise. That is not an
error to report: it is what a rotation looks like from the far side, and the
answer is to go and look rather than to refuse a peer that did the right thing.
*/
func (c *Client) Refresh(ctx context.Context, address string) (Document, error) {
	host, err := Host(address)
	if err != nil {
		return Document{}, err
	}

	document, err := c.fetch(ctx, address)
	if err != nil {
		return Document{}, err
	}

	// Checked against the host we actually reached, not the address the
	// document names for itself. A document may claim anything; what makes the
	// claim mean something is that it came from the domain being claimed.
	if err := document.DomainMatches(host); err != nil {
		return Document{}, err
	}

	c.mu.Lock()
	c.documents[host] = cached{document: document, expires: c.now().Add(c.ttl)}
	c.mu.Unlock()

	return document, nil
}

// Forget drops a peer from the cache, so the next lookup fetches afresh.
func (c *Client) Forget(address string) {
	host, err := Host(address)
	if err != nil {
		return
	}
	c.mu.Lock()
	delete(c.documents, host)
	c.mu.Unlock()
}

func (c *Client) fetch(ctx context.Context, address string) (Document, error) {
	endpoint, err := wellKnownURL(address)
	if err != nil {
		return Document{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrFetchFailed, err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return Document{}, fmt.Errorf("%w from %s: %w", ErrFetchFailed, address, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Document{}, fmt.Errorf("%w from %s: status %d", ErrFetchFailed, address, response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxDocumentBytes))
	if err != nil {
		return Document{}, fmt.Errorf("%w from %s: %w", ErrFetchFailed, address, err)
	}

	return Parse(body)
}

// wellKnownURL builds the discovery address for a server, defaulting to HTTPS
// for an operator who wrote a bare domain.
func wellKnownURL(address string) (string, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return "", ErrNoAddress
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/") + WellKnownPath, nil
}

/*
Keys resolves a server's published keys, for verifying what it wrote.

The same discovery and cache as everything else, exposed as the narrow thing a
caller verifying a batch of events actually needs: once remote members post,
one delivery can carry events written by several servers, and each must be
checked against its own author's keys.
*/
func (c *Client) Keys(ctx context.Context, server string) (*events.Keyring, error) {
	document, err := c.Discover(ctx, server)
	if err != nil {
		return nil, err
	}
	return document.Keyring()
}
