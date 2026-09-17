package passkeys_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/passkeys"
)

/*
These tests drive real ceremonies against a real database.

An authenticator in software rather than a recorded exchange, because what is
being checked is whether this server verifies a signature it did not produce —
and a fixture that agrees with itself checks nothing. The authenticator here
holds its own keys and signs with them, so a server that skipped verification
would pass a replayed fixture and fail this.
*/

const testOrigin = "https://chat.example.test"

// relyingParty is what the authenticator believes it is talking to. It must
// match what the service was configured with, because that agreement is the
// property WebAuthn rests on.
var relyingParty = virtualwebauthn.RelyingParty{
	Name:   "Test Server",
	ID:     "chat.example.test",
	Origin: testOrigin,
}

// harness is a service over a real database, with an account to enrol against.
type harness struct {
	service *passkeys.Service
	queries *dbgen.Queries
	user    dbgen.User
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	url := dbtest.URL(t, "passkeys")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, url, quiet); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, db.Config{URL: url}, quiet)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clean := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		// Credentials and ceremonies both cascade from users.
		if _, err := pool.Exec(cleanCtx, "DELETE FROM users"); err != nil {
			t.Errorf("clean: %v", err)
		}
	}
	clean()
	t.Cleanup(func() {
		clean()
		pool.Close()
	})

	queries := dbgen.New(pool)

	service, err := passkeys.NewService(passkeys.Settings{
		DisplayName:      relyingParty.Name,
		CanonicalAddress: relyingParty.ID,
		Origins:          []string{testOrigin},
	}, queries, quiet)
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}
	if service == nil {
		t.Fatal("a server with a canonical address offers no passkeys")
	}

	user, err := queries.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       "amina",
		UsernameFolded: "amina",
		DisplayName:    "Amina",
	})
	if err != nil {
		t.Fatalf("creating an account: %v", err)
	}

	return &harness{service: service, queries: queries, user: user}
}

// enrol runs a whole registration ceremony and returns the authenticator that
// now holds the passkey.
func (h *harness) enrol(t *testing.T, label string) (virtualwebauthn.Authenticator, virtualwebauthn.Credential) {
	t.Helper()

	authenticator := virtualwebauthn.NewAuthenticator()
	credential := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	enrolment, err := h.service.BeginEnrolment(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("beginning enrolment: %v", err)
	}

	options, err := virtualwebauthn.ParseAttestationOptions(asJSON(t, enrolment.Options))
	if err != nil {
		t.Fatalf("parsing the enrolment options: %v", err)
	}

	answer := virtualwebauthn.CreateAttestationResponse(
		relyingParty, authenticator, credential, *options)

	if _, err := h.service.FinishEnrolment(context.Background(), postJSON(answer), label); err != nil {
		t.Fatalf("finishing enrolment: %v", err)
	}

	// What the authenticator now holds, and who it holds it for. The handle is
	// what makes a sign-in with nothing typed possible.
	authenticator.AddCredential(credential)
	authenticator.Options.UserHandle = h.handle(t)

	return authenticator, credential
}

// handle reads the WebAuthn user handle the service established.
func (h *harness) handle(t *testing.T) []byte {
	t.Helper()

	user, err := h.queries.GetUserByID(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("reloading the account: %v", err)
	}
	if len(user.WebauthnHandle) == 0 {
		t.Fatal("enrolling a passkey established no handle, so nothing can sign in with it")
	}
	return user.WebauthnHandle
}

// signIn runs a whole discoverable sign-in with the given authenticator.
func (h *harness) signIn(
	t *testing.T,
	authenticator virtualwebauthn.Authenticator,
	credential virtualwebauthn.Credential,
) (dbgen.User, error) {
	t.Helper()

	assertion, err := h.service.BeginSignIn(context.Background())
	if err != nil {
		t.Fatalf("beginning sign-in: %v", err)
	}

	options, err := virtualwebauthn.ParseAssertionOptions(asJSON(t, assertion.Options))
	if err != nil {
		t.Fatalf("parsing the sign-in options: %v", err)
	}

	answer := virtualwebauthn.CreateAssertionResponse(
		relyingParty, authenticator, credential, *options)

	return h.service.FinishSignIn(context.Background(), postJSON(answer))
}

/*
TestAPasskeySignsSomebodyIn is the whole feature in one test.

Enrol, then sign in having named nobody: the authenticator says whose account it
is and the server believes it only because the signature verifies.
*/
func TestAPasskeySignsSomebodyIn(t *testing.T) {
	h := newHarness(t)
	authenticator, credential := h.enrol(t, "Phone")

	signedIn, err := h.signIn(t, authenticator, credential)
	if err != nil {
		t.Fatalf("signing in with a passkey: %v", err)
	}
	if signedIn.ID != h.user.ID {
		t.Errorf("signed in as %s, want %s", signedIn.Username, h.user.Username)
	}
}

