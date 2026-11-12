package passkeys

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"uuid"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
relyingPartyID reduces the operator's canonical address to a hostname.

A passkey is bound to this value, so it must be the bare host: no scheme, no
port, no path. Getting it wrong does not fail loudly — it produces passkeys that
enrol and then refuse to work, which is the expensive kind of wrong.
*/
func relyingPartyID(canonical string) string {
	trimmed := strings.TrimSpace(canonical)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// Enrolment is a challenge for an authenticator to answer.
type Enrolment struct {
	// Options is what the browser passes to navigator.credentials.create.
	Options *protocol.CredentialCreation
}

/*
BeginEnrolment asks an authenticator to make a new passkey for this account.

Only ever for somebody already signed in: enrolling a passkey adds a way into an
account, so it is done by somebody who has already proved they hold it.
*/
func (s *Service) BeginEnrolment(ctx context.Context, userID uuid.UUID) (Enrolment, error) {
	if !s.Enabled() {
		return Enrolment{}, ErrNotEnabled
	}

	who, err := s.accountFor(ctx, userID)
	if err != nil {
		return Enrolment{}, err
	}

	options, session, err := s.web.BeginRegistration(who,
		/*
			A passkey rather than a second factor.

			Discoverable ("resident") means the authenticator stores who it is
			for, which is what lets somebody sign in having typed nothing. The
			preference is not a demand: an authenticator that cannot store one
			still enrols, and its owner types a username first.
		*/
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementPreferred,
			// Asking the authenticator to check it is really its owner — a
			// fingerprint, a face, a PIN. Preferred rather than required so
			// that a plain hardware key is still usable.
			UserVerification: protocol.VerificationPreferred,
		}),
		// Excluding what is already enrolled, so an authenticator says "you
		// already have one of these" rather than silently making a second.
		webauthn.WithExclusions(descriptorsFor(who.credentials)),
	)
	if err != nil {
		return Enrolment{}, fmt.Errorf("begin enrolment: %w", err)
	}

	if err := s.remember(ctx, session, purposeRegister, &userID); err != nil {
		return Enrolment{}, err
	}

	return Enrolment{Options: options}, nil
}

/*
FinishEnrolment stores the passkey an authenticator just made.

label is what somebody called it, so a list of three passkeys is three
distinguishable rows rather than three identical ones.
*/
func (s *Service) FinishEnrolment(
	ctx context.Context,
	request *http.Request,
	label string,
) (dbgen.Credential, error) {
	if !s.Enabled() {
		return dbgen.Credential{}, ErrNotEnabled
	}

	challenge, parsed, err := challengeFrom(request)
	if err != nil {
		return dbgen.Credential{}, err
	}

	session, record, err := s.recall(ctx, challenge, purposeRegister)
	if err != nil {
		return dbgen.Credential{}, err
	}
	if record.UserID == nil {
		// An enrolment with nobody to enrol for. Not something the routes can
		// produce, but the row is the authority on whose ceremony this was.
		return dbgen.Credential{}, ErrCeremonyInvalid
	}

	who, err := s.accountFor(ctx, *record.UserID)
	if err != nil {
		return dbgen.Credential{}, err
	}

	credential, err := s.web.CreateCredential(who, session, parsed)
	if err != nil {
		s.log.Info("a passkey enrolment did not verify", "error", err)
		return dbgen.Credential{}, ErrCeremonyInvalid
	}

	return s.keep(ctx, *record.UserID, label, credential)
}

// keep writes a verified credential, with everything the library may want back
// kept as JSON beside the columns that are indexed.
func (s *Service) keep(
	ctx context.Context,
	userID uuid.UUID,
	label string,
	credential *webauthn.Credential,
) (dbgen.Credential, error) {
	encoded, err := encodeCredential(credential)
	if err != nil {
		return dbgen.Credential{}, err
	}

	identifier := credentialID(credential.ID)

	stored, err := s.store.CreatePasskeyCredential(ctx, dbgen.CreatePasskeyCredentialParams{
		ID:         uuid.New(),
		UserID:     userID,
		Label:      strings.TrimSpace(label),
		Secret:     credential.PublicKey,
		ExternalID: &identifier,
		SignCount:  int64(credential.Authenticator.SignCount),
		Passkey:    encoded,
	})
	if err != nil {
		// The unique index on (kind, provider, external_id) is what makes this
		// safe: the same authenticator enrolled twice at once produces one row
		// and one refusal rather than two rows found by a check that passed in
		// both.
		return dbgen.Credential{}, fmt.Errorf("store the passkey: %w", err)
	}
	return stored, nil
}

// Assertion is a challenge for signing in.
type Assertion struct {
	Options *protocol.CredentialAssertion
}

