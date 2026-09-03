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
}

// Errors callers are expected to handle.
var (
	ErrMalformed        = errors.New("event is malformed")
	ErrSignatureInvalid = errors.New("event signature is not valid")
	ErrUnknownKey       = errors.New("event was signed by an unknown key")
)

// canonical returns the bytes that are signed and hashed.
//
// Identifier and signature are excluded: the identifier is derived from this
// form, and the signature is over it. Sequence is excluded too, because it is
// assigned by the home server when the event is accepted, and an event's
// identity must not change at that moment.
//
// Go's encoding/json sorts map keys and emits struct fields in declaration
// order, so this is deterministic for a given struct definition.
func (e Event) canonical() ([]byte, error) {
	form := struct {
		Room      string          `json:"room"`
		Sender    string          `json:"sender"`
		Origin    string          `json:"origin"`
		Type      Type            `json:"type"`
		CreatedAt int64           `json:"createdAt"`
		Nonce     string          `json:"nonce"`
		Content   json.RawMessage `json:"content"`
	}{
		Room:      e.Room,
		Sender:    e.Sender,
		Origin:    e.Origin,
		Type:      e.Type,
		CreatedAt: e.CreatedAt,
		Nonce:     e.Nonce,
		Content:   e.Content,
	}

	encoded, err := json.Marshal(form)
	if err != nil {
		return nil, fmt.Errorf("encode event: %w", err)
	}
	return encoded, nil
}

// DeriveID computes the content-derived identifier.
//
// Because it covers every signed field, two events differing in any of them
// cannot share an identifier, and altering a stored event makes its identifier
// wrong — which is what makes tampering detectable without re-verifying every
// signature.
func (e Event) DeriveID() (string, error) {
	canonical, err := e.canonical()
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
