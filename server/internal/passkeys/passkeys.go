/*
Package passkeys signs people in with something they have rather than something
they know.

A passkey is a key pair an authenticator holds — a phone, a laptop's secure
enclave, a hardware key — where the private half never leaves it and the public
half is all this server stores. There is nothing here worth stealing: a database
read yields public keys, which is what they are for.

Both ceremonies take two requests, one to issue a challenge and one to answer
it, and the challenge is remembered on this side in between. That is the whole
security property — a challenge the caller keeps for itself proves nothing —
which is why it lives in the database rather than in anything the caller holds.
*/
package passkeys

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
ErrCeremonyInvalid covers every way an answer can fail to match a challenge this
server issued: unknown, already answered, expired, or issued for the other
ceremony. One error, for the same reason authentication failures are one error.
*/
var ErrCeremonyInvalid = errors.New("this did not come from here, or has expired")

// ErrNoPasskey reports an assertion for a credential this server does not hold.
var ErrNoPasskey = errors.New("that passkey is not registered here")

// ErrNotEnabled reports a server whose operator has not configured passkeys,
// which needs a canonical address to be the relying party.
var ErrNotEnabled = errors.New("this server does not offer passkeys")

/*
ceremonyLifetime is how long somebody has to answer a challenge.

Long enough to find a hardware key in a drawer or reach for a phone across the
room, short enough that an abandoned ceremony is not a challenge lying about.
The library's own default session expiry is of the same order.
*/
const ceremonyLifetime = 5 * time.Minute

/*
Purposes, as stored. A registration challenge must not complete a sign-in, nor
the reverse.

Neither direction rests on this alone. A registration session carries a user id,
which the library refuses to accept for a discoverable login; a login session has
none, which the enrolment refuses to proceed without. Both of those are
consequences of the session's shape rather than statements about what a ceremony
was for, so the purpose is recorded as well — it is the one of the three that
says the thing directly, and the one that will still be right when the other two
stop being accidents.
*/
const (
	purposeRegister = "register"
	purposeLogin    = "login"
)

