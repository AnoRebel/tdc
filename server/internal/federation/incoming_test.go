package federation_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
)

// homeServerKeyring is the key a room's home server signs its events with,
// plus the verify-only keyring a peer would build from what it publishes.
func homeServerKeyring(t *testing.T) (*events.Keyring, *events.Keyring) {
	t.Helper()

	key, err := events.NewKeyPair("home-key")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	signing := events.NewKeyring(key)

	verifying, err := events.KeyringFromPublished(signing.Published())
	if err != nil {
		t.Fatalf("KeyringFromPublished: %v", err)
	}
	return signing, verifying
}

// signedEvent builds an event as a home server would, signed and sequenced.
func signedEvent(t *testing.T, keyring *events.Keyring, event events.Event) events.Event {
	t.Helper()

	if event.Nonce == "" {
		nonce, err := events.NewNonce()
		if err != nil {
			t.Fatalf("NewNonce: %v", err)
		}
		event.Nonce = nonce
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = events.Now()
	}
	if err := keyring.Sign(&event); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return event
}

func messageFrom(sender, origin, room string) events.Event {
	return events.Event{
		Room:      room,
		Sender:    sender,
		Origin:    origin,
		Type:      events.TypeMessageSent,
		Sequence:  7,
		CreatedAt: events.Now(),
		Content:   json.RawMessage(`{"body":"hello from far away"}`),
	}
}

// An event the home server really signed, about the room it really owns, from
// one of its own users, is accepted.
func TestAGenuineForeignEventIsAccepted(t *testing.T) {
	signing, verifying := homeServerKeyring(t)

	event := signedEvent(t, signing,
		messageFrom("amina@a.example.org", "a.example.org", "general@a.example.org"))

	if err := federation.Accept(event, verifying, "general@a.example.org", "a.example.org"); err != nil {
		t.Fatalf("a genuine event was refused: %v", err)
	}
}

/*
A forged signature is refused.

The event is well formed in every other respect: the right room, the right
home server, a sender that server hosts. Only the signature is wrong, which is
the case the cryptography exists for.
*/
func TestAForgedEventIsRefused(t *testing.T) {
	signing, verifying := homeServerKeyring(t)

	event := signedEvent(t, signing,
		messageFrom("amina@a.example.org", "a.example.org", "general@a.example.org"))

	// Alter the content after signing: the signature no longer covers it.
	event.Content = json.RawMessage(`{"body":"something nobody said"}`)

	err := federation.Accept(event, verifying, "general@a.example.org", "a.example.org")
	if !errors.Is(err, federation.ErrEventUnverified) {
		t.Fatalf("Accept = %v, want ErrEventUnverified", err)
	}
}

/*
A server signing for somebody else's user is refused.

The signature here is genuine — the origin really did sign this — and that is
exactly why the check matters. Cryptography proves who produced the bytes, not
that they were entitled to claim what the bytes say.
*/
func TestAServerSigningForAnotherServersUserIsRefused(t *testing.T) {
	signing, verifying := homeServerKeyring(t)

	// a.example.org signs an event claiming to be from somebody on b.
	event := signedEvent(t, signing,
		messageFrom("impostor@b.example.org", "a.example.org", "general@a.example.org"))

	err := federation.Accept(event, verifying, "general@a.example.org", "a.example.org")
	if !errors.Is(err, federation.ErrImpersonation) {
		t.Fatalf("Accept = %v, want ErrImpersonation", err)
	}
}

/*
An event about a different room is refused.

Otherwise a peer participating in one room could use that delivery to inject
events into a room it has nothing to do with.
*/
func TestAnEventForAnotherRoomIsRefused(t *testing.T) {
	signing, verifying := homeServerKeyring(t)

	event := signedEvent(t, signing,
		messageFrom("amina@a.example.org", "a.example.org", "private@a.example.org"))

	err := federation.Accept(event, verifying, "general@a.example.org", "a.example.org")
	if !errors.Is(err, federation.ErrWrongRoom) {
		t.Fatalf("Accept = %v, want ErrWrongRoom", err)
	}
}

