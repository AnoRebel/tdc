package rooms_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
Two properties of the log, both of which are easy to lose and hard to notice.

A redacted event must still prove itself. Its identifier and signature were
computed over content that is now gone, so without care it reads as tampered
with — and an event that cannot tell a deliberate redaction from an alteration
is a log that proves nothing.

And an event must be verifiable from itself. Not from the database it happens to
sit in: that is the whole premise of federation, and a dependency on local state
is invisible until the day somebody tries to verify an event they did not store.
*/

/*
TestARedactedEventStillProvesItself.

The content is gone and unretrievable, and the event still verifies. Both halves
matter: a redaction that kept the content would not be a redaction, and one that
broke verification would make every removal look like tampering.
*/
func TestARedactedEventStillProvesItself(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "conversation")

	stored, err := service.SendMessage(ctx, room.ID, sender.ID, "something regrettable", "")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	// It verifies before anything is done to it.
	if err := service.Keyring().Verify(rooms.Verifiable(stored)); err != nil {
		t.Fatalf("a freshly stored event does not verify: %v", err)
	}

	if _, err := service.DeleteMessage(ctx, room.ID, sender.ID, stored.ID, false); err != nil {
		t.Fatalf("redacting: %v", err)
	}

	redacted, err := service.Event(ctx, stored.ID)
	if err != nil {
		t.Fatalf("reading the redacted event: %v", err)
	}

	// The content is gone.
	if strings.Contains(string(redacted.Content), "regrettable") {
		t.Errorf("the content survived redaction: %s", redacted.Content)
	}
	if redacted.RedactedAt == nil {
		t.Error("the event does not record that it was redacted")
	}

	// And it still proves itself.
	if err := service.Keyring().Verify(rooms.Verifiable(redacted)); err != nil {
		t.Errorf("a redacted event no longer verifies, so a removal is indistinguishable from tampering: %v", err)
	}
}

/*
TestRedactionLeavesTheNeighboursAlone.

The surrounding events must remain verifiable and correctly ordered, which the
specification asks for by name. A scheme that rewrote neighbours — a chain of
hashes, say — would make one removal invalidate everything after it.
*/
func TestRedactionLeavesTheNeighboursAlone(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "conversation")

	sent := make([]dbgen.Event, 0, 5)
	for index := 0; index < 5; index++ {
		event, err := service.SendMessage(ctx, room.ID, sender.ID, "message", "")
		if err != nil {
			t.Fatalf("sending %d: %v", index, err)
		}
		sent = append(sent, event)
	}

	// The one in the middle.
	if _, err := service.DeleteMessage(ctx, room.ID, sender.ID, sent[2].ID, false); err != nil {
		t.Fatalf("redacting: %v", err)
	}

	for index, original := range sent {
		current, err := service.Event(ctx, original.ID)
		if err != nil {
			t.Fatalf("reading event %d: %v", index, err)
		}

		if err := service.Keyring().Verify(rooms.Verifiable(current)); err != nil {
			t.Errorf("event %d no longer verifies after its neighbour was redacted: %v", index, err)
		}
		if current.Sequence != original.Sequence {
			t.Errorf("event %d moved from sequence %d to %d",
				index, original.Sequence, current.Sequence)
		}
	}
}

/*
TestAnAlteredEventDoesNotVerify.

The other half of the same property. If a redacted event verifies, a changed one
must not — otherwise the digest would be a way of laundering an edit rather than
of preserving a proof.
*/
func TestAnAlteredEventDoesNotVerify(t *testing.T) {
	service, queries, pool := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "conversation")

	stored, err := service.SendMessage(ctx, room.ID, sender.ID, "what was said", "")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	// Edited in the database, as somebody with access to it could.
	if _, err := pool.Exec(ctx,
		`UPDATE events SET content = '{"body":"what was not said"}'::jsonb WHERE id = $1`,
		stored.ID); err != nil {
		t.Fatalf("altering: %v", err)
	}

	altered, err := service.Event(ctx, stored.ID)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if err := service.Keyring().Verify(rooms.Verifiable(altered)); err == nil {
		t.Error("an event edited in the database still verifies, so the log proves nothing")
	}
}

