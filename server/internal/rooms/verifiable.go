package rooms

import (
	"encoding/json"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Turning a stored row back into the event that was signed.

Everything an event needs to prove itself is in its own row: the addresses, the
type, the timestamp, the nonce, the content or its digest, the signature and the
key that made it. Nothing is read from anywhere else — no join, no lookup, no
server-local state — because that is the property federation rests on. A server
that needed its own database to verify its own events could never hand one to
anybody else.

The identifier is deliberately not copied from the column. It is derived from
the rest, and comparing the two is what makes a row somebody edited in the
database fail to verify.
*/
func Verifiable(row dbgen.Event) events.Event {
	event := events.Event{
		ID:        row.ID,
		Room:      row.RoomAddress,
		Sender:    row.SenderAddress,
		Origin:    row.OriginServer,
		Type:      events.Type(row.Type),
		Sequence:  row.Sequence,
		CreatedAt: row.CreatedAt,
		Content:   json.RawMessage(row.Content),
		Nonce:     row.Nonce,
		Signature: row.Signature,
		KeyID:     row.KeyID,
	}

	/*
		The digest of the content exactly as it was signed, for every event.

		Not only redacted ones. Postgres stores content as jsonb, which
		normalises it — keys reordered, spacing changed — so the bytes read back
		are not the bytes that were signed and hashing them would never reach
		the right value. The digest recorded at the moment of signing is what
		verification uses, which makes that normalisation harmless and keeps a
		redacted event verifiable by the same mechanism.
	*/
	if row.ContentHash != nil {
		event.ContentHash = *row.ContentHash
	}

	return event
}

/*
Unrecorded marks an event written before its content digest was kept.

Those cannot be verified and never will be: what was signed is the content
before the database normalised it, and that is gone. Computing a digest from
what is stored would record something nobody signed, which is worse than
recording nothing — it would make an unverifiable event look verifiable.

Callers check for it so that "this cannot be checked" is a distinct answer from
"this failed to check".
*/
const Unrecorded = "unrecorded"

// Verifiable reports whether an event can be checked at all.
func CanVerify(row dbgen.Event) bool {
	return row.ContentHash != nil && *row.ContentHash != Unrecorded
}
