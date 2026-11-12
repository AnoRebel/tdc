package federation

import (
	"strings"
	"testing"
)

/*
Open deals with anybody who can prove who they are.

The default once federation is enabled, so that a feature nobody configured
behaves predictably rather than silently refusing every peer.
*/
func TestOpenModeAdmitsAnyPeer(t *testing.T) {
	trust := Trust{Mode: ModeOpen}

	if allowed, why := trust.Allows("https://anyone.example.org"); !allowed {
		t.Fatalf("open mode refused a peer: %v", why)
	}
}

// An allowlist admits the listed and refuses everybody else.
func TestAllowlistAdmitsOnlyTheListed(t *testing.T) {
	trust := Trust{
		Mode:    ModeAllowlist,
		Allowed: []string{"friend.example.org"},
	}

	if allowed, why := trust.Allows("https://friend.example.org"); !allowed {
		t.Fatalf("a listed peer was refused: %v", why)
	}

	allowed, why := trust.Allows("https://stranger.example.org")
	if allowed {
		t.Fatal("an unlisted peer was admitted under allowlist mode")
	}
	if !strings.Contains(why.Error(), "allowlist") {
		t.Errorf("the refusal should say why, got %q", why)
	}
}

// A blocklist refuses the listed and admits everybody else.
func TestBlocklistRefusesOnlyTheListed(t *testing.T) {
	trust := Trust{
		Mode:    ModeBlocklist,
		Blocked: []string{"nuisance.example.org"},
	}

	if allowed, why := trust.Allows("https://anyone.example.org"); !allowed {
		t.Fatalf("an unlisted peer was refused under blocklist mode: %v", why)
	}

	allowed, why := trust.Allows("https://nuisance.example.org")
	if allowed {
		t.Fatal("a blocked peer was admitted")
	}
	if !strings.Contains(why.Error(), "blocked") {
		t.Errorf("the refusal should say why, got %q", why)
	}
}

/*
A block holds under every mode, including open and allowlist.

An operator adding a server to the blocklist is saying "not this one". It
would be a trap for that to quietly stop meaning anything because the mode was
later changed to open — the operator would believe a peer was refused while it
was being admitted.
*/
func TestABlockHoldsUnderEveryMode(t *testing.T) {
	for _, mode := range []Mode{ModeOpen, ModeAllowlist, ModeBlocklist} {
		t.Run(string(mode), func(t *testing.T) {
			trust := Trust{
				Mode:    mode,
				Allowed: []string{"nuisance.example.org"}, // listed as allowed, too
				Blocked: []string{"nuisance.example.org"},
			}

			if allowed, _ := trust.Allows("https://nuisance.example.org"); allowed {
				t.Fatalf("a blocked peer was admitted under %s mode", mode)
			}
		})
	}
}

/*
An operator should not have to guess the spelling a peer will arrive with.

A list written as a URL and a peer identifying itself with a port are the same
server, and a comparison that missed that would leave an operator believing
they had blocked somebody they had not.
*/
func TestListsMatchHoweverTheOperatorSpelledThem(t *testing.T) {
	trust := Trust{
		Mode:    ModeBlocklist,
		Blocked: []string{"https://Nuisance.example.org/", " other.example.org "},
	}

	for _, spelling := range []string{
		"https://nuisance.example.org",
		"nuisance.example.org",
		"https://nuisance.example.org:8443",
		"http://NUISANCE.example.org",
	} {
		if allowed, _ := trust.Allows(spelling); allowed {
			t.Errorf("%q was admitted despite being blocked", spelling)
		}
	}

	// And the second entry, to prove the list is read as a list rather than
	// only its first element.
	if allowed, _ := trust.Allows("https://other.example.org"); allowed {
		t.Error("the second blocked entry was not honoured")
	}
}

// A stray comma must not be read as a server named "", which would otherwise
// sit in the list matching nothing and quietly confusing an operator.
func TestBlankEntriesAreDiscarded(t *testing.T) {
	entries := SplitList(" friend.example.org , , other.example.org,")

	if len(entries) != 2 {
		t.Fatalf("SplitList gave %d entries (%v), want 2", len(entries), entries)
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "" {
			t.Error("a blank entry survived")
		}
	}
}

/*
A misspelled mode behaves like an unconfigured one.

A server whose mode setting was fat-fingered should not refuse every peer for
a reason nobody can see; the visible failure is the better one.
*/
func TestAnUnrecognisedModeIsOpen(t *testing.T) {
	for _, value := range []string{"", "allow-list", "ALLOWLIST typo", "nonsense"} {
		if mode := ParseMode(value); mode != ModeOpen {
			t.Errorf("ParseMode(%q) = %q, want open", value, mode)
		}
	}

	// But the real names are still read correctly, or the above would be
	// satisfied by a function that always returned open.
	if ParseMode("allowlist") != ModeAllowlist {
		t.Error("allowlist was not recognised")
	}
	if ParseMode("blocklist") != ModeBlocklist {
		t.Error("blocklist was not recognised")
	}
}

/*
A peer is identified by host, not by host and port.

This follows from a server's identity ignoring the port — tdc.example.org on
:8443 is the same server as on :443 — and it is what an operator means by
blocking a server. Worth pinning, because it also means two servers sharing a
hostname on different ports cannot be told apart by these lists, which is
invisible in production and surprising in a test on loopback.
*/
func TestPeersAreIdentifiedByHostRatherThanPort(t *testing.T) {
	trust := Trust{Mode: ModeBlocklist, Blocked: []string{"peer.example.org"}}

	for _, port := range []string{"", ":443", ":8443", ":9999"} {
		if allowed, _ := trust.Allows("https://peer.example.org" + port); allowed {
			t.Errorf("blocking the host did not block it on port %q", port)
		}
	}

	// And a different host is unaffected, or the above would be satisfied by
	// a list that blocked everything.
	if allowed, _ := trust.Allows("https://other.example.org"); !allowed {
		t.Error("blocking one host blocked another")
	}
}
