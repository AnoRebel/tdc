package federation

import (
	"errors"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/events"
)

func documentFor(address string) Document {
	return Document{
		CanonicalAddress: address,
		Keys:             []events.PublishedKey{{ID: "k1", PublicKey: "irrelevant-here"}},
		Versions:         []string{Version1},
	}
}

/*
The impersonation check, which is the whole point of fetching rather than
asking.

A server may put any address it likes in the document it serves. What makes
the claim worth something is that the document came from the domain it names —
so a document claiming example.org is only believed when example.org served it.
*/
func TestDocumentClaimingAnotherDomainIsRefused(t *testing.T) {
	doc := documentFor("https://example.org")

	if err := doc.DomainMatches("example.org"); err != nil {
		t.Fatalf("a document served from the domain it claims must be accepted: %v", err)
	}

	err := doc.DomainMatches("impostor.example.net")
	if !errors.Is(err, ErrDomainMismatch) {
		t.Fatalf("a document served from elsewhere must be refused with ErrDomainMismatch, got %v", err)
	}
}

// An operator writes an address in whichever of several equivalent ways comes
// to hand. All of them name the same server, and a check that accepted only
// one spelling would refuse a correctly configured peer.
func TestAddressSpellingsNameTheSameServer(t *testing.T) {
	spellings := []string{
		"example.org",
		"https://example.org",
		"https://example.org/",
		"https://EXAMPLE.org",
		"https://example.org:8443",
	}

	for _, spelling := range spellings {
		host, err := Host(spelling)
		if err != nil {
			t.Fatalf("Host(%q): %v", spelling, err)
		}
		if host != "example.org" {
			t.Errorf("Host(%q) = %q, want %q", spelling, host, "example.org")
		}
	}
}

/*
A port is not part of the identity.

A server reached on a non-default port is still that domain. Comparing ports
would refuse every development and self-hosted deployment behind one, which is
most of them.
*/
func TestPortDoesNotChangeIdentity(t *testing.T) {
	doc := documentFor("https://example.org:8443")

	if err := doc.DomainMatches("https://example.org"); err != nil {
		t.Fatalf("a differing port must not make a document foreign: %v", err)
	}
}

// A structurally broken document is refused before anything tries to use it,
// so that a missing key list is a stated error rather than a verification that
// silently has nothing to check against.
func TestIncompleteDocumentsAreRefused(t *testing.T) {
	cases := map[string]struct {
		document Document
		want     error
	}{
		"no address":  {Document{Keys: documentFor("x").Keys, Versions: []string{Version1}}, ErrNoAddress},
		"no keys":     {Document{CanonicalAddress: "example.org", Versions: []string{Version1}}, ErrNoKeys},
		"no versions": {Document{CanonicalAddress: "example.org", Keys: documentFor("x").Keys}, ErrNoVersions},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if err := test.document.Valid(); !errors.Is(err, test.want) {
				t.Fatalf("Valid() = %v, want %v", err, test.want)
			}
		})
	}
}

// Parse refuses a body that is not a document at all, rather than yielding an
// empty one that would look like a server publishing nothing.
func TestParseRefusesRubbish(t *testing.T) {
	if _, err := Parse([]byte("this is not json")); err == nil {
		t.Fatal("unreadable body must be an error")
	}
	if _, err := Parse([]byte(`{"canonicalAddress":"example.org"}`)); !errors.Is(err, ErrNoKeys) {
		t.Fatal("a document with no keys must be refused by Parse, not only by Valid")
	}
}

/*
Authority and Host answer different questions, and conflating them breaks
signing on any server not on a default port.

Host is identity: a server on :8443 is the same server as on :443. Authority is
the exact string both sides must reproduce to build the same signature base,
and RFC 9421 requires the non-default port be part of it. Using Host where
Authority belongs verified fine in every test on a default port and failed on
every real deployment behind one.
*/
func TestAuthorityKeepsTheNonDefaultPortThatHostDiscards(t *testing.T) {
	cases := []struct {
		address       string
		wantAuthority string
		wantHost      string
	}{
		{"https://example.org", "example.org", "example.org"},
		{"https://example.org:443", "example.org", "example.org"},
		{"http://example.org:80", "example.org", "example.org"},
		{"https://example.org:8443", "example.org:8443", "example.org"},
		{"http://127.0.0.1:44415", "127.0.0.1:44415", "127.0.0.1"},
		{"https://EXAMPLE.org:8443", "example.org:8443", "example.org"},
	}

	for _, test := range cases {
		t.Run(test.address, func(t *testing.T) {
			authority, err := Authority(test.address)
			if err != nil {
				t.Fatalf("Authority(%q): %v", test.address, err)
			}
			if authority != test.wantAuthority {
				t.Errorf("Authority = %q, want %q", authority, test.wantAuthority)
			}

			host, err := Host(test.address)
			if err != nil {
				t.Fatalf("Host(%q): %v", test.address, err)
			}
			if host != test.wantHost {
				t.Errorf("Host = %q, want %q", host, test.wantHost)
			}
		})
	}
}
