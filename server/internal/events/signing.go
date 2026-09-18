package events

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
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

/*
PublishedKey is a key as another server receives it.

Public half only, which is the point: a peer needs enough to check a signature
and nothing that would let it make one. What a server publishes about itself is
exactly this, and nothing here is secret.
*/
type PublishedKey struct {
	ID string `json:"id"`
	// PublicKey is base64url, as PublicKeyBase64 produces it.
	PublicKey string `json:"publicKey"`
	// Retired marks a key that no longer signs but still verifies what it
	// signed. A peer must keep accepting those or history stops verifying.
	Retired bool `json:"retired,omitempty"`
}

/*
VerifyOnlyKey builds a key that can check a signature and cannot make one.

What a server does with somebody else's published key. The private half is
absent rather than zeroed, so signing with it is impossible rather than
unwise — there is nothing to sign with.
*/
func VerifyOnlyKey(published PublishedKey) (*KeyPair, error) {
	raw, err := base64.RawURLEncoding.DecodeString(published.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode the published key %q: %w", published.ID, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("the published key %q is %d bytes, expected %d",
			published.ID, len(raw), ed25519.PublicKeySize)
	}

	key := &KeyPair{
		ID:        published.ID,
		Public:    ed25519.PublicKey(raw),
		CreatedAt: time.Now(),
	}
	if published.Retired {
		retired := time.Now()
		key.RetiredAt = &retired
	}
	return key, nil
}

/*
KeyringFromPublished builds a keyring that can verify another server's events
and sign nothing.

This is the whole of what federation asks for on the receiving side: given a
peer's published keys and an event, decide whether the peer really signed it —
without a connection to that peer, a shared secret, or anything in its database.
*/
func KeyringFromPublished(published []PublishedKey) (*Keyring, error) {
	ring := &Keyring{keys: make(map[string]*KeyPair, len(published))}

	for _, entry := range published {
		key, err := VerifyOnlyKey(entry)
		if err != nil {
			return nil, err
		}
		ring.keys[key.ID] = key
	}

	if len(ring.keys) == 0 {
		return nil, errors.New("no published keys, so nothing could be verified")
	}
	return ring, nil
}

/*
Published is what this server tells its peers about its keys.

Every key, retired ones included: a peer that dropped a retired key would stop
being able to verify everything that key ever signed, which is history becoming
unverifiable because of a rotation.
*/
func (r *Keyring) Published() []PublishedKey {
	r.mu.RLock()
	defer r.mu.RUnlock()

	published := make([]PublishedKey, 0, len(r.keys))
	for _, key := range r.keys {
		published = append(published, PublishedKey{
			ID:        key.ID,
			PublicKey: key.PublicKeyBase64(),
			Retired:   key.Retired(),
		})
	}
	return published
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

	canonical, err := event.canonicalForID()
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

	/*
		Content that is present must match the digest that is recorded.

		Verification is otherwise over the digest, which is what makes it
		survive the database normalising the content and the content being
		removed. The cost is that the content itself would no longer be
		checked — so somebody editing the stored content would change what
		people read while the event still verified, which is precisely the
		tampering the log exists to detect.

		A redacted event is exempt: its content is gone, and the whole point is
		that it still verifies without it.
	*/
	if !event.Redacted() && event.ContentHash != "" {
		if HashContent(event.Content) != event.ContentHash {
			return fmt.Errorf("%w: content does not match its recorded digest", ErrSignatureInvalid)
		}
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

	canonical, err := event.canonicalForID()
	if err != nil {
		return err
	}

	if !ed25519.Verify(key.Public, canonical, signature) {
		return ErrSignatureInvalid
	}

	return nil
}
