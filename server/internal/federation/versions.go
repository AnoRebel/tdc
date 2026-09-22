package federation

import (
	"errors"
	"fmt"
	"strings"
)

/*
Which protocol two servers agree to speak.

A version list rather than a single number, because federation's whole premise
is servers upgrading independently. A server that only ever announced its
current version would force every peer to upgrade in lockstep, which is the
thing a federated network exists not to require.

Negotiation is: take the newest version both sides accept. If there is none,
refuse and say so — the spec's requirement is that this failure is legible, not
that it is impossible. Two servers too far apart in age genuinely cannot talk,
and the useful behaviour is telling the operator that in those words rather
than failing somewhere deeper with a parse error.
*/

// Version1 is the protocol this server speaks today.
const Version1 = "tdc/1"

// SupportedVersions are the versions this server accepts, newest first.
func SupportedVersions() []string { return []string{Version1} }

// ErrNoCommonVersion means two servers share no protocol version.
var ErrNoCommonVersion = errors.New("no protocol version in common")

/*
Negotiate picks the version two servers will use.

Ours is ordered newest first and that order is what decides: the newest version
we accept that they also accept. Preferring our order over theirs is arbitrary
but it must be one or the other, and using the caller's own preference makes
the outcome depend on which side asked — the same pair of servers would
negotiate differently in each direction.
*/
func Negotiate(ours, theirs []string) (string, error) {
	accepted := make(map[string]struct{}, len(theirs))
	for _, version := range theirs {
		if trimmed := strings.TrimSpace(version); trimmed != "" {
			accepted[trimmed] = struct{}{}
		}
	}

	for _, version := range ours {
		trimmed := strings.TrimSpace(version)
		if trimmed == "" {
			continue
		}
		if _, shared := accepted[trimmed]; shared {
			return trimmed, nil
		}
	}

	return "", fmt.Errorf("%w: this server speaks %s, the peer speaks %s",
		ErrNoCommonVersion,
		strings.Join(ours, ", "),
		strings.Join(theirs, ", "))
}

/*
NegotiateWith picks the version to use with a peer whose document we hold.

The common case, kept as its own function so a caller cannot accidentally pass
the two lists in the wrong order — an error that would silently prefer the
peer's ordering over ours.
*/
func (d Document) NegotiateWith() (string, error) {
	return Negotiate(SupportedVersions(), d.Versions)
}
