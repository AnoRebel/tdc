package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

/*
Deciding which servers this client will talk to.

The rule is one sentence: a server reached over plain HTTP must be on this
machine. Everything else here is about saying so in words somebody can act on,
because a refusal that only says "invalid" leaves somebody to guess whether
they mistyped the name or asked for something this will not do.

Why the rule exists: the session cookie and everything in every message travel
over this connection. On a network somebody else can see — an office, a café, a
conference — plain HTTP hands all of it to anybody on the same wire. Loopback
is exempt because there is no wire: the packets never leave the machine, and
refusing it would make running a server locally impossible without a
certificate.
*/

// ErrInsecure is returned for an address this client will not use.
var ErrInsecure = errors.New("insecure address")

/*
Address is a server address that has been checked.

A distinct type rather than a string, so a caller cannot pass an unchecked one
by accident: the only way to make one is through ParseAddress, which is where
the refusals live.
*/
type Address struct {
	// URL is the origin, with no trailing slash: "https://tdc.or.tz".
	URL string
	// Loopback records that this is a server on this machine, which is what
	// makes an unencrypted one acceptable.
	Loopback bool
}

/*
ParseAddress reads what somebody typed and says whether it can be used.

Deliberately forgiving about form and strict about substance. Somebody typing
the name of their server has not mistyped it by leaving off the scheme, so a
bare host is read as HTTPS — the safe reading, and the one that is right nearly
always. What it will not do is quietly upgrade an address somebody explicitly
wrote as http:// on a network where that is unsafe, because silently changing
what somebody asked for is how people end up trusting the wrong thing.
*/
func ParseAddress(typed string) (Address, error) {
	text := strings.TrimSpace(typed)
	if text == "" {
		return Address{}, fmt.Errorf("%w: no address was given", ErrInsecure)
	}

	/*
		A bare host gets https://. Without this, url.Parse reads "tdc.or.tz"
		as a path with no host at all, and the refusal would be about a
		malformed address rather than about what somebody meant.
	*/
	if !strings.Contains(text, "://") {
		text = "https://" + text
	}

	parsed, err := url.Parse(text)
	if err != nil {
		return Address{}, fmt.Errorf("%w: that is not an address this can read", ErrInsecure)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return Address{}, fmt.Errorf(
			"%w: a server address begins with https://, not %s://", ErrInsecure, scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return Address{}, fmt.Errorf("%w: that address names no server", ErrInsecure)
	}

	loopback := isLoopback(host)

	/*
		The rule, and the only refusal that is about safety rather than form.

		The message names the two ways forward — a certificate, or a server on
		this machine — because somebody meeting this is usually running their
		own server and has not yet arranged one.
	*/
	if scheme == "http" && !loopback {
		return Address{}, fmt.Errorf(
			"%w: %s would send your password and messages unencrypted, where anybody on "+
				"the same network could read them. Use https://, or run the server on this "+
				"machine", ErrInsecure, parsed.Host)
	}

	/*
		Only the origin is kept. A path, a query or a fragment would be
		appended to every request path this client builds, producing addresses
		nobody meant — and credentials in the URL would be stored with the
		server rather than in the keychain.
	*/
	origin := url.URL{Scheme: scheme, Host: parsed.Host}

	return Address{URL: strings.TrimSuffix(origin.String(), "/"), Loopback: loopback}, nil
}

/*
isLoopback reports whether this host is on the machine this is running on.

Both the literal addresses and the name. "localhost" is resolved by convention
rather than by DNS on every system this runs on, and refusing it while
accepting 127.0.0.1 would refuse the form nearly everybody types.
*/
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") ||
		strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}

	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		// A name rather than a literal. Resolving it would make this depend on
		// DNS at the moment somebody types an address, and a name that
		// resolves to loopback today may not tomorrow — so anything but the
		// conventional names is treated as remote, which is the safe reading.
		return false
	}

	/*
		Unmapped first, so an IPv4 address written as IPv6 is judged as the
		IPv4 address it is.

		Go's IsLoopback already unmaps internally, so this changes nothing
		today — it is here because the same parse is used to decide other
		properties as this grows, and a check that depends on the standard
		library to keep doing that for it is one that breaks quietly.
	*/
	return address.Unmap().IsLoopback()
}

/*
Reachable reports whether anything is listening, without signing in.

Somebody who has mistyped an address should be told that rather than being sent
to a sign-in form that will fail for a reason it cannot explain. This asks the
server what it is, which is the one route every TDC server answers without a
session.
*/
func (s *Service) Reachable(address string) error {
	checked, err := ParseAddress(address)
	if err != nil {
		return err
	}

	/*
		A client of its own, with no cookie jar.

		Checking whether a server is there must not touch any session: this is
		called for an address somebody has just typed, which may be one they
		are not signed in to, or one they typed by mistake.
	*/
	probe := &http.Client{Timeout: 15 * time.Second}

	response, err := probe.Get(checked.URL + "/api/server")
	if err != nil {
		return fmt.Errorf("nothing answered at %s", checked.URL)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		return fmt.Errorf("%s answered, but not like a TDC server", checked.URL)
	}

	return nil
}
