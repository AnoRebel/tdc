package events_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/events"
)

func newEvent() events.Event {
	return events.Event{
		Room:      "general@tdc.example",
		Sender:    "amina@tdc.example",
		Origin:    "tdc.example",
		Type:      events.TypeMessageSent,
		CreatedAt: events.Now(),
		Content:   json.RawMessage(`{"body":"hello"}`),
	}
}

func newKeyring(t *testing.T) *events.Keyring {
	t.Helper()
	key, err := events.NewKeyPair("key-1")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	return events.NewKeyring(key)
}

func TestSignedEventVerifies(t *testing.T) {
	ring := newKeyring(t)
	event := newEvent()

	if err := ring.Sign(&event); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if event.ID == "" || event.Signature == "" || event.KeyID == "" {
		t.Fatal("signing left the event incomplete")
	}
	if err := ring.Verify(event); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

// The identifier is derived from content, so any change to a signed field must
// produce a different identifier.
func TestIdentifierChangesWithEverySignedField(t *testing.T) {
	base := newEvent()
	baseID, err := base.DeriveID()
	if err != nil {
		t.Fatalf("DeriveID: %v", err)
	}

	variations := map[string]func(*events.Event){
		"room":      func(e *events.Event) { e.Room = "other@tdc.example" },
		"sender":    func(e *events.Event) { e.Sender = "juma@tdc.example" },
		"origin":    func(e *events.Event) { e.Origin = "elsewhere.example" },
		"type":      func(e *events.Event) { e.Type = events.TypeMessageEdited },
		"createdAt": func(e *events.Event) { e.CreatedAt = base.CreatedAt + 1 },
		"content":   func(e *events.Event) { e.Content = json.RawMessage(`{"body":"different"}`) },
	}

	for field, mutate := range variations {
		t.Run(field, func(t *testing.T) {
			altered := base
			mutate(&altered)

			alteredID, err := altered.DeriveID()
			if err != nil {
				t.Fatalf("DeriveID: %v", err)
			}
			if alteredID == baseID {
				t.Errorf("changing %s did not change the identifier", field)
			}
		})
	}
}

// The identifier must not depend on the sequence number, which the home server
// assigns when it accepts the event: an event's identity cannot change at that
// moment.
func TestIdentifierIgnoresSequence(t *testing.T) {
	event := newEvent()
	before, err := event.DeriveID()
	if err != nil {
		t.Fatalf("DeriveID: %v", err)
	}

	event.Sequence = 42
	after, err := event.DeriveID()
	if err != nil {
		t.Fatalf("DeriveID: %v", err)
	}

	if before != after {
		t.Error("assigning a sequence number changed the event's identifier")
	}
}

// Altering a stored event must be detectable.
func TestTamperingIsDetected(t *testing.T) {
	ring := newKeyring(t)
	event := newEvent()
	if err := ring.Sign(&event); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	t.Run("content altered", func(t *testing.T) {
		tampered := event
		tampered.Content = json.RawMessage(`{"body":"something else entirely"}`)
		if err := ring.Verify(tampered); !errors.Is(err, events.ErrSignatureInvalid) {
			t.Errorf("error = %v, want ErrSignatureInvalid", err)
		}
	})

	t.Run("sender altered", func(t *testing.T) {
		tampered := event
		tampered.Sender = "impostor@tdc.example"
		if err := ring.Verify(tampered); !errors.Is(err, events.ErrSignatureInvalid) {
			t.Errorf("error = %v, want ErrSignatureInvalid", err)
		}
	})

	// Altering content and recomputing the identifier must still fail, because
	// the signature covers the same bytes.
	t.Run("content and identifier both altered", func(t *testing.T) {
		tampered := event
		tampered.Content = json.RawMessage(`{"body":"forged"}`)
		id, err := tampered.DeriveID()
		if err != nil {
			t.Fatalf("DeriveID: %v", err)
		}
		tampered.ID = id

		if err := ring.Verify(tampered); !errors.Is(err, events.ErrSignatureInvalid) {
			t.Errorf("error = %v, want ErrSignatureInvalid", err)
		}
	})
}

func TestUnsignedEventIsRejected(t *testing.T) {
	ring := newKeyring(t)
	event := newEvent()

	id, err := event.DeriveID()
	if err != nil {
		t.Fatalf("DeriveID: %v", err)
	}
	event.ID = id

	if err := ring.Verify(event); !errors.Is(err, events.ErrSignatureInvalid) {
		t.Errorf("error = %v, want ErrSignatureInvalid", err)
	}
}

// Rotating a key must not make history unverifiable.
func TestRetiredKeyStillVerifiesItsHistory(t *testing.T) {
	ring := newKeyring(t)

	old := newEvent()
	if err := ring.Sign(&old); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	signedBy := old.KeyID

	next, err := events.NewKeyPair("key-2")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	ring.Rotate(next)

	// The old event still verifies.
	if err := ring.Verify(old); err != nil {
		t.Errorf("an event signed by the retired key no longer verifies: %v", err)
	}

	// New events are signed by the new key.
	fresh := newEvent()
	fresh.CreatedAt = old.CreatedAt + 1000
	if err := ring.Sign(&fresh); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if fresh.KeyID == signedBy {
		t.Error("a new event was signed by the retired key")
	}
	if err := ring.Verify(fresh); err != nil {
		t.Errorf("Verify after rotation: %v", err)
	}
}

func TestUnknownKeyIsReported(t *testing.T) {
	ring := newKeyring(t)
	event := newEvent()
	if err := ring.Sign(&event); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	event.KeyID = "a-key-this-server-has-never-seen"
	if err := ring.Verify(event); !errors.Is(err, events.ErrUnknownKey) {
		t.Errorf("error = %v, want ErrUnknownKey", err)
	}
}

// The federation-readiness property: another server holding only the
// originator's published public key and the event itself must be able to
// verify it, with no shared secret and no access to the originator's database.
func TestForeignEventVerifiesFromPublishedKeyAlone(t *testing.T) {
	origin := newKeyring(t)

	event := newEvent()
	if err := origin.Sign(&event); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// A second server that knows nothing but the published public key.
	published, ok := origin.Lookup(event.KeyID)
	if !ok {
		t.Fatal("the signing key is not in the originator's ring")
	}

	peerKey, err := events.NewKeyPair("peer-key")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	peer := events.NewKeyring(peerKey)
	peer.Add(&events.KeyPair{ID: published.ID, Public: published.Public})

	if err := peer.Verify(event); err != nil {
		t.Errorf("a peer could not verify the event from the published key alone: %v", err)
	}
}

// A server must reject an event whose signature does not match the key it
// names, which is what stops one server forging another's events.
func TestSignatureFromAnotherServerIsRejected(t *testing.T) {
	honest := newKeyring(t)

	forgerKey, err := events.NewKeyPair("key-1") // the same key identifier
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	forger := events.NewKeyring(forgerKey)

	forged := newEvent()
	if err := forger.Sign(&forged); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// The honest server holds a different key under that identifier.
	if err := honest.Verify(forged); !errors.Is(err, events.ErrSignatureInvalid) {
		t.Errorf("error = %v, want ErrSignatureInvalid", err)
	}
}

// A server keeps its identity across restarts: a key rebuilt from its seed
// verifies signatures the original made.
func TestKeySurvivesRestart(t *testing.T) {
	original, err := events.NewKeyPair("key-1")
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	ring := events.NewKeyring(original)

	event := newEvent()
	if err := ring.Sign(&event); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	restored, err := events.KeyPairFromSeed("key-1", original.Seed())
	if err != nil {
		t.Fatalf("KeyPairFromSeed: %v", err)
	}
	if err := events.NewKeyring(restored).Verify(event); err != nil {
		t.Errorf("an event does not verify after the key was reloaded: %v", err)
	}
}

func TestMalformedEventsAreRejected(t *testing.T) {
	ring := newKeyring(t)

	cases := map[string]func(*events.Event){
		"no room":      func(e *events.Event) { e.Room = "" },
		"no sender":    func(e *events.Event) { e.Sender = "" },
		"no origin":    func(e *events.Event) { e.Origin = "" },
		"no type":      func(e *events.Event) { e.Type = "" },
		"no timestamp": func(e *events.Event) { e.CreatedAt = 0 },
		"no content":   func(e *events.Event) { e.Content = nil },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			event := newEvent()
			mutate(&event)
			if err := ring.Sign(&event); !errors.Is(err, events.ErrMalformed) {
				t.Errorf("error = %v, want ErrMalformed", err)
			}
		})
	}
}

// Identifiers appear in paths and headers, so they must need no escaping.
func TestIdentifierIsSafeInUrlsAndHeaders(t *testing.T) {
	event := newEvent()
	id, err := event.DeriveID()
	if err != nil {
		t.Fatalf("DeriveID: %v", err)
	}

	if !strings.HasPrefix(id, "$") {
		t.Errorf("identifier %q does not carry the expected prefix", id)
	}
	for _, unsafe := range []string{"/", "+", "=", " ", "?", "#", "&"} {
		if strings.Contains(id, unsafe) {
			t.Errorf("identifier %q contains %q, which needs escaping", id, unsafe)
		}
	}
}