/*
BeginSignIn asks whichever authenticator is present to prove an account.

Nothing is named. The authenticator offers what it holds for this server and the
account is known only once it answers, which is what makes a passkey sign-in one
gesture rather than a username and then a gesture.
*/
func (s *Service) BeginSignIn(ctx context.Context) (Assertion, error) {
	if !s.Enabled() {
		return Assertion{}, ErrNotEnabled
	}

	options, session, err := s.web.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationPreferred),
	)
	if err != nil {
		return Assertion{}, fmt.Errorf("begin sign-in: %w", err)
	}

	if err := s.remember(ctx, session, purposeLogin, nil); err != nil {
		return Assertion{}, err
	}

	return Assertion{Options: options}, nil
}

/*
FinishSignIn verifies an assertion and says whose account it proved.

The account comes from the credential rather than from anything the caller said,
which is the point: a caller who could name the account would be choosing whose
account an authenticator's answer applies to.
*/
func (s *Service) FinishSignIn(ctx context.Context, request *http.Request) (dbgen.User, error) {
	if !s.Enabled() {
		return dbgen.User{}, ErrNotEnabled
	}

	parsed, err := protocol.ParseCredentialRequestResponse(request)
	if err != nil {
		return dbgen.User{}, ErrCeremonyInvalid
	}

	session, _, err := s.recall(ctx, parsed.Response.CollectedClientData.Challenge, purposeLogin)
	if err != nil {
		return dbgen.User{}, err
	}

	var signedIn dbgen.User

	credential, err := s.web.ValidateDiscoverableLogin(
		func(rawID, handle []byte) (webauthn.User, error) {
			user, who, err := s.byHandle(ctx, handle, rawID)
			if err != nil {
				return nil, err
			}
			signedIn = user
			return who, nil
		},
		session,
		parsed,
	)
	if err != nil {
		s.log.Info("a passkey sign-in did not verify", "error", err)
		return dbgen.User{}, ErrCeremonyInvalid
	}

	/*
		The signature counter, which is how a cloned authenticator is noticed.

		A genuine authenticator's counter only goes up. One that goes backwards
		means two things are answering for the same credential, and the library
		reports it; recording the new value is what makes the next comparison
		meaningful.
	*/
	if err := s.touch(ctx, credential); err != nil {
		return dbgen.User{}, err
	}

	return signedIn, nil
}

// byHandle resolves the account an authenticator named, and checks the
// credential it used is one of that account's.
func (s *Service) byHandle(ctx context.Context, handle, rawID []byte) (dbgen.User, webauthn.User, error) {
	user, err := s.storeUserByHandle(ctx, handle)
	if err != nil {
		return dbgen.User{}, nil, err
	}

	who, err := s.accountFor(ctx, user.ID)
	if err != nil {
		return dbgen.User{}, nil, err
	}

	// The credential must belong to the account the handle named. Without this
	// a handle and a credential from two different accounts would be accepted
	// together.
	identifier := credentialID(rawID)
	for _, credential := range who.credentials {
		if credentialID(credential.ID) == identifier {
			return user, who, nil
		}
	}

	return dbgen.User{}, nil, ErrNoPasskey
}

func (s *Service) storeUserByHandle(ctx context.Context, handle []byte) (dbgen.User, error) {
	user, err := s.store.GetUserByWebauthnHandle(ctx, handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, ErrNoPasskey
	}
	if err != nil {
		return dbgen.User{}, fmt.Errorf("find the account that handle names: %w", err)
	}
	return user, nil
}

// touch records the authenticator's new signature counter.
func (s *Service) touch(ctx context.Context, credential *webauthn.Credential) error {
	identifier := credentialID(credential.ID)

	stored, err := s.store.GetPasskeyByCredentialID(ctx, &identifier)
	if err != nil {
		return fmt.Errorf("find the passkey that just signed in: %w", err)
	}

	return s.store.TouchCredential(ctx, dbgen.TouchCredentialParams{
		ID:        stored.ID,
		SignCount: int64(credential.Authenticator.SignCount),
	})
}

// credentialID is how an authenticator's credential identifier is stored: text,
// because the column it shares with OAuth2 subjects is text.
func credentialID(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// newHandle is a fresh WebAuthn user handle: random, opaque, and unrelated to
// anything else about the account.
func newHandle() ([]byte, error) {
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	return handle, nil
}

// descriptorsFor is the list an authenticator checks against so it does not
// enrol a second passkey where it already holds one.
func descriptorsFor(credentials []webauthn.Credential) []protocol.CredentialDescriptor {
	descriptors := make([]protocol.CredentialDescriptor, 0, len(credentials))
	for _, credential := range credentials {
		descriptors = append(descriptors, credential.Descriptor())
	}
	return descriptors
}