/*
A remote member's message is written by their server and passed on by the home
server, and both facts are checked separately.

This is the case that makes authorship and delivery two questions rather than
one. B's user posts into A's room: B signs the event, A orders it and relays
it to C. C must accept it — B really wrote it, A really delivered it — while
still refusing an event A wrote claiming to be from B's user, and refusing one
B tries to deliver directly about A's room.
*/
func TestAuthorshipAndDeliveryAreCheckedSeparately(t *testing.T) {
	bSigning, bVerifying := homeServerKeyring(t)

	// B's user posts into a room A owns. B signs it; A will deliver it.
	event := signedEvent(t, bSigning,
		messageFrom("someone@b.example.org", "b.example.org", "general@a.example.org"))

	// Accept alone is satisfied: B wrote it, honestly, about the right room.
	if err := federation.Accept(event, bVerifying, "general@a.example.org", "a.example.org"); err != nil {
		t.Fatalf("a remote member's message was refused on its own merits: %v", err)
	}

	// Delivered by A, the room's home server: accepted.
	if err := federation.AcceptDelivered(
		event, bVerifying, "general@a.example.org", "a.example.org", "a.example.org",
	); err != nil {
		t.Fatalf("a remote member's message relayed by the home server was refused: %v", err)
	}

	/*
		Delivered by B itself: refused.

		B wrote the event and is entitled to say so, but it is not entitled to
		put it into A's room directly — only A assigns positions there, and a
		peer relaying its own idea of a room's contents is where two servers
		start to disagree about the order.
	*/
	err := federation.AcceptDelivered(
		event, bVerifying, "general@a.example.org", "a.example.org", "b.example.org",
	)
	if !errors.Is(err, federation.ErrNotHomeServer) {
		t.Fatalf("AcceptDelivered = %v, want ErrNotHomeServer", err)
	}
}

/*
The home server cannot invent a message from somebody it does not host.

The point of keeping the author's signature rather than re-signing on relay.
A orders the room and could drop or reorder anything, which is accepted — but
it holds none of B's keys, so a message attributed to B's user that B never
signed fails verification wherever it is checked.
*/
func TestTheHomeServerCannotForgeARemoteMembersMessage(t *testing.T) {
	aSigning, _ := homeServerKeyring(t)
	_, bVerifying := homeServerKeyring(t)

	// A signs an event claiming to be from B's user, and delivers it as the
	// room's home server — every part of the delivery is legitimate.
	forged := signedEvent(t, aSigning,
		messageFrom("someone@b.example.org", "a.example.org", "general@a.example.org"))

	/*
		Checked against B's keys, because B is who the sender belongs to — and
		it is the signature check that refuses this, not the impersonation
		check. Worth stating: A's event is internally consistent (its origin
		really is A) and would pass a comparison of origin against sender's
		server if it were checked against A's keys. What defeats it is asking
		the right keyring, which is the whole reason a batch is verified per
		author rather than per deliverer.
	*/
	err := federation.AcceptDelivered(
		forged, bVerifying, "general@a.example.org", "a.example.org", "a.example.org",
	)
	if !errors.Is(err, federation.ErrEventUnverified) {
		t.Fatalf("AcceptDelivered = %v, want ErrEventUnverified — "+
			"the home server forged a message from a member it does not host", err)
	}
}

/*
And the impersonation check covers what a signature alone cannot.

A server signing for somebody else's user produces an event that verifies
perfectly against its own keys: the cryptography is impeccable and the
attribution is a lie. Only comparing the sender's server against the origin
refuses it, which is why both checks exist rather than one.
*/
func TestAValidSignatureOverAFalseAttributionIsStillRefused(t *testing.T) {
	aSigning, aVerifying := homeServerKeyring(t)

	// A signs, honestly as A, an event claiming to be from B's user.
	event := signedEvent(t, aSigning,
		messageFrom("someone@b.example.org", "a.example.org", "general@a.example.org"))

	// Verified against A's own keys, so the signature is genuinely valid.
	err := federation.Accept(event, aVerifying, "general@a.example.org", "a.example.org")
	if !errors.Is(err, federation.ErrImpersonation) {
		t.Fatalf("Accept = %v, want ErrImpersonation", err)
	}
}

