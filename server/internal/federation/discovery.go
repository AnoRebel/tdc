package federation

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
What a server publishes about itself, and how a peer checks it.

Federation starts with a question that has to be answered before any content
moves: is the server at this address really the server it says it is? The
answer here is deliberately the same one DNS and TLS already give. A server
claiming to be example.org is example.org only if
https://example.org/.well-known/tdc-federation says so — the claim is checked
against the domain it names, not against the peer's own word for itself over a
connection that peer already controls.

A static document rather than a handshake: there is no session to establish, a
GET caches trivially, and a peer that only wants to verify one signature should
not have to negotiate anything to do it.
*/

// WellKnownPath is where every TDC server publishes its identity.
const WellKnownPath = "/.well-known/tdc-federation"

/*
Document is what a server publishes at WellKnownPath.

The published keys include retired ones, because a peer that dropped a retired
key would stop being able to verify everything that key ever signed — history
becoming unverifiable is not an acceptable cost of rotating.
*/
type Document struct {
	// CanonicalAddress is how this server is known, as its operator set it.
	// Never inferred from a request, so it stays correct behind a proxy.
	CanonicalAddress string `json:"canonicalAddress"`
	// Keys are the current and retired public keys, as the event log
	// publishes them.
	Keys []events.PublishedKey `json:"keys"`
	// Versions are the protocol versions this server accepts, newest first.
	Versions []string `json:"versions"`
	// Enabled says whether this server will actually federate. A server with
	// federation off still answers here — saying "not participating" is a
	// usable answer, where a connection timeout is not.
	Enabled bool `json:"enabled"`
}

var (
	// ErrNoAddress means a document named no server.
	ErrNoAddress = errors.New("discovery document has no canonical address")
	// ErrNoKeys means a document published nothing to verify against.
	ErrNoKeys = errors.New("discovery document publishes no keys")
	// ErrNoVersions means a document advertised no protocol version.
	ErrNoVersions = errors.New("discovery document advertises no protocol version")
	// ErrDomainMismatch means a document claimed a server other than the one
	// serving it. This is the impersonation check, and it is fatal.
	ErrDomainMismatch = errors.New("discovery document claims a domain it is not served from")
)

/*
Valid reports whether a document is well formed, without regard to where it
came from.

Structural only. Whether the document may be believed is DomainMatches, which
is a separate question deliberately: a perfectly well formed document served
from the wrong place is exactly what an impersonation looks like.
*/
func (d Document) Valid() error {
	if strings.TrimSpace(d.CanonicalAddress) == "" {
		return ErrNoAddress
	}
	if len(d.Keys) == 0 {
		return ErrNoKeys
	}
	if len(d.Versions) == 0 {
		return ErrNoVersions
	}
	return nil
}

/*
Keyring builds a verify-only keyring from what a peer published.

This is the whole receiving side of content verification: the same
events.Keyring.Verify that checks a local event checks a foreign one, given a
different set of keys. Verification logic that differed between local and
foreign events would be two implementations that could disagree about whether
the same bytes were signed.
*/
func (d Document) Keyring() (*events.Keyring, error) {
	return events.KeyringFromPublished(d.Keys)
}

/*
Host reduces an address to the host a document must be served from.

Accepts what operators actually write — "example.org", "https://example.org",
"https://example.org/" — because a configuration value that has to be spelled
one exact way is a configuration value that will be spelled another.
*/
func Host(address string) (string, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return "", ErrNoAddress
	}

	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNoAddress, address)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("%w: %s", ErrNoAddress, address)
	}

	// Lowered because a domain is not case sensitive, and an operator who
	// capitalised one means the same server.
	return strings.ToLower(host), nil
}

/*
DomainMatches reports whether a document may be believed, given the domain it
was actually fetched from.

The check the spec requires, and the reason discovery is a fetch rather than an
assertion: a server may claim any identity it likes in the body it serves, and
the only thing that makes the claim worth anything is that the body came from
the domain being claimed.

Ports are deliberately not compared. A server reached on a non-default port is
still that domain, and requiring the port to match would break every
development and self-hosted deployment that runs behind one.
*/
func (d Document) DomainMatches(fetchedFrom string) error {
	claimed, err := Host(d.CanonicalAddress)
	if err != nil {
		return err
	}
	actual, err := Host(fetchedFrom)
	if err != nil {
		return err
	}

	if claimed != actual {
		return fmt.Errorf("%w: claims %q, served from %q", ErrDomainMismatch, claimed, actual)
	}
	return nil
}

// Parse reads a discovery document and checks it is structurally sound.
func Parse(body []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return Document{}, fmt.Errorf("discovery document is not readable: %w", err)
	}
	if err := doc.Valid(); err != nil {
		return Document{}, err
	}
	return doc, nil
}
