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

/*
TestErasureRemovesWhatItSaysAndKeepsWhatItSays.

The specification asks that erasure be bounded and documented, and this is the
boundary as a test: what goes, what stays, and that the conversations other
people took part in survive it.

The failure to guard against is an erasure that claims more than it does. A
partial erasure described as a complete one is worse for an operator than an
honest partial one, because it is the description they will repeat to whoever
asked.
*/
func TestErasureRemovesWhatItSaysAndKeepsWhatItSays(t *testing.T) {
	service, queries, pool := newTestService(t)
	ctx := context.Background()

	leaving := makeUser(t, queries, "amina")
	staying := makeUser(t, queries, "juma")
	room := makeRoom(t, service, staying.ID, "conversation")

	if err := service.Join(ctx, room.ID, leaving.ID); err != nil {
		t.Fatalf("joining: %v", err)
	}

	// A conversation both of them were part of.
	theirs, err := service.SendMessage(ctx, room.ID, leaving.ID, "something they said", "")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	others, err := service.SendMessage(ctx, room.ID, staying.ID, "something somebody else said", "")
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	erased, err := service.Erase(ctx, leaving.ID)
	if err != nil {
		t.Fatalf("erasing: %v", err)
	}
	if erased.MessagesEmptied == 0 {
		t.Error("erasure reports emptying nothing")
	}

	// What they said is gone.
	after, err := service.Event(ctx, theirs.ID)
	if err != nil {
		t.Fatalf("reading their event: %v", err)
	}
	if strings.Contains(string(after.Content), "something they said") {
		t.Errorf("their words survived erasure: %s", after.Content)
	}

	// And their name with it.
	if strings.Contains(after.SenderAddress, "amina") {
		t.Errorf("their address survived erasure: %s", after.SenderAddress)
	}

	/*
		The erased event no longer verifies, and that is the documented cost.

		Erasure rewrites the sender's address, which is part of what was signed
		— so the event cannot reproduce its own identifier and there is no
		mechanism that could preserve both. A person asking to be erased has a
		right that outweighs this server's ability to prove who said something
		they are no longer allowed to be recorded as having said.

		Asserted rather than left implicit, so that a change which quietly
		restored verifiability — by keeping the address — fails here.
	*/
	if rooms.CanVerify(after) {
		t.Error("an erased event claims to be verifiable, which would mean the address was kept")
	}

	untouched, err := service.Event(ctx, others.ID)
	if err != nil {
		t.Fatalf("reading the other event: %v", err)
	}
	if !strings.Contains(string(untouched.Content), "somebody else") {
		t.Error("somebody else's message was erased too")
	}
	if err := service.Keyring().Verify(rooms.Verifiable(untouched)); err != nil {
		t.Errorf("somebody else's message stopped verifying: %v", err)
	}

	// The account is gone.
	if _, err := queries.GetUserByID(ctx, leaving.ID); err == nil {
		t.Error("the account survived erasure")
	}

	/*
		And what the documentation says remains, remains.

		Checked because the documentation is what an operator repeats to
		whoever asked — so a change that quietly removed more, or less, should
		fail here rather than make that description wrong.
	*/
	var remainingEvents int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM events WHERE id = $1", theirs.ID).Scan(&remainingEvents); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if remainingEvents != 1 {
		t.Error("the event row was deleted, which breaks the ordering of everybody else's conversation")
	}

	var searchCopies int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM message_search WHERE event_id = $1", theirs.ID).Scan(&searchCopies); err != nil {
		t.Fatalf("counting the search index: %v", err)
	}
	if searchCopies != 0 {
		t.Error("the search index still holds a copy of what they said")
	}
}