// An address naming no server is refused rather than read as a server called
// "", which would make the impersonation check compare two empty strings.
func TestAnAddressWithNoServerIsRefused(t *testing.T) {
	for _, address := range []string{"amina", "amina@", "amina@   ", ""} {
		if _, err := federation.ServerOf(address); !errors.Is(err, federation.ErrMalformedAddress) {
			t.Errorf("ServerOf(%q) = %v, want ErrMalformedAddress", address, err)
		}
	}

	server, err := federation.ServerOf("amina@A.Example.ORG")
	if err != nil {
		t.Fatalf("ServerOf: %v", err)
	}
	if server != "a.example.org" {
		t.Errorf("ServerOf lowercases the server, got %q", server)
	}
}

/*
An event still verifies after a storage round trip.

Postgres normalises jsonb — keys reordered, whitespace changed — so the content
bytes read back are not the bytes that were signed. The digest captured at
storage time is what keeps verification working; without it an event would
verify on arrival and fail the moment it was read back, on the very server that
had just accepted it.

This reproduces that round trip without a database: hash the content as storage
does, normalise the content the way jsonb would, and verify against the digest
rather than the bytes.
*/
func TestAForeignEventStillVerifiesAfterStorageNormalisesIt(t *testing.T) {
	signing, verifying := homeServerKeyring(t)

	event := signedEvent(t, signing, events.Event{
		Room:      "general@a.example.org",
		Sender:    "amina@a.example.org",
		Origin:    "a.example.org",
		Type:      events.TypeMessageSent,
		Sequence:  7,
		CreatedAt: events.Now(),
		// Deliberately unsorted keys and loose spacing, which is what a
		// sender may legitimately send and what jsonb will not preserve.
		Content: json.RawMessage(`{"body":"hello",  "alpha":1}`),
	})

	// What storage records, computed from the content as it arrived.
	digest := events.HashContent(event.Content)

	// What the database gives back: same values, different bytes.
	normalised := event
	normalised.Content = json.RawMessage(`{"alpha":1,"body":"hello"}`)
	normalised.ContentHash = digest

	if err := federation.Accept(normalised, verifying, "general@a.example.org", "a.example.org"); err != nil {
		t.Fatalf("a stored event stopped verifying after normalisation: %v", err)
	}

	/*
		Key order alone would survive without the digest, because HashContent
		canonicalises the JSON before hashing — so reordering is already
		harmless. What the digest is actually for is content that is *gone*:
		once an event is redacted there is no content left to hash, and only
		the digest recorded at storage time can reproduce what was signed.
	*/
	redacted := normalised
	redacted.Content = json.RawMessage(`{}`)
	if err := federation.Accept(redacted, verifying, "general@a.example.org", "a.example.org"); err != nil {
		t.Fatalf("a redacted event stopped verifying, which the digest exists to prevent: %v", err)
	}

	// And a digest that does not match the content it claims to summarise is
	// refused, or the field would be a place to put anything.
	lying := normalised
	lying.ContentHash = events.HashContent(json.RawMessage(`{"body":"something else"}`))
	if err := federation.Accept(lying, verifying, "general@a.example.org", "a.example.org"); err == nil {
		t.Fatal("an event whose recorded digest does not match its content was accepted")
	}
}

/*
The deliverer is matched as a host, whichever form the caller had.

A room's home server is recorded as a host; the server delivering is often
known by its full canonical address. Comparing those raw refused every
legitimate delivery whose caller happened to hold an address — which was most
of them, and which the unit tests missed because they all passed bare hosts.
*/
func TestTheDelivererIsMatchedAsAHostNotAString(t *testing.T) {
	signing, verifying := homeServerKeyring(t)

	event := signedEvent(t, signing,
		messageFrom("amina@a.example.org", "a.example.org", "general@a.example.org"))

	// Every way a caller might legitimately name the delivering server.
	for _, deliveredBy := range []string{
		"a.example.org",
		"https://a.example.org",
		"http://a.example.org:8111",
		"https://A.Example.ORG/",
	} {
		if err := federation.AcceptDelivered(
			event, verifying, "general@a.example.org", "a.example.org", deliveredBy,
		); err != nil {
			t.Errorf("delivery named as %q was refused: %v", deliveredBy, err)
		}
	}

	// And a genuinely different server is still refused, or the above would
	// be satisfied by a check that accepted anything.
	err := federation.AcceptDelivered(
		event, verifying, "general@a.example.org", "a.example.org", "https://c.example.org")
	if !errors.Is(err, federation.ErrNotHomeServer) {
		t.Fatalf("AcceptDelivered from a stranger = %v, want ErrNotHomeServer", err)
	}
}
