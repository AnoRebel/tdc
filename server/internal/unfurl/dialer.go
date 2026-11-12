// Package unfurl fetches a summary of a link that someone posted.
//
// Every request here goes to an address chosen by whoever wrote the message,
// which makes this the most dangerous outbound path in the server. A link to
// http://169.254.169.254/ reaches a cloud provider's metadata service and its
// credentials; a link to http://127.0.0.1:5432/ reaches the database. The
// server must refuse those while still fetching ordinary public pages.
package unfurl

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"

	"go4.org/netipx"
)

// blockedRanges are the address ranges a link must never reach.
//
// Built from the IANA special-purpose registries rather than invented, and kept
// as a set rather than a chain of conditions so that adding a range is a
// one-line change and lookup stays constant-time.
//
// net/netip classifies loopback, private, link-local, multicast and unspecified
// addresses on its own, and those are checked separately; what is listed here is
// what it does not classify — including 100.64.0.0/10, which is where carrier
// -grade NAT puts real infrastructure on many mobile networks.
var blockedRanges = buildBlockedRanges()

func buildBlockedRanges() *netipx.IPSet {
	var builder netipx.IPSetBuilder

	for _, prefix := range []string{
		// IPv4 special-purpose ranges that netip does not classify.
		"0.0.0.0/8",       // "this network"
		"100.64.0.0/10",   // carrier-grade NAT — real hosts live here
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"192.31.196.0/24", // AS112-v4
		"192.52.193.0/24", // AMT
		"192.88.99.0/24",  // deprecated 6to4 relay anycast
		"192.175.48.0/24", // direct delegation AS112
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved, including the broadcast address
		// IPv6 special-purpose ranges.
		"::/128",         // unspecified
		"64:ff9b::/96",   // NAT64 — translates to arbitrary IPv4
		"64:ff9b:1::/48", // local-use NAT64
		"100::/64",       // discard-only
		"2001::/23",      // IETF protocol assignments
		"2001:db8::/32",  // documentation
		"2002::/16",      // 6to4
		"fc00::/7",       // unique local
	} {
		builder.AddPrefix(netip.MustParsePrefix(prefix))
	}

	set, err := builder.IPSet()
	if err != nil {
		// The prefixes above are constants; a failure here is a programming
		// error rather than anything an operator can cause.
		panic(fmt.Sprintf("unfurl: building the blocked ranges: %v", err))
	}
	return set
}

// addressAllowed reports whether an address may be connected to.
func addressAllowed(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}

	// An IPv4 address written as IPv6 must be judged as the IPv4 address it is,
	// or ::ffff:127.0.0.1 would slip past every IPv4 rule below.
	address = address.Unmap()

	switch {
	case address.IsLoopback(),
		address.IsPrivate(),
		address.IsLinkLocalUnicast(),
		address.IsLinkLocalMulticast(),
		address.IsMulticast(),
		address.IsUnspecified(),
		address.IsInterfaceLocalMulticast():
		return false
	}

	return !blockedRanges.Contains(address)
}

/*
safeControl refuses a connection to a forbidden address at connect time.

This is the whole defence, and where it sits matters more than what it checks.
The obvious approach — resolve the hostname, check the address, then dial the
hostname — loses to a DNS rebinding race: the name resolves to a public address
for the check and to 127.0.0.1 for the connection a moment later. Rocket.Chat
shipped exactly that and was exploited for it.

net.Dialer.Control runs after the address is resolved and immediately before
connect(2), on the address that will actually be used. There is no window
between the check and the connection for anything to change.
*/
func safeControl(network, address string, _ syscall.RawConn) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return fmt.Errorf("unfurl: refusing to dial over %q", network)
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("unfurl: cannot read the address %q: %w", address, err)
	}

	parsed, err := netip.ParseAddr(host)
	if err != nil {
		// Control is called with a literal address; anything else means the
		// resolution did not happen as expected, and refusing is the safe
		// answer.
		return fmt.Errorf("unfurl: %q is not an address", host)
	}

	if !addressAllowed(parsed) {
		return fmt.Errorf("unfurl: refusing to reach %s, which is not a public address", parsed)
	}

	return nil
}

// safeDialer returns a dialer that will not reach private infrastructure.
func safeDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: safeControl,
	}
}

// AddressAllowed exposes the address check for tests and for callers that need
// to make the same judgement without dialling.
func AddressAllowed(address netip.Addr) bool { return addressAllowed(address) }

// DialContext dials only public addresses, for callers that want the dialer
// rather than the whole client.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return safeDialer(10*time.Second).DialContext(ctx, network, address)
}
