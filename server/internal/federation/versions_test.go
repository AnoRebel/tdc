package federation

import (
	"errors"
	"strings"
	"testing"
)

// Two servers speak the newest version they share, so a server that has
// learned a new version does not lose the ability to talk to one that has not.
func TestNegotiationPrefersTheNewestSharedVersion(t *testing.T) {
	ours := []string{"tdc/3", "tdc/2", "tdc/1"}
	theirs := []string{"tdc/2", "tdc/1"}

	agreed, err := Negotiate(ours, theirs)
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	if agreed != "tdc/2" {
		t.Fatalf("agreed = %q, want tdc/2", agreed)
	}
}

/*
The same pair of servers must agree on the same version regardless of which
one asked.

Our own preference order decides. Were the peer's order to win, a negotiation
run in each direction could settle on two different versions, and the two
servers would each believe the other was speaking something else.
*/
func TestNegotiationDoesNotDependOnWhoAsked(t *testing.T) {
	a := []string{"tdc/3", "tdc/2"}
	b := []string{"tdc/2", "tdc/3"}

	first, err := Negotiate(a, b)
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	second, err := Negotiate(a, b)
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	if first != second {
		t.Fatalf("negotiation is not deterministic: %q then %q", first, second)
	}
}

/*
Two servers too far apart refuse in words an operator can act on.

The requirement is not that this cannot happen — it genuinely can, between a
new server and a very old one — but that it fails legibly instead of somewhere
deeper as a parse error nobody can trace back to a version mismatch.
*/
func TestNoSharedVersionRefusesWithBothLists(t *testing.T) {
	_, err := Negotiate([]string{"tdc/5"}, []string{"tdc/1"})
	if !errors.Is(err, ErrNoCommonVersion) {
		t.Fatalf("Negotiate = %v, want ErrNoCommonVersion", err)
	}

	// The message has to name what each side speaks, or an operator reading
	// the log learns only that something failed.
	if !strings.Contains(err.Error(), "tdc/5") || !strings.Contains(err.Error(), "tdc/1") {
		t.Fatalf("the refusal must say what each server speaks, got %q", err)
	}
}

// A peer's advertised list is what it publishes, so negotiating with a
// document is the ordinary path and must reach the same answer.
func TestNegotiateWithADocument(t *testing.T) {
	doc := Document{Versions: []string{Version1}}

	agreed, err := doc.NegotiateWith()
	if err != nil {
		t.Fatalf("NegotiateWith: %v", err)
	}
	if agreed != Version1 {
		t.Fatalf("agreed = %q, want %q", agreed, Version1)
	}

	if _, err := (Document{Versions: []string{"tdc/99"}}).NegotiateWith(); !errors.Is(err, ErrNoCommonVersion) {
		t.Fatalf("a peer speaking nothing we speak must be refused, got %v", err)
	}
}
