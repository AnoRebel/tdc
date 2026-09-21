// Package events defines the signed, append-only record of everything that
// happens in a room.
//
// Every message and state change is an event. Timelines, membership and unread
// counts are all derived from the log rather than maintained independently,
// which is what makes them reconstructible and what will later make it
// possible for another server to verify what this one asserts.
package events

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Type names what an event records.
type Type string

const (
	// Room lifecycle.
	TypeRoomCreated  Type = "room.created"
	TypeRoomRenamed  Type = "room.renamed"
	TypeRoomArchived Type = "room.archived"
	TypeRoomRestored Type = "room.restored"

	// Membership.
	TypeMemberJoined  Type = "member.joined"
	TypeMemberLeft    Type = "member.left"
	TypeMemberRemoved Type = "member.removed"

	// Messages.
	TypeMessageSent    Type = "message.sent"
	TypeMessageEdited  Type = "message.edited"
	TypeMessageDeleted Type = "message.deleted"

	/*
		Somebody pressed a button on a message.

		An event in the room's log like any other, so the program that posted
		the message receives it over the live connection it already holds —
		rather than a separate delivery mechanism with its own retries, its own
		failures and its own way of being wrong.
	*/
	TypeActionPressed Type = "action.pressed"
)

// Event is one entry in a room's log.
//
// The fields are ordered and named for the wire: this is what gets signed, so
// its shape is a compatibility surface. Adding a field is safe; changing the
// meaning of one is not.
type Event struct {
	// ID is derived from the content, so any change to a signed field yields a
	// different identifier. It is not chosen by the sender.
	ID string `json:"id"`

	// Room this event belongs to, in room@server form.
	Room string `json:"room"`
	// Sender in user@server form. Naming the home server is what keeps the
	// attribution unambiguous once servers exchange events.
	Sender string `json:"sender"`
	// Origin is the server that accepted this event, which is the server whose
	// signature must verify.
	Origin string `json:"origin"`

	Type Type `json:"type"`
	// Sequence is this event's position in its room's total order, assigned by
	// the room's home server. Zero before the event is accepted.
	Sequence int64 `json:"sequence"`
	// CreatedAt is when the origin server accepted the event, in milliseconds
	// since the Unix epoch. Millisecond precision keeps the signed form stable
	// across encodings that do not agree on sub-millisecond digits.
	CreatedAt int64 `json:"createdAt"`

	// Content is the type-specific payload.
	Content json.RawMessage `json:"content"`

	// Nonce distinguishes events that are otherwise identical.
	//
	// Without it, two events with the same sender, room, type, content and
	// millisecond timestamp derive the same identifier — which a person
	// double-clicking send, or an integration posting a repeated status line,
	// produces in ordinary use. It is part of the signed form, so it cannot be
	// altered after the fact, and it is what makes identity unique by
	// construction rather than by hoping the inputs differ.
	Nonce string `json:"nonce"`

	// Signature over the canonical form, produced by the origin server.
	Signature string `json:"signature,omitempty"`
	// KeyID names which of the origin's keys signed this, so a key can be
	// rotated without invalidating history.
	KeyID string `json:"keyId,omitempty"`

	/*
		ContentHash is what the content was, for an event whose content is gone.

		Empty on an ordinary event, where the content is present and the
		identifier is derived from it directly. Set when an event is redacted,
		and it is what keeps a redacted event verifiable: the identifier and the
		signature were computed over content that no longer exists, so without
		this they could never be reproduced and the event would read as tampered
		with rather than as deliberately emptied.

		A hash rather than the content, obviously. It discloses nothing about
		what was said — recovering the body from it means guessing the body —
		while still proving that whatever was signed hashed to this.

		Not part of the canonical form. It describes the content rather than
		being content, and including it would change the identifier of every
		event that has one.
	*/
	ContentHash string `json:"contentHash,omitempty"`
}

// Errors callers are expected to handle.
var (
	ErrMalformed        = errors.New("event is malformed")
	ErrSignatureInvalid = errors.New("event signature is not valid")
	ErrUnknownKey       = errors.New("event was signed by an unknown key")
)

/*
canonicalForID is what the identifier and the signature are computed over.

The content appears as its own digest rather than in full, which is the whole
mechanism by which a redacted event stays verifiable: an event that still has
its content hashes it here, and one whose content has been removed uses the
digest recorded when it was removed. Both reach the same bytes, so both derive
the same identifier and check against the same signature.

The alternative — signing the content directly — means a redacted event can
never reproduce what was signed, so a deliberate redaction is indistinguishable
from tampering. That is the distinction the log exists to keep.
*/
func (e Event) canonicalForID() ([]byte, error) {
	hash := e.ContentHash
	if hash == "" {
		hash = HashContent(e.Content)
	}
	return e.canonicalWithHash(hash)
}

