package oauth_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/oauth"
)

/*
These tests are about one rule and the ways of getting it wrong.

A provider that signs somebody in has proved they control an account at the
provider. Whether that says anything about an account here depends entirely on
whether the provider verified the email, and the cost of getting it backwards is
an account takeover: anybody able to set an unverified address at a permissive
provider could otherwise sign in as whoever holds that address here.
*/

// accountsDouble is the account surface, in memory, so these tests are about
// the decision rather than about the database.
type accountsDouble struct {
	credentials []dbgen.Credential
	usersByID   map[uuid.UUID]dbgen.User
	byEmail     map[string]dbgen.User
	// created records what was attached, so a test can tell "decided to link"
	// from "actually linked".
	created []dbgen.CreateOAuthCredentialParams
}

func newAccounts() *accountsDouble {
	return &accountsDouble{
		usersByID: map[uuid.UUID]dbgen.User{},
		byEmail:   map[string]dbgen.User{},
	}
}

func (a *accountsDouble) withUser(username, email string) dbgen.User {
	user := dbgen.User{ID: uuid.New(), Username: username, Email: &email}
	a.usersByID[user.ID] = user
	if email != "" {
		a.byEmail[email] = user
	}
	return user
}

func (a *accountsDouble) GetOAuthCredential(_ context.Context, arg dbgen.GetOAuthCredentialParams) (dbgen.Credential, error) {
	for _, credential := range a.credentials {
		if credential.Provider != nil && *credential.Provider == *arg.Provider &&
			credential.ExternalID != nil && *credential.ExternalID == *arg.ExternalID {
			return credential, nil
		}
	}
	return dbgen.Credential{}, pgx.ErrNoRows
}

func (a *accountsDouble) CreateOAuthCredential(_ context.Context, arg dbgen.CreateOAuthCredentialParams) (dbgen.Credential, error) {
	a.created = append(a.created, arg)
	credential := dbgen.Credential{
		ID: arg.ID, UserID: arg.UserID, Kind: "oauth2",
		ExternalID: arg.ExternalID, Provider: arg.Provider,
		ExternalEmail: arg.ExternalEmail, ExternalEmailVerified: arg.ExternalEmailVerified,
	}
	a.credentials = append(a.credentials, credential)
	return credential, nil
}

func (a *accountsDouble) UpdateOAuthCredentialEmail(_ context.Context, arg dbgen.UpdateOAuthCredentialEmailParams) error {
	for i, credential := range a.credentials {
		if credential.ID == arg.ID {
			a.credentials[i].ExternalEmail = arg.ExternalEmail
			a.credentials[i].ExternalEmailVerified = arg.ExternalEmailVerified
			return nil
		}
	}
	return pgx.ErrNoRows
}

func (a *accountsDouble) GetUserByEmail(_ context.Context, emailFolded *string) (dbgen.User, error) {
	if emailFolded == nil {
		return dbgen.User{}, pgx.ErrNoRows
	}
	if user, ok := a.byEmail[*emailFolded]; ok {
		return user, nil
	}
	return dbgen.User{}, pgx.ErrNoRows
}

func (a *accountsDouble) GetUserByID(_ context.Context, id uuid.UUID) (dbgen.User, error) {
	if user, ok := a.usersByID[id]; ok {
		return user, nil
	}
	return dbgen.User{}, pgx.ErrNoRows
}

