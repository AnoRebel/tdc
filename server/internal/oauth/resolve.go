package oauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	identitypkg "github.com/AnoRebel/tdc/server/internal/identity"
)

// Accounts is the account surface resolving an identity needs. It is the
// identity service's, narrowed to what is used here.
type Accounts interface {
	GetOAuthCredential(ctx context.Context, arg dbgen.GetOAuthCredentialParams) (dbgen.Credential, error)
	CreateOAuthCredential(ctx context.Context, arg dbgen.CreateOAuthCredentialParams) (dbgen.Credential, error)
	UpdateOAuthCredentialEmail(ctx context.Context, arg dbgen.UpdateOAuthCredentialEmailParams) error
	GetUserByEmail(ctx context.Context, emailFolded *string) (dbgen.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (dbgen.User, error)
}

// Outcome says what a completed sign-in turned out to be.
type Outcome string

const (
	// OutcomeSignedIn: this identity is already attached to an account, so the
	// person is signed in to it. Nothing is decided from an email here — the
	// credential itself is the answer.
	OutcomeSignedIn Outcome = "signed-in"

	// OutcomeLinkOffered: no credential yet, but the provider verified an email
	// that belongs to an existing account. The person is asked whether to
	// attach this identity to it. Deliberately not done for them: the provider
	// proved control of an address, not of the account here that happens to use
	// it, and the person holding that account is the one who gets to decide.
	OutcomeLinkOffered Outcome = "link-offered"

	// OutcomeNewAccount: nothing matched, so there is an account to create. The
	// suggestions from the provider are offered and the person confirms them,
	// because a username taken here or a display name they would not choose
	// should not be settled without them.
	OutcomeNewAccount Outcome = "new-account"

	// OutcomeLinked: somebody already signed in added this provider to their
	// account.
	OutcomeLinked Outcome = "linked"
)

// ErrIdentityBelongsToAnother is returned when somebody signed in tries to
// attach a provider identity that is already on a different account. Attaching
// it would either move the credential or create a second route into the first
// account, and neither is something to do quietly.
var ErrIdentityBelongsToAnother = errors.New("that provider account is already attached to another account here")

// Resolution is what happened, and what the caller should do next.
type Resolution struct {
	Outcome Outcome
	// User is set for every outcome except OutcomeNewAccount, where there is no
	// account yet, and OutcomeLinkOffered, where there is one but the person
	// has not agreed to use it.
	User dbgen.User
	// Identity is what the provider said, carried through so a caller offering
	// a link or a new account has the suggestions to show.
	Identity Identity
	// LinkTo is the account a link is being offered for, set only on
	// OutcomeLinkOffered.
	LinkTo dbgen.User
}

/*
Resolve decides what a completed sign-in means.

This is where the linking rule lives, and the order of the checks is the rule:

  - An existing credential wins outright. It was created by somebody who proved
    they held the account, and it is keyed on the provider's own identifier, so
    it does not move when an email does.

  - Somebody already signed in is adding a provider, and the identity is
    attached to them and to nobody else.

  - A verified email matching an account offers a link. It does not take one:
    the provider proved control of an address, not of the account here.

  - Everything else is a new account, including an unverified email that
    matches one exactly. That last case is the whole point — auto-linking there
    would let anybody who can set an unverified address at a permissive provider
    sign in as whoever holds that address here.
*/
func (s *Service) Resolve(ctx context.Context, accounts Accounts, completed Completed) (Resolution, error) {
	identity := completed.Identity

	existing, err := accounts.GetOAuthCredential(ctx, dbgen.GetOAuthCredentialParams{
		Provider:   &identity.Provider,
		ExternalID: &identity.Subject,
	})
	switch {
	case err == nil:
		// Somebody already signed in, adding a provider they had already added
		// to a different account.
		if completed.UserID != nil && existing.UserID != *completed.UserID {
			return Resolution{}, ErrIdentityBelongsToAnother
		}

		user, err := accounts.GetUserByID(ctx, existing.UserID)
		if err != nil {
			return Resolution{}, fmt.Errorf("load the account this identity belongs to: %w", err)
		}

		// The provider's claim may have changed since last time, so it is
		// refreshed rather than left at whatever it was.
		if err := accounts.UpdateOAuthCredentialEmail(ctx, dbgen.UpdateOAuthCredentialEmailParams{
			ID:                    existing.ID,
			ExternalEmail:         optional(identity.Email),
			ExternalEmailVerified: identity.EmailVerified,
		}); err != nil {
			return Resolution{}, fmt.Errorf("refresh what the provider says: %w", err)
		}

		outcome := OutcomeSignedIn
		if completed.UserID != nil {
			// Adding a provider already on this same account. Nothing to do,
			// but saying "linked" is truer than "signed in" for what happened.
			outcome = OutcomeLinked
		}
		return Resolution{Outcome: outcome, User: user, Identity: identity}, nil

	case !errors.Is(err, pgx.ErrNoRows):
		return Resolution{}, fmt.Errorf("look for an existing identity: %w", err)
	}

	// Somebody already signed in, adding a provider for the first time.
	if completed.UserID != nil {
		user, _, err := s.attach(ctx, accounts, *completed.UserID, identity)
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Outcome: OutcomeLinked, User: user, Identity: identity}, nil
	}

	/*
		An email nobody verified decides nothing.

		Not a link, and not a refusal either: it becomes a new account, exactly
		as if the provider had asserted no email at all. Refusing would let
		somebody discover which addresses have accounts here by watching which
		sign-ins fail.
	*/
	if identity.Email != "" && identity.EmailVerified {
		match, err := accounts.GetUserByEmail(ctx, optionalFolded(identity.Email))
		switch {
		case err == nil:
			return Resolution{Outcome: OutcomeLinkOffered, LinkTo: match, Identity: identity}, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return Resolution{}, fmt.Errorf("look for an account with that email: %w", err)
		}
	}

	return Resolution{Outcome: OutcomeNewAccount, Identity: identity}, nil
}

