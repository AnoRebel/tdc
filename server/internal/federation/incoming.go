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
  3. Does it belong to the room it names, on the server that owns that room?

The first is cryptography and the other two are not, which is the point: a
perfectly valid signature over a lie about whose message this is would pass
check one and must still be refused.
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
	senderServer, err := ServerOf(event.Sender)
	if err != nil {
		return err
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

	/*
		And the origin must be the room's home server.

		Only the home server assigns sequences and decides membership, so an
		event about this room signed by any other server is one nobody was
		entitled to make — a participating peer relaying its own idea of the
		room's contents rather than passing on the home server's.
	*/
	if !strings.EqualFold(strings.ToLower(event.Origin), strings.ToLower(homeServer)) {
		return fmt.Errorf("%w: %s is not %s", ErrNotHomeServer, event.Origin, homeServer)
	}

	return nil
}
