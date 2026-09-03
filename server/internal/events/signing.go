package events

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// KeyPair is a server's signing key.
//
// Ed25519 rather than RSA: signatures and keys are small, verification is
// fast, and there are no parameter choices to get wrong. Federated servers
// will verify every event they receive, so the cost matters.
type KeyPair struct {
	// ID names this key, so a signature can say which key made it and a
	// verifier can find the right public key after rotation.
	ID        string
	Public    ed25519.PublicKey
	private   ed25519.PrivateKey
	CreatedAt time.Time
	// RetiredAt marks a key that no longer signs new events but must still
	// verify the ones it signed. History does not become unverifiable because
	// a key was rotated.
	RetiredAt *time.Time
}

// NewKeyPair generates a signing key.
func NewKeyPair(id string) (*KeyPair, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}

	return &KeyPair{
		ID:        id,
		Public:    public,
		private:   private,
		CreatedAt: time.Now(),
	}, nil
}

// KeyPairFromSeed rebuilds a key from its stored seed, so a server keeps its
// identity across restarts. A server that generated a new key each start would
// invalidate every signature it had ever made.
func KeyPairFromSeed(id string, seed []byte) (*KeyPair, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("signing key seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}

	private := ed25519.NewKeyFromSeed(seed)
	return &KeyPair{
		ID:        id,
		Public:    private.Public().(ed25519.PublicKey),
		private:   private,
		CreatedAt: time.Now(),
	}, nil
}

// Seed returns the private seed, for storage. It is a secret: anything holding
// it can sign as this server.
func (k *KeyPair) Seed() []byte {
	return k.private.Seed()
}

// PublicKeyBase64 returns the public key in the form published to peers.
func (k *KeyPair) PublicKeyBase64() string {
	return base64.RawURLEncoding.EncodeToString(k.Public)
}

// Retired reports whether this key may still sign new events.
func (k *KeyPair) Retired() bool {
	return k.RetiredAt != nil && time.Now().After(*k.RetiredAt)
}

// Keyring holds a server's signing keys: one current, plus retired keys kept
// so their signatures still verify.
type Keyring struct {
	mu      sync.RWMutex
	current *KeyPair
	keys    map[string]*KeyPair
}

// NewKeyring builds a keyring with the given key as current.
func NewKeyring(current *KeyPair) *Keyring {
	return &Keyring{
		current: current,
		keys:    map[string]*KeyPair{current.ID: current},
	}
}

// Current returns the key new events are signed with.
func (r *Keyring) Current() *KeyPair {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// Add makes a key available for verification without making it current, which
// is how a peer's published keys are held.
func (r *Keyring) Add(key *KeyPair) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[key.ID] = key
}

// Rotate makes a new key current and retires the previous one.
//
// The retired key stays in the ring, so every event it signed continues to
// verify. Retiring a key must never make history unverifiable.
func (r *Keyring) Rotate(next *KeyPair) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current != nil {
		retiredAt := time.Now()
		r.current.RetiredAt = &retiredAt
	}

	r.keys[next.ID] = next
	r.current = next
}

// Lookup finds a key by identifier, including retired ones.
func (r *Keyring) Lookup(id string) (*KeyPair, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.keys[id]
	return key, ok
}

// Sign fills in an event's identifier and signature.
//
// The identifier is derived before signing, so it covers exactly the bytes the
// signature covers.
func (r *Keyring) Sign(event *Event) error {
	if err := event.Validate(); err != nil {
		return err
	}

	key := r.Current()
	if key == nil {
		return fmt.Errorf("no signing key is available")
	}

	id, err := event.DeriveID()
	if err != nil {
		return err
	}

	canonical, err := event.canonical()
	if err != nil {
		return err
	}

	event.ID = id
	event.KeyID = key.ID
	event.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key.private, canonical))

	return nil
}

// Verify checks an event's identifier and signature.
//
// Both are checked: a valid signature over different content would still be
// wrong if the identifier did not match, and a matching identifier proves
// nothing without a signature.
func (r *Keyring) Verify(event Event) error {
	if err := event.Validate(); err != nil {
		return err
	}

	if event.Signature == "" || event.KeyID == "" {
		return fmt.Errorf("%w: event is not signed", ErrSignatureInvalid)
	}

	expectedID, err := event.DeriveID()
	if err != nil {
		return err
	}
	if event.ID != expectedID {
		// The content does not match the identifier, so the event has been
		// altered since it was created.
		return fmt.Errorf("%w: identifier does not match content", ErrSignatureInvalid)
	}

	key, ok := r.Lookup(event.KeyID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKey, event.KeyID)
	}

	signature, err := base64.RawURLEncoding.DecodeString(event.Signature)
	if err != nil {
		return fmt.Errorf("%w: signature is not valid base64", ErrSignatureInvalid)
	}

	canonical, err := event.canonical()
	if err != nil {
		return err
	}

	if !ed25519.Verify(key.Public, canonical, signature) {
		return ErrSignatureInvalid
	}

	return nil
}