/*
TestSomebodyElsesAuthenticatorIsRefused.

The same account handle, a credential this server never saw — which is what an
attacker with their own authenticator and a guessed handle would present.

Two things refuse it: the handler here, which checks the credential belongs to
the account the handle named, and the library, which checks the same thing
again before it verifies. Either alone would be enough, and the test passes with
either removed; both are kept because the one that matters is whichever survives
a later refactor.
*/
func TestSomebodyElsesAuthenticatorIsRefused(t *testing.T) {
	h := newHarness(t)
	authenticator, _ := h.enrol(t, "Phone")

	// An authenticator holding a credential nobody enrolled, presenting the
	// enrolled account's handle.
	impostor := virtualwebauthn.NewAuthenticator()
	stranger := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	impostor.AddCredential(stranger)
	impostor.Options.UserHandle = authenticator.Options.UserHandle

	if _, err := h.signIn(t, impostor, stranger); err == nil {
		t.Fatal("an authenticator holding a credential nobody enrolled signed in")
	}
}

/*
TestAChallengeIsAnsweredOnce.

The response is a bearer value until it is used: whoever replays it presents a
signature this server accepted before. The ceremony is consumed as it is read,
so the second attempt finds nothing.
*/
func TestAChallengeIsAnsweredOnce(t *testing.T) {
	h := newHarness(t)
	authenticator, credential := h.enrol(t, "Phone")

	assertion, err := h.service.BeginSignIn(context.Background())
	if err != nil {
		t.Fatalf("beginning sign-in: %v", err)
	}
	options, err := virtualwebauthn.ParseAssertionOptions(asJSON(t, assertion.Options))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	answer := virtualwebauthn.CreateAssertionResponse(
		relyingParty, authenticator, credential, *options)

	if _, err := h.service.FinishSignIn(context.Background(), postJSON(answer)); err != nil {
		t.Fatalf("first answer: %v", err)
	}

	_, err = h.service.FinishSignIn(context.Background(), postJSON(answer))
	if !errors.Is(err, passkeys.ErrCeremonyInvalid) {
		t.Errorf("replaying an answer gave %v, want a refusal", err)
	}
}

/*
TestAnEnrolmentChallengeCannotSignAnybodyIn.

The two ceremonies issue challenges that look alike. If the purpose were not
recorded, an enrolment challenge — which somebody signed in can ask for freely —
could be answered as a sign-in.
*/
func TestAnEnrolmentChallengeCannotSignAnybodyIn(t *testing.T) {
	h := newHarness(t)
	authenticator, credential := h.enrol(t, "Phone")

	enrolment, err := h.service.BeginEnrolment(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("beginning enrolment: %v", err)
	}

	// The enrolment's challenge, answered as though it were a sign-in.
	attestation, err := virtualwebauthn.ParseAttestationOptions(asJSON(t, enrolment.Options))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	assertion := virtualwebauthn.AssertionOptions{
		Challenge:        attestation.Challenge,
		RelyingPartyID:   attestation.RelyingPartyID,
		AllowCredentials: []string{base64.RawURLEncoding.EncodeToString(credential.ID)},
	}
	answer := virtualwebauthn.CreateAssertionResponse(
		relyingParty, authenticator, credential, assertion)

	_, err = h.service.FinishSignIn(context.Background(), postJSON(answer))
	if !errors.Is(err, passkeys.ErrCeremonyInvalid) {
		t.Errorf("an enrolment challenge answered as a sign-in gave %v, want a refusal", err)
	}
	t.Logf("refused with: %v", err)
}

// TestMoreThanOnePasskeyPerAccount, and either signs in — which is what lets
// somebody hold one on a phone and one on a laptop.
func TestMoreThanOnePasskeyPerAccount(t *testing.T) {
	h := newHarness(t)
	phone, phoneCredential := h.enrol(t, "Phone")
	laptop, laptopCredential := h.enrol(t, "Laptop")

	for name, attempt := range map[string]struct {
		authenticator virtualwebauthn.Authenticator
		credential    virtualwebauthn.Credential
	}{
		"the phone":  {phone, phoneCredential},
		"the laptop": {laptop, laptopCredential},
	} {
		t.Run(name, func(t *testing.T) {
			signedIn, err := h.signIn(t, attempt.authenticator, attempt.credential)
			if err != nil {
				t.Fatalf("signing in with %s: %v", name, err)
			}
			if signedIn.ID != h.user.ID {
				t.Errorf("signed in as the wrong account")
			}
		})
	}

	// And both are listed, with the names they were given.
	listed, err := h.service.List(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("got %d passkeys listed, want 2", len(listed))
	}

	labels := []string{listed[0].Label, listed[1].Label}
	for _, want := range []string{"Phone", "Laptop"} {
		if !contains(labels, want) {
			t.Errorf("the list is %v; expected it to name %q", labels, want)
		}
	}
}

