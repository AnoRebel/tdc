package federation

import (
	"fmt"
	"strings"
)

/*
Which peers this server will deal with.

One decision, consulted from two directions. A block that stopped incoming
requests but not outgoing ones would be a block in name only — this server
would still be reaching out to a peer its operator had refused — so Allows is
called before verifying an arriving request and before dialing a departing one,
and both get the same answer from the same lists.

Blocking is checked first and under every mode, including "open" and
"allowlist". An operator adding a server to the blocklist is saying "not this
one", and it would be a trap for that to quietly stop meaning anything because
the mode was later changed.
*/

// Mode is how this server decides which peers it will deal with.
type Mode string

const (
	// ModeOpen deals with any server that can prove who it is.
	ModeOpen Mode = "open"
	// ModeAllowlist deals only with servers an operator has listed.
	ModeAllowlist Mode = "allowlist"
	// ModeBlocklist deals with any server except those an operator listed.
	ModeBlocklist Mode = "blocklist"
)

// ParseMode reads a configured mode, defaulting to open on anything
// unrecognised — a server whose mode was misspelled should behave like one
// nobody configured rather than refusing every peer for an invisible reason.
func ParseMode(value string) Mode {
	switch Mode(strings.TrimSpace(value)) {
	case ModeAllowlist:
		return ModeAllowlist
	case ModeBlocklist:
		return ModeBlocklist
	default:
		return ModeOpen
	}
}

/*
Trust is an operator's answer to "which peers?", assembled from settings.

Held as a value rather than read from settings at each check, so that one
request cannot see the mode from before an operator's change and the lists
from after it.
*/
type Trust struct {
	Mode    Mode
	Allowed []string
	Blocked []string
}

/*
Allows reports whether this server will deal with a peer, and why not.

The reason is returned rather than logged here because the two callers want it
differently: an arriving request is refused with it, a departing one is
abandoned with it, and neither wants the other's phrasing.
*/
func (t Trust) Allows(peer string) (bool, error) {
	host, err := Host(peer)
	if err != nil {
		return false, fmt.Errorf("peer %q is not an address: %w", peer, err)
	}

	// Blocking wins over everything, in every mode.
	if listed(t.Blocked, host) {
		return false, fmt.Errorf("%s is blocked by this server", host)
	}

	if t.Mode == ModeAllowlist && !listed(t.Allowed, host) {
		return false, fmt.Errorf("%s is not on this server's allowlist", host)
	}

	return true, nil
}

/*
listed reports whether a host appears in a configured list.

Compared as hosts rather than as strings, so that an operator who listed
"https://peer.example.org/" and a peer identifying itself as
"peer.example.org:8443" are understood to mean the same server. An operator
should not have to guess the spelling a peer will arrive with.
*/
func listed(list []string, host string) bool {
	for _, entry := range list {
		listedHost, err := Host(entry)
		if err != nil {
			continue
		}
		if strings.EqualFold(listedHost, host) {
			return true
		}
	}
	return false
}

// SplitList reads a comma-separated setting into entries, discarding blanks so
// that a trailing comma or a stray space is not read as a server named "".
func SplitList(value string) []string {
	var entries []string
	for _, entry := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}