// Attach adds a provider identity to an account.
//
// Called for somebody already signed in, and for somebody who accepted an
// offered link after proving they hold the account the offer named. It is never
// called on the strength of a matching email alone.
func (s *Service) Attach(ctx context.Context, accounts Accounts, userID uuid.UUID, identity Identity) (dbgen.User, error) {
	user, _, err := s.attach(ctx, accounts, userID, identity)
	return user, err
}

func (s *Service) attach(
	ctx context.Context,
	accounts Accounts,
	userID uuid.UUID,
	identity Identity,
) (dbgen.User, dbgen.Credential, error) {
	user, err := accounts.GetUserByID(ctx, userID)
	if err != nil {
		return dbgen.User{}, dbgen.Credential{}, fmt.Errorf("load the account to attach to: %w", err)
	}

	provider := identity.Provider
	subject := identity.Subject

	credential, err := accounts.CreateOAuthCredential(ctx, dbgen.CreateOAuthCredentialParams{
		ID:                    uuid.New(),
		UserID:                userID,
		Label:                 label(identity),
		ExternalID:            &subject,
		Provider:              &provider,
		ExternalEmail:         optional(identity.Email),
		ExternalEmailVerified: identity.EmailVerified,
	})
	if err != nil {
		// The unique index on (kind, provider, external_id) is what makes this
		// safe under concurrency: two callbacks attaching the same identity at
		// once produce one credential and one refusal, rather than two
		// credentials found by a check that passed in both.
		return dbgen.User{}, dbgen.Credential{}, fmt.Errorf("attach the identity: %w", err)
	}

	return user, credential, nil
}

// label is what somebody sees in their list of credentials. The provider's own
// name for them where it gave one, so a list of three providers is three
// distinguishable rows rather than three identical ones.
func label(identity Identity) string {
	if identity.Email != "" {
		return identity.Email
	}
	if identity.Username != "" {
		return identity.Username
	}
	return identity.Provider
}

// optional turns a blank string into a null column, so "no email" is one value
// in the database rather than two.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// optionalFolded is the same, for the lower-cased column emails are looked up
// by.
func optionalFolded(value string) *string {
	// The identity package's rule, not a second one: two definitions of
	// "the same address" is how a linking bug gets in.
	folded := identitypkg.NormaliseEmail(value)
	if folded == "" {
		return nil
	}
	return &folded
}