/*
TestTheLastWayInCannotBeRemoved.

An account whose only passkey is revoked has no password to fall back on and no
recovery yet. It is an account nobody can reach, including the person who just
revoked it while tidying up.
*/
func TestTheLastWayInCannotBeRemoved(t *testing.T) {
	h := newHarness(t)
	h.enrol(t, "Only one")

	listed, err := h.service.List(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d passkeys, want 1", len(listed))
	}

	if err := h.service.Revoke(context.Background(), h.user.ID, listed[0].ID); !errors.Is(err, passkeys.ErrLastCredential) {
		t.Errorf("revoking the only way in gave %v, want a refusal", err)
	}

	// Still there, and still works.
	if remaining, _ := h.service.List(context.Background(), h.user.ID); len(remaining) != 1 {
		t.Error("the refused revocation removed it anyway")
	}
}

// TestARevokedPasskeyStopsWorking, which is the point of being able to revoke
// one at all.
func TestARevokedPasskeyStopsWorking(t *testing.T) {
	h := newHarness(t)
	phone, phoneCredential := h.enrol(t, "Phone")
	h.enrol(t, "Laptop")

	listed, err := h.service.List(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	var phoneRow dbgen.Credential
	for _, row := range listed {
		if row.Label == "Phone" {
			phoneRow = row
		}
	}
	if phoneRow.ID == uuid.Nil {
		t.Fatal("the phone's passkey is not in the list")
	}

	if err := h.service.Revoke(context.Background(), h.user.ID, phoneRow.ID); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	if _, err := h.signIn(t, phone, phoneCredential); err == nil {
		t.Error("a revoked passkey still signs somebody in")
	}
}

/*
TestAServerWithNoAddressOffersNoPasskeys.

A passkey is bound to a hostname, and a server with no canonical address has
none to bind to. Guessing one produces passkeys that enrol and then stop working
the moment the guess changes, so nothing is offered instead.
*/
func TestAServerWithNoAddressOffersNoPasskeys(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := passkeys.NewService(passkeys.Settings{DisplayName: "Nowhere"}, nil, quiet)
	if err != nil {
		t.Fatalf("building without an address: %v", err)
	}
	if service.Enabled() {
		t.Error("a server with no canonical address offers passkeys, which cannot work")
	}
}

// asJSON is what the browser would have been sent.
func asJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding options: %v", err)
	}
	return string(encoded)
}

// postJSON is what the browser would have sent back.
func postJSON(body string) *http.Request {
	request, _ := http.NewRequest(http.MethodPost, testOrigin, bytes.NewReader([]byte(body)))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

/*
TestASignInChallengeCannotEnrolAPasskey.

The other direction, and the one nothing else catches. A sign-in challenge can
be asked for by anybody at all — no session, no account named — so if it could be
answered as an enrolment, an unauthenticated caller would be adding a passkey.
To whose account is a separate question the enrolment path answers from the row;
the point is that the challenge must not be usable for this at all.

Two things refuse it, and this test fails only when both are removed: the
purpose recorded on the ceremony, and the enrolment's own refusal to proceed
without a user id — which a sign-in ceremony never has, since nobody is named
when it begins. Either alone is sufficient; both are kept because they fail for
different reasons, and a change that defeats one is unlikely to defeat the other.
*/
func TestASignInChallengeCannotEnrolAPasskey(t *testing.T) {
	h := newHarness(t)
	authenticator, _ := h.enrol(t, "Phone")

	assertion, err := h.service.BeginSignIn(context.Background())
	if err != nil {
		t.Fatalf("beginning sign-in: %v", err)
	}
	options, err := virtualwebauthn.ParseAssertionOptions(asJSON(t, assertion.Options))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	// An enrolment answer over the sign-in's challenge.
	fresh := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	answer := virtualwebauthn.CreateAttestationResponse(
		relyingParty, authenticator, fresh, virtualwebauthn.AttestationOptions{
			Challenge:        options.Challenge,
			RelyingPartyID:   options.RelyingPartyID,
			RelyingPartyName: relyingParty.Name,
			UserID:           base64.RawURLEncoding.EncodeToString(h.handle(t)),
			UserName:         h.user.Username,
			UserDisplayName:  h.user.DisplayName,
		})

	_, err = h.service.FinishEnrolment(context.Background(), postJSON(answer), "Sneaked in")
	if !errors.Is(err, passkeys.ErrCeremonyInvalid) {
		t.Errorf("a sign-in challenge answered as an enrolment gave %v, want a refusal", err)
	}

	// And nothing was added.
	listed, err := h.service.List(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("got %d passkeys, want the 1 that was genuinely enrolled", len(listed))
	}
}
