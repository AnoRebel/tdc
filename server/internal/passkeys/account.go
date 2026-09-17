package passkeys

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
ErrLastCredential refuses to leave somebody with no way back in.

Not a courtesy. An account whose only passkey is revoked has no password to fall
back on and no recovery yet, so it is an account nobody can reach — including
the person who just did it, one click after deciding to tidy up.
*/
var ErrLastCredential = errors.New("this is the only way left into this account")

// accountFor loads an account in the shape the library expects, with its
// passkeys and the handle authenticators know it by.
func (s *Service) accountFor(ctx context.Context, userID uuid.UUID) (account, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return account{}, fmt.Errorf("load the account: %w", err)
	}

	/*
		The handle, established on first use and never changed.

		COALESCE in the query means a second caller finds the first one's value
		rather than overwriting it — which matters, because changing a handle
		orphans every passkey already enrolled against it.
	*/
	handle := user.WebauthnHandle
	if len(handle) == 0 {
		fresh, err := newHandle()
		if err != nil {
			return account{}, err
		}
		handle, err = s.store.EnsureWebauthnHandle(ctx, dbgen.EnsureWebauthnHandleParams{
			ID:             userID,
			WebauthnHandle: fresh,
		})
		if err != nil {
			return account{}, fmt.Errorf("establish the handle: %w", err)
		}
	}

	rows, err := s.store.ListPasskeysForUser(ctx, userID)
	if err != nil {
		return account{}, fmt.Errorf("load the passkeys: %w", err)
	}

	credentials := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		credential, err := decodeCredential(row)
		if err != nil {
			// One unreadable row must not make an account unusable: the others
			// still work, and this is said where somebody will see it.
			s.log.Error("a stored passkey could not be read",
				"credential", row.ID, "error", err)
			continue
		}
		credentials = append(credentials, credential)
	}

	display := user.DisplayName
	if display == "" {
		display = user.Username
	}

	return account{
		handle:      handle,
		name:        user.Username,
		displayName: display,
		credentials: credentials,
	}, nil
}

// encodeCredential is what goes in the JSON column: everything about the
// credential, so nothing is lost that a later version of the library may want.
func encodeCredential(credential *webauthn.Credential) ([]byte, error) {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return nil, fmt.Errorf("encode the passkey: %w", err)
	}
	return encoded, nil
}

/*
decodeCredential rebuilds a credential from its row.

The counter comes from the column rather than the JSON: the column is what each
sign-in updates, and the JSON is what the authenticator said when it was
enrolled. Reading the stale one would mean a cloned authenticator went unnoticed.
*/
func decodeCredential(row dbgen.Credential) (webauthn.Credential, error) {
	var credential webauthn.Credential
	if err := json.Unmarshal(row.Passkey, &credential); err != nil {
		return webauthn.Credential{}, fmt.Errorf("decode the passkey: %w", err)
	}

	if row.SignCount >= 0 {
		credential.Authenticator.SignCount = uint32(row.SignCount)
	}
	return credential, nil
}

// List is somebody's own passkeys.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]dbgen.Credential, error) {
	if !s.Enabled() {
		return nil, ErrNotEnabled
	}
	return s.store.ListPasskeysForUser(ctx, userID)
}

// Rename changes what somebody calls one of their passkeys.
func (s *Service) Rename(ctx context.Context, userID, credentialID uuid.UUID, label string) (dbgen.Credential, error) {
	if !s.Enabled() {
		return dbgen.Credential{}, ErrNotEnabled
	}
	return s.store.RenamePasskey(ctx, dbgen.RenamePasskeyParams{
		ID: credentialID, UserID: userID, Label: label,
	})
}

/*
Revoke removes a passkey, unless it is the last way into the account.

Counted across every kind rather than across passkeys: somebody with a password
may revoke their only passkey, and somebody with two passkeys and no password
may revoke one. What is refused is the removal that leaves nothing.
*/
func (s *Service) Revoke(ctx context.Context, userID, credentialID uuid.UUID) error {
	if !s.Enabled() {
		return ErrNotEnabled
	}

	remaining, err := s.store.CountCredentialsForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("count what is left: %w", err)
	}
	if remaining <= 1 {
		return ErrLastCredential
	}

	return s.store.DeleteCredential(ctx, dbgen.DeleteCredentialParams{
		ID: credentialID, UserID: userID,
	})
}