func testService() *oauth.Service {
	return oauth.NewService(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func completed(identity oauth.Identity, userID *uuid.UUID) oauth.Completed {
	return oauth.Completed{Identity: identity, UserID: userID}
}

/*
TestAnUnverifiedEmailNeverLinks is the one that matters.

The email matches an existing account exactly. The provider has not verified it.
Linking here would hand that account to whoever typed the address at the
provider, which is why this must resolve to a new account and touch nothing.
*/
func TestAnUnverifiedEmailNeverLinks(t *testing.T) {
	accounts := newAccounts()
	victim := accounts.withUser("asha", "asha@example.test")

	resolution, err := testService().Resolve(context.Background(), accounts, completed(oauth.Identity{
		Provider:      "forgejo",
		Subject:       "impostor-9",
		Email:         "asha@example.test",
		EmailVerified: false,
	}, nil))
	if err != nil {
		t.Fatalf("resolving an unverified identity: %v", err)
	}

	if resolution.Outcome != oauth.OutcomeNewAccount {
		t.Errorf("an unverified email matching %s resolved to %q; it must resolve to a new account",
			*victim.Email, resolution.Outcome)
	}
	if resolution.User.ID != uuid.Nil() || resolution.LinkTo.ID != uuid.Nil() {
		t.Error("an unverified email produced an account to sign in to or link")
	}
	if len(accounts.created) != 0 {
		t.Error("an unverified email attached a credential")
	}
}

/*
TestAVerifiedEmailIsOfferedNotTaken.

The spec asks for a prompt, not an automatic link, and the difference is real:
the provider proved control of an address, not of the account here that happens
to use it. The person holding that account is the one who decides.
*/
func TestAVerifiedEmailIsOfferedNotTaken(t *testing.T) {
	accounts := newAccounts()
	existing := accounts.withUser("asha", "asha@example.test")

	resolution, err := testService().Resolve(context.Background(), accounts, completed(oauth.Identity{
		Provider:      "forgejo",
		Subject:       "asha-1",
		Email:         "asha@example.test",
		EmailVerified: true,
	}, nil))
	if err != nil {
		t.Fatalf("resolving a verified identity: %v", err)
	}

	if resolution.Outcome != oauth.OutcomeLinkOffered {
		t.Fatalf("a verified match resolved to %q, want a link offer", resolution.Outcome)
	}
	if resolution.LinkTo.ID != existing.ID {
		t.Error("the link was offered for the wrong account")
	}
	if len(accounts.created) != 0 {
		t.Error("the link offer attached a credential; it must wait to be accepted")
	}
	if resolution.User.ID != uuid.Nil() {
		t.Error("a link offer signed somebody in before they accepted it")
	}
}

// TestAnAttachedIdentitySignsIn: once a credential exists, it decides, and no
// email is consulted at all.
func TestAnAttachedIdentitySignsIn(t *testing.T) {
	accounts := newAccounts()
	user := accounts.withUser("asha", "asha@example.test")
	service := testService()

	identity := oauth.Identity{
		Provider: "forgejo", Subject: "asha-1",
		Email: "asha@example.test", EmailVerified: true,
	}
	if _, err := service.Attach(context.Background(), accounts, user.ID, identity); err != nil {
		t.Fatalf("attaching: %v", err)
	}

	resolution, err := service.Resolve(context.Background(), accounts, completed(identity, nil))
	if err != nil {
		t.Fatalf("resolving an attached identity: %v", err)
	}
	if resolution.Outcome != oauth.OutcomeSignedIn {
		t.Errorf("an attached identity resolved to %q, want a sign-in", resolution.Outcome)
	}
	if resolution.User.ID != user.ID {
		t.Error("signed in as the wrong account")
	}
}

/*
TestTheEmailMovingDoesNotMoveTheAccount.

The credential is keyed on the provider's subject, which is the only field a
provider promises not to reuse. If it were keyed on the email, somebody changing
their address at the provider would arrive here as a stranger — and worse, the
address they released could later sign in as them.
*/
func TestTheEmailMovingDoesNotMoveTheAccount(t *testing.T) {
	accounts := newAccounts()
	user := accounts.withUser("asha", "asha@example.test")
	service := testService()

	if _, err := service.Attach(context.Background(), accounts, user.ID, oauth.Identity{
		Provider: "forgejo", Subject: "asha-1",
		Email: "asha@example.test", EmailVerified: true,
	}); err != nil {
		t.Fatalf("attaching: %v", err)
	}

	// Same person at the provider, new address.
	resolution, err := service.Resolve(context.Background(), accounts, completed(oauth.Identity{
		Provider: "forgejo", Subject: "asha-1",
		Email: "asha@newplace.test", EmailVerified: true,
	}, nil))
	if err != nil {
		t.Fatalf("resolving after the email changed: %v", err)
	}
	if resolution.Outcome != oauth.OutcomeSignedIn || resolution.User.ID != user.ID {
		t.Errorf("changing the email at the provider resolved to %q; the subject decides, not the email",
			resolution.Outcome)
	}

	// And the new claim is recorded rather than left stale.
	if got := accounts.credentials[0].ExternalEmail; got == nil || *got != "asha@newplace.test" {
		t.Error("the provider's new claim was not recorded")
	}
}

// TestSomebodySignedInAddsAProvider covers the third way in: not a sign-in at
// all, but an account gaining another way to reach it.
func TestSomebodySignedInAddsAProvider(t *testing.T) {
	accounts := newAccounts()
	user := accounts.withUser("asha", "asha@example.test")

	resolution, err := testService().Resolve(context.Background(), accounts, completed(oauth.Identity{
		Provider: "gitlab", Subject: "asha-42",
		// Deliberately unverified: the account is already proven by the
		// session, so the email claim decides nothing here.
		Email: "someone.else@example.test", EmailVerified: false,
	}, &user.ID))
	if err != nil {
		t.Fatalf("adding a provider: %v", err)
	}
	if resolution.Outcome != oauth.OutcomeLinked {
		t.Errorf("adding a provider resolved to %q, want linked", resolution.Outcome)
	}
	if len(accounts.created) != 1 || accounts.created[0].UserID != user.ID {
		t.Error("the identity was not attached to the account that was signed in")
	}
}

/*
TestAnIdentityCannotBeTakenFromAnotherAccount.

Somebody signed in as one account presents an identity already attached to
another. Allowing it would either move the credential or leave two accounts
reachable by one provider identity, so it is refused and said out loud.
*/
func TestAnIdentityCannotBeTakenFromAnotherAccount(t *testing.T) {
	accounts := newAccounts()
	owner := accounts.withUser("asha", "asha@example.test")
	other := accounts.withUser("juma", "juma@example.test")
	service := testService()

	identity := oauth.Identity{Provider: "gitlab", Subject: "asha-42", Email: "asha@example.test", EmailVerified: true}
	if _, err := service.Attach(context.Background(), accounts, owner.ID, identity); err != nil {
		t.Fatalf("attaching: %v", err)
	}

	_, err := service.Resolve(context.Background(), accounts, completed(identity, &other.ID))
	if !errors.Is(err, oauth.ErrIdentityBelongsToAnother) {
		t.Errorf("taking another account's identity gave %v, want a refusal", err)
	}
}

// TestNoEmailAtAllIsANewAccount: a provider that asserts nothing decides
// nothing, which is the same as asserting something it has not verified.
func TestNoEmailAtAllIsANewAccount(t *testing.T) {
	accounts := newAccounts()
	accounts.withUser("asha", "asha@example.test")

	resolution, err := testService().Resolve(context.Background(), accounts, completed(oauth.Identity{
		Provider: "forgejo", Subject: "nobody-7",
	}, nil))
	if err != nil {
		t.Fatalf("resolving an identity with no email: %v", err)
	}
	if resolution.Outcome != oauth.OutcomeNewAccount {
		t.Errorf("an identity with no email resolved to %q", resolution.Outcome)
	}
}

// TestTheEmailIsFoldedBeforeMatching, so a provider shouting the address does
// not defeat the match — nor create a second account for the same person.
func TestTheEmailIsFoldedBeforeMatching(t *testing.T) {
	accounts := newAccounts()
	existing := accounts.withUser("asha", "asha@example.test")

	resolution, err := testService().Resolve(context.Background(), accounts, completed(oauth.Identity{
		Provider: "forgejo", Subject: "asha-1",
		Email: "  ASHA@Example.TEST ", EmailVerified: true,
	}, nil))
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if resolution.Outcome != oauth.OutcomeLinkOffered || resolution.LinkTo.ID != existing.ID {
		t.Errorf("a differently-cased address resolved to %q; addresses are folded before matching",
			resolution.Outcome)
	}
}
