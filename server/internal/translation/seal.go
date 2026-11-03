package translation

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
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
	plain, err := s.sealer.open(userID, provider, sealed)
	if err != nil && s.previous != nil {
		return s.previous.open(userID, provider, sealed)
	}
	return plain, err
}

/*
ResealAll moves every saved key still sealed under the previous secret key to
the current one, so the previous key can then be removed from configuration
without anybody connecting again.

Run at startup. A key that opens under neither is left alone, and goes on
asking its owner to connect again, as it did. A row changed while this ran
keeps its change.
*/
func (s *Service) ResealAll(ctx context.Context, queries *dbgen.Queries) (int, error) {
	if s.sealer == nil || s.previous == nil {
		return 0, nil
	}
	rows, err := queries.AllTranslationConnections(ctx)
	if err != nil {
		return 0, fmt.Errorf("translation: reading saved keys: %w", err)
	}
	moved := 0
	for _, row := range rows {
		provider := Provider(row.Provider)
		if len(row.SealedKey) == 0 {
			continue
		}
		if _, err := s.sealer.open(row.UserID, provider, row.SealedKey); err == nil {
			continue
		}
		plain, err := s.previous.open(row.UserID, provider, row.SealedKey)
		if err != nil {
			continue
		}
		resealed, err := s.sealer.seal(row.UserID, provider, plain)
		if err != nil {
			return moved, err
		}
		changed, err := queries.ResealTranslationConnection(ctx, dbgen.ResealTranslationConnectionParams{
			UserID: row.UserID, Provider: row.Provider, Resealed: resealed, Sealed: row.SealedKey,
		})
		if err != nil {
			return moved, fmt.Errorf("translation: resealing a key: %w", err)
		}
		moved += int(changed)
	}
	return moved, nil
}

// CanStoreKeys reports whether personal keys can be saved here.
func (s *Service) CanStoreKeys() bool { return s.sealer != nil }
