package federation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Deciding whether an event that arrived from another server may be believed.

Three questions, and all three must be answered before anything is stored.
Storing first and checking later would mean a forged event existing in the
database for however long the check takes, which is long enough for somebody
to read it.

  1. Did the server it claims to come from actually sign it? — the signature,
     verified against that server's published keys.
  2. Is the sender one that server is entitled to speak for? — a server may
     say anything about its own users and nothing about anybody else's.
  3. Does it belong to the room it names?

The first is cryptography and the other two are not, which is the point: a
perfectly valid signature over a lie about whose message this is would pass
check one and must still be refused.

Whether the server that *delivered* it was entitled to is a separate question,
because the answer differs: an event is written by its author's server and
delivered by the room's home server, and for a remote member's message those
are two different servers. AcceptDelivered asks both.
*/

var (
	// ErrEventUnverified means the signature did not verify against the
	// claimed origin's published keys.
	ErrEventUnverified = errors.New("event signature does not verify")
	// ErrImpersonation means the origin signed for a sender it does not host.
	ErrImpersonation = errors.New("origin server signed for somebody else's user")
	// ErrWrongRoom means the event names a room this delivery is not about.
	ErrWrongRoom = errors.New("event belongs to a different room")
	// ErrNotHomeServer means the origin is not the room's home server.
	ErrNotHomeServer = errors.New("event's origin is not the room's home server")
	// ErrMalformedAddress means an address was not in name@server form.
	ErrMalformedAddress = errors.New("address is not in name@server form")
)

/*
ServerOf returns the server half of a name@server address.

An address with no "@", or with nothing after it, names no server — and
treating that as a server called "" would make an impersonation check pass by
comparing two empty strings.
*/
func ServerOf(address string) (string, error) {
	_, server, found := strings.Cut(address, "@")
	if !found || strings.TrimSpace(server) == "" {
		return "", fmt.Errorf("%w: %q", ErrMalformedAddress, address)
	}
	return strings.ToLower(strings.TrimSpace(server)), nil
}

/*
Accept decides whether an incoming event may be stored.

keyring holds the claimed origin's published keys, fetched through discovery.
roomAddress is the room this delivery is about, and homeServer the server that
owns it — both from the receiving server's own records, never from the event,
since an event asserting which room it belongs to is exactly what must be
checked rather than trusted.
*/
func Accept(event events.Event, keyring *events.Keyring, roomAddress, homeServer string) error {
	/*
		The signature first, because nothing else about the event means
		anything until we know the origin really produced it. Verify is the
		same function that checks a local event — run against a different set
		of keys, which is the whole of what "verify a foreign event" turns out
		to be.
	*/
	if err := keyring.Verify(event); err != nil {
		return fmt.Errorf("%w: %w", ErrEventUnverified, err)
	}

	/*
		A server may speak for its own users and for nobody else's.

		Without this a server could sign a perfectly valid event claiming to
		be from somebody on another server — the cryptography would be
		flawless and the attribution a lie. The check is a comparison of two
		fields that are both inside the signature, so neither can be altered
		after signing.
	*/
	/*
		A sender with no person in it is a server speaking for itself — a
		baseline, the end of a cycle, a change it made at somebody's signed
		request — and is held to the same rule: only that server may sign it.
	*/
	senderServer := strings.ToLower(strings.TrimSpace(event.Sender))
	if strings.Contains(event.Sender, "@") || senderServer == "" {
		var err error
		if senderServer, err = ServerOf(event.Sender); err != nil {
			return err
		}
	}
	if !strings.EqualFold(senderServer, strings.ToLower(event.Origin)) {
		return fmt.Errorf("%w: %s signed for %s", ErrImpersonation, event.Origin, event.Sender)
	}

	// The event must be about the room this delivery concerns, or a peer
	// could use a room it participates in to inject events into one it does
	// not.
	if !strings.EqualFold(event.Room, roomAddress) {
		return fmt.Errorf("%w: event names %q, delivery is for %q",
			ErrWrongRoom, event.Room, roomAddress)
	}

	return nil
}

/*
AcceptDelivered decides whether an event delivered by a peer may be stored.

Accept checks the event itself. This adds the one question that is about the
delivery rather than the content: is the server that handed it to us entitled
to be handing it over at all?

That is the room's home server, and only it — because only it assigns
sequences. An event arriving from anybody else is a participating peer relaying
its own idea of the room's contents rather than passing on the home server's,
which is where two servers start to disagree about the order.

Note what this deliberately does not require: that the event was *written* by
the home server. A member on another server posts through their own server,
which signs the event and forwards it; the home server orders it and passes it
on with that signature intact. So a relayed event's origin is the author's
server while its deliverer is the home server, and both facts are checked —
the first by Accept, the second here.
*/
func AcceptDelivered(
	event events.Event,
	keyring *events.Keyring,
	roomAddress, homeServer, deliveredBy string,
) error {
	if err := Accept(event, keyring, roomAddress, homeServer); err != nil {
		return err
	}

	/*
		Compared as hosts, because one side is an address and the other an
		identity.

		deliveredBy is whatever the caller had — often a full canonical
		address — while a room's home server is recorded as a host, the way
		every peer is. Comparing them raw refused every legitimate delivery
		whose caller passed an address, which is most of them.
	*/
	deliverer, err := Host(deliveredBy)
	if err != nil {
		return fmt.Errorf("%w: %q is not an address", ErrNotHomeServer, deliveredBy)
	}
	home, err := Host(homeServer)
	if err != nil {
		return fmt.Errorf("%w: room's home server %q is not an address", ErrNotHomeServer, homeServer)
	}

	if !strings.EqualFold(deliverer, home) {
		return fmt.Errorf("%w: delivered by %s, which is not %s",
			ErrNotHomeServer, deliverer, home)
	}
	return nil
}