// Store is the database surface this package needs.
type Store interface {
	StartWebauthnCeremony(ctx context.Context, arg dbgen.StartWebauthnCeremonyParams) (dbgen.WebauthnCeremony, error)
	TakeWebauthnCeremony(ctx context.Context, arg dbgen.TakeWebauthnCeremonyParams) (dbgen.WebauthnCeremony, error)
	DeleteExpiredWebauthnCeremonies(ctx context.Context) (int64, error)

	CreatePasskeyCredential(ctx context.Context, arg dbgen.CreatePasskeyCredentialParams) (dbgen.Credential, error)
	GetPasskeyByCredentialID(ctx context.Context, externalID *string) (dbgen.Credential, error)
	ListPasskeysForUser(ctx context.Context, userID uuid.UUID) ([]dbgen.Credential, error)
	RenamePasskey(ctx context.Context, arg dbgen.RenamePasskeyParams) (dbgen.Credential, error)
	TouchCredential(ctx context.Context, arg dbgen.TouchCredentialParams) error
	DeleteCredential(ctx context.Context, arg dbgen.DeleteCredentialParams) error
	CountCredentialsForUser(ctx context.Context, userID uuid.UUID) (int64, error)

	EnsureWebauthnHandle(ctx context.Context, arg dbgen.EnsureWebauthnHandleParams) ([]byte, error)
	GetUserByWebauthnHandle(ctx context.Context, webauthnHandle []byte) (dbgen.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (dbgen.User, error)
}

// Service runs the two WebAuthn ceremonies.
type Service struct {
	web   *webauthn.WebAuthn
	store Store
	log   *slog.Logger
}

/*
Settings describe this server as a relying party.

A passkey is bound to the relying party's identifier, which means bound to a
hostname: one enrolled at chat.example.org cannot be used anywhere else, and
this is the whole point rather than an inconvenience. It follows that the
identifier must be correct and stable — changing it invalidates every passkey
already enrolled — so it is taken from the operator's canonical address and
never from a request.
*/
type Settings struct {
	// DisplayName is what an authenticator shows when it asks.
	DisplayName string
	// CanonicalAddress is where this server is, as the operator set it.
	CanonicalAddress string
	// Origins the browser may present. Derived from the canonical address, with
	// any extra the operator allows for a client served from elsewhere.
	Origins []string
}

/*
NewService builds the service, or nothing.

Nil where passkeys cannot work — a server with no canonical address has no
relying party identifier to bind them to, and enrolling against a guess would
produce passkeys that stop working the moment the guess changes. Callers treat
nil as "this server does not offer passkeys", which is a working state.
*/
func NewService(settings Settings, store Store, log *slog.Logger) (*Service, error) {
	identifier := relyingPartyID(settings.CanonicalAddress)
	if identifier == "" {
		return nil, nil
	}

	origins := settings.Origins
	if len(origins) == 0 {
		origins = []string{"https://" + identifier}
	}

	name := settings.DisplayName
	if name == "" {
		name = identifier
	}

	web, err := webauthn.New(&webauthn.Config{
		RPID:          identifier,
		RPDisplayName: name,
		RPOrigins:     origins,
	})
	if err != nil {
		return nil, fmt.Errorf("configure passkeys: %w", err)
	}

	return &Service{web: web, store: store, log: log}, nil
}

// Enabled says whether this server signs people in with passkeys.
func (s *Service) Enabled() bool { return s != nil && s.web != nil }

/*
account adapts a TDC account to what the library expects of a user.

The handle is deliberately not the account's id: that appears in URLs, in
responses and in the audit log, and WebAuthn asks for a value that cannot be
correlated with anything else.
*/
type account struct {
	handle      []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (a account) WebAuthnID() []byte                         { return a.handle }
func (a account) WebAuthnName() string                       { return a.name }
func (a account) WebAuthnDisplayName() string                { return a.displayName }
func (a account) WebAuthnCredentials() []webauthn.Credential { return a.credentials }

// challengeKey is what a ceremony is looked up by. SHA-256 rather than a
// password hash: the input is the library's own random challenge, so there is
// no dictionary to slow down.
func challengeKey(challenge string) []byte {
	sum := sha256.Sum256([]byte(challenge))
	return sum[:]
}

// remember stores a ceremony until its answer arrives.
func (s *Service) remember(
	ctx context.Context,
	session *webauthn.SessionData,
	purpose string,
	userID *uuid.UUID,
) error {
	encoded, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode the ceremony: %w", err)
	}

	_, err = s.store.StartWebauthnCeremony(ctx, dbgen.StartWebauthnCeremonyParams{
		ID:            uuid.New(),
		ChallengeHash: challengeKey(session.Challenge),
		Session:       encoded,
		Purpose:       purpose,
		UserID:        userID,
		ExpiresAt:     time.Now().Add(ceremonyLifetime),
	})
	if err != nil {
		return fmt.Errorf("remember the ceremony: %w", err)
	}
	return nil
}

// recall takes the ceremony a response answers, consuming it.
func (s *Service) recall(
	ctx context.Context,
	challenge, purpose string,
) (webauthn.SessionData, dbgen.WebauthnCeremony, error) {
	record, err := s.store.TakeWebauthnCeremony(ctx, dbgen.TakeWebauthnCeremonyParams{
		ChallengeHash: challengeKey(challenge),
		Purpose:       purpose,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return webauthn.SessionData{}, dbgen.WebauthnCeremony{}, ErrCeremonyInvalid
	}
	if err != nil {
		return webauthn.SessionData{}, dbgen.WebauthnCeremony{}, fmt.Errorf("take the ceremony: %w", err)
	}

	var session webauthn.SessionData
	if err := json.Unmarshal(record.Session, &session); err != nil {
		return webauthn.SessionData{}, dbgen.WebauthnCeremony{}, fmt.Errorf("decode the ceremony: %w", err)
	}

	return session, record, nil
}

// SweepExpired removes ceremonies nobody answered.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	if !s.Enabled() {
		return 0, nil
	}
	return s.store.DeleteExpiredWebauthnCeremonies(ctx)
}

// challengeFrom reads the challenge out of a response, which is how the
// ceremony it answers is found. The library parses the response again itself;
// this only needs the one value to look the session up by.
func challengeFrom(request *http.Request) (string, *protocol.ParsedCredentialCreationData, error) {
	parsed, err := protocol.ParseCredentialCreationResponse(request)
	if err != nil {
		return "", nil, ErrCeremonyInvalid
	}
	return parsed.Response.CollectedClientData.Challenge, parsed, nil
}