// canonicalWithHash is the canonical form with a given content digest in it.
func (e Event) canonicalWithHash(contentHash string) ([]byte, error) {
	form := struct {
		Room        string `json:"room"`
		Sender      string `json:"sender"`
		Origin      string `json:"origin"`
		Type        Type   `json:"type"`
		CreatedAt   int64  `json:"createdAt"`
		Nonce       string `json:"nonce"`
		ContentHash string `json:"contentHash"`
	}{
		Room:        e.Room,
		Sender:      e.Sender,
		Origin:      e.Origin,
		Type:        e.Type,
		CreatedAt:   e.CreatedAt,
		Nonce:       e.Nonce,
		ContentHash: contentHash,
	}

	encoded, err := json.Marshal(form)
	if err != nil {
		return nil, fmt.Errorf("encode event: %w", err)
	}
	return encoded, nil
}

/*
HashContent is the digest of an event's content.

Over a canonical form of the JSON — keys sorted, no insignificant whitespace —
rather than the bytes as they happen to be written. That matters because the
content makes a round trip through Postgres's jsonb, which reorders keys and
respaces: the bytes that come back are not the bytes that went in, and hashing
them directly would mean no stored event ever matched its own signature.

Canonicalising means the digest is a property of what the content *says* rather
than how it was typed, so it survives the database, and a redacted event's
recorded digest is the same value its content would still produce.
*/
func HashContent(content json.RawMessage) string {
	sum := sha256.Sum256(canonicalJSON(content))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

/*
canonicalJSON reduces JSON to one form per value.

Go's encoding/json sorts map keys when it marshals a map, so decoding into
interface{} and re-encoding produces sorted keys and minimal whitespace — which
is exactly the canonicalisation wanted, without hand-written traversal.

Content that will not parse is hashed as it stands. It cannot have come from
this server, and refusing to hash it would turn a malformed event into an error
in a place that has no way to report one; verification will reject it anyway.
*/
func canonicalJSON(content json.RawMessage) []byte {
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return content
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return content
	}
	return encoded
}

/*
Redacted says whether this event's content has been removed.

Decided from the content rather than from the presence of a digest: every event
carries a digest now, so a digest says nothing about whether the content is
still there. An empty JSON object is what redaction leaves behind.
*/
func (e Event) Redacted() bool {
	return len(canonicalJSON(e.Content)) <= len("{}")
}

// DeriveID computes the content-derived identifier.
//
// Because it covers every signed field, two events differing in any of them
// cannot share an identifier, and altering a stored event makes its identifier
// wrong — which is what makes tampering detectable without re-verifying every
// signature.
func (e Event) DeriveID() (string, error) {
	/*
		A redacted event derives its identifier from what its content was.

		The identifier and the signature were both computed over content this
		event no longer carries. Deriving from the empty content it carries now
		would produce a different identifier and make a deliberate redaction
		indistinguishable from tampering — which is exactly the distinction the
		log exists to keep.
	*/
	canonical, err := e.canonicalForID()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	// URL-safe and unpadded, so an identifier can sit in a path or a header
	// without escaping.
	return "$" + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// Validate checks the fields every event must carry, before any signature work.
func (e Event) Validate() error {
	switch {
	case e.Room == "":
		return fmt.Errorf("%w: room is required", ErrMalformed)
	case e.Sender == "":
		return fmt.Errorf("%w: sender is required", ErrMalformed)
	case e.Origin == "":
		return fmt.Errorf("%w: origin is required", ErrMalformed)
	case e.Type == "":
		return fmt.Errorf("%w: type is required", ErrMalformed)
	case e.CreatedAt <= 0:
		return fmt.Errorf("%w: createdAt is required", ErrMalformed)
	case len(e.Content) == 0:
		return fmt.Errorf("%w: content is required", ErrMalformed)
	case e.Nonce == "":
		return fmt.Errorf("%w: nonce is required", ErrMalformed)
	}

	if !json.Valid(e.Content) {
		return fmt.Errorf("%w: content is not valid JSON", ErrMalformed)
	}

	return nil
}

// NewNonce returns a fresh nonce.
func NewNonce() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate event nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// Now returns the current time in the representation events use.
func Now() int64 {
	return time.Now().UnixMilli()
}

// Time returns the event's creation time.
func (e Event) Time() time.Time {
	return time.UnixMilli(e.CreatedAt)
}
