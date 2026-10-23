package translation

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrCannotStoreKeys is returned when the operator has not configured
// translation.secret_key, so a personal key has nothing to be sealed with.
var ErrCannotStoreKeys = errors.New("this server is not set up to store translation keys")

// ErrUnreadableKey is returned when a sealed key will not open: a different
// secret key, or a row copied from somebody else's.
var ErrUnreadableKey = errors.New("a saved translation key could not be read")

// GenerateSecretKey returns a fresh secret key, encoded as configuration
// expects it.
func GenerateSecretKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("translation: generating a key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// ParseSecretKey decodes a configured secret key, or reports why it is not one.
func ParseSecretKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("must be base64")
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("must decode to 32 bytes, not %d", len(key))
	}
	return key, nil
}

/*
sealer encrypts personal keys at rest.

AES-256-GCM, with the account and provider as additional data. A database
read on its own does not reveal a key — the secret key lives in the server's
configuration, not beside the rows — and a sealed key copied onto another
person's row, or another provider's, fails to open rather than becoming theirs.
*/
type sealer struct {
	aead cipher.AEAD
}

func newSealer(key []byte) (*sealer, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("translation: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("translation: %w", err)
	}
	return &sealer{aead: aead}, nil
}

func associated(userID uuid.UUID, provider Provider) []byte {
	return append(userID[:], []byte(provider)...)
}

func (s *sealer) seal(userID uuid.UUID, provider Provider, plain string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("translation: %w", err)
	}
	return s.aead.Seal(nonce, nonce, []byte(plain), associated(userID, provider)), nil
}

func (s *sealer) open(userID uuid.UUID, provider Provider, sealed []byte) (string, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size {
		return "", ErrUnreadableKey
	}
	plain, err := s.aead.Open(nil, sealed[:size], sealed[size:], associated(userID, provider))
	if err != nil {
		return "", ErrUnreadableKey
	}
	return string(plain), nil
}

// Seal encrypts a personal key for storage.
func (s *Service) Seal(userID uuid.UUID, provider Provider, key string) ([]byte, error) {
	if s.sealer == nil {
		return nil, ErrCannotStoreKeys
	}
	return s.sealer.seal(userID, provider, key)
}

// Open decrypts a stored personal key, for one request to its provider.
func (s *Service) Open(userID uuid.UUID, provider Provider, sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	if s.sealer == nil {
		return "", ErrCannotStoreKeys
	}
	return s.sealer.open(userID, provider, sealed)
}

// CanStoreKeys reports whether personal keys can be saved here.
func (s *Service) CanStoreKeys() bool { return s.sealer != nil }