/*
TestARedactedEventCannotBeAlteredEither.

A redacted event verifies against a digest, which is a second place somebody
could try to lie: writing a different hash to make different content appear to
have been what was there. It fails, because the digest is part of what the
identifier is derived from.
*/
func TestARedactedEventCannotBeAlteredEither(t *testing.T) {
	service, queries, pool := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "conversation")

	stored, err := service.SendMessage(ctx, room.ID, sender.ID, "something", "")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	if _, err := service.DeleteMessage(ctx, room.ID, sender.ID, stored.ID, false); err != nil {
		t.Fatalf("redacting: %v", err)
	}

	// A digest of something else entirely.
	forged := events.HashContent(json.RawMessage(`{"body":"a different message"}`))
	if _, err := pool.Exec(ctx,
		`UPDATE events SET content_hash = $2 WHERE id = $1`, stored.ID, forged); err != nil {
		t.Fatalf("forging: %v", err)
	}

	forgedEvent, err := service.Event(ctx, stored.ID)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if err := service.Keyring().Verify(rooms.Verifiable(forgedEvent)); err == nil {
		t.Error("a forged content digest verifies, so a redaction can be used to rewrite history")
	}
}

/*
TestAForeignEventVerifiesWithoutItsOriginServer.

Federation readiness, which is what task 10.7 asks for. An event created by one
server is handed to another, along with nothing but the first server's published
public key — no database, no connection, no shared secret — and the second
verifies it.

The way to fail this is not to write code that refuses. It is to have some field
required for verification live somewhere other than the event, so that a server
holding only the event cannot do it. This checks by building a verifier that
holds only the key.
*/
func TestAForeignEventVerifiesWithoutItsOriginServer(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "conversation")

	stored, err := service.SendMessage(ctx, room.ID, sender.ID, "hello from elsewhere", "")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	/*
		What the other server has: the event, and a published key.

		The key is reconstructed from its public half alone, as a server reading
		somebody else's published keys would — not borrowed from the origin's
		keyring, which would be the origin lending its own state and prove
		nothing.
	*/
	published := service.Keyring().Published()
	if len(published) == 0 {
		t.Fatal("the server publishes no keys, so nothing could ever verify its events")
	}

	elsewhere, err := events.KeyringFromPublished(published)
	if err != nil {
		t.Fatalf("building a verifier from published keys: %v", err)
	}

	foreign := rooms.Verifiable(stored)
	if err := elsewhere.Verify(foreign); err != nil {
		t.Errorf("a server holding only the event and the published key cannot verify it: %v", err)
	}

	/*
		And it is genuinely checking rather than accepting anything.

		A verifier that returned nil regardless would pass the test above, which
		is the way this kind of test quietly stops meaning anything.
	*/
	tampered := foreign
	tampered.Content = json.RawMessage(`{"body":"not what was sent"}`)
	if err := elsewhere.Verify(tampered); err == nil {
		t.Error("the foreign verifier accepts an altered event, so it is not verifying")
	}
}

/*
TestNothingNeededForVerificationIsServerLocal.

The second federation scenario, checked against the reconstruction itself: every
field verification uses comes from the event's own row. Written as a test rather
than a comment because it is the kind of thing a later change breaks by adding a
convenient join.
*/
func TestNothingNeededForVerificationIsServerLocal(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	sender := makeUser(t, queries, "amina")
	room := makeRoom(t, service, sender.ID, "conversation")

	stored, err := service.SendMessage(ctx, room.ID, sender.ID, "hello", "")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	/*
		Everything local removed.

		The sender's account identifier, the room's identifier, the redaction
		bookkeeping — all of them are this server's own rows, meaningless to
		anybody else. What is left is the addresses, which name servers rather
		than rows, and the signed fields.
	*/
	stripped := stored
	stripped.SenderID = nil
	stripped.RoomID = [16]byte{}
	stripped.RedactedBy = nil

	if err := service.Keyring().Verify(rooms.Verifiable(stripped)); err != nil {
		t.Errorf("verification depends on something only this server has: %v", err)
	}
}
