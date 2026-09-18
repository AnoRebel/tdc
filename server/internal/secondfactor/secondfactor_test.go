package secondfactor_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/dbtest"
	"github.com/AnoRebel/tdc/server/internal/secondfactor"
)

/*
These tests are about codes being good exactly once.

TOTP's weakness is that a code is valid for its whole window and one either
side, so the same six digits work for up to ninety seconds. Somebody who read
them over a shoulder, or off a page pretending to be this server, has that long
to use them — unless the server remembers which period it last accepted. Most of
what follows is that.
*/

type harness struct {
	service *secondfactor.Service
	queries *dbgen.Queries
	user    dbgen.User
	// at is where the service believes it stands, so a test can hold still
	// rather than race the window turning over.
	at *time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	url := dbtest.URL(t, "secondfactor")
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

	user, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       "amina",
		UsernameFolded: "amina",
		DisplayName:    "Amina",
	})
	if err != nil {
		t.Fatalf("creating an account: %v", err)
	}

	// A fixed moment, so a code generated here is checked in the same window.
	moment := time.Now()
	service := secondfactor.NewService("Test Server", queries, quiet)
	secondfactor.SetClock(service, func() time.Time { return moment })

	return &harness{service: service, queries: queries, user: user, at: &moment}
}

// enrol sets up an authenticator app and returns its secret.
func (h *harness) enrol(t *testing.T) string {
	t.Helper()

	enrolment, err := h.service.BeginTOTP(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("beginning enrolment: %v", err)
	}

	if err := h.service.ConfirmTOTP(context.Background(), h.user.ID, h.codeAt(t, enrolment.Secret, *h.at)); err != nil {
		t.Fatalf("confirming enrolment: %v", err)
	}

	/*
		On to the next window before signing in.

		Confirming spends the period its code came from — that is the rule
		TestTheCodeThatConfirmedEnrolmentCannotSignIn exists for — so a test
		that then signed in within the same thirty seconds would be asserting
		the opposite of it. In practice nobody enrols and signs in inside one
		window; here it has to be said explicitly.
	*/
	*h.at = h.at.Add(30 * time.Second)

	return enrolment.Secret
}

// codeAt is what the app would show at a given moment.
func (h *harness) codeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()

	code, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{
		Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generating a code: %v", err)
	}
	return code
}

// begin starts a sign-in that has passed the first factor.
func (h *harness) begin(t *testing.T) secondfactor.Pending {
	t.Helper()

	pending, err := h.service.Begin(context.Background(), h.user.ID, "test", netip.MustParseAddr("192.0.2.10"))
	if err != nil {
		t.Fatalf("beginning a pending sign-in: %v", err)
	}
	return pending
}

/*
TestACodeWorksOnce is the whole point of this package.

A code is valid across three windows, so without recording which period was
accepted the same digits work again for up to ninety seconds. This is the test
that fails if that recording is removed.
*/
func TestACodeWorksOnce(t *testing.T) {
	h := newHarness(t)
	secret := h.enrol(t)

	code := h.codeAt(t, secret, *h.at)

	first := h.begin(t)
	if _, err := h.service.Complete(context.Background(), first.Ticket, code); err != nil {
		t.Fatalf("the first use of a code: %v", err)
	}

	// The same code, the same window, a fresh ticket. Still within the time it
	// would otherwise be valid for.
	second := h.begin(t)
	if _, err := h.service.Complete(context.Background(), second.Ticket, code); !errors.Is(err, secondfactor.ErrRejected) {
		t.Errorf("reusing a code gave %v, want a refusal", err)
	}
}

/*
TestTheCodeThatConfirmedEnrolmentCannotSignIn.

Proving the app works and using it to get in are two uses of one code. Without
recording the period at confirmation, somebody watching the enrolment — or a
log that captured it — would hold a code that still signs in.
*/
func TestTheCodeThatConfirmedEnrolmentCannotSignIn(t *testing.T) {
	h := newHarness(t)

	enrolment, err := h.service.BeginTOTP(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("beginning enrolment: %v", err)
	}

	code := h.codeAt(t, enrolment.Secret, *h.at)
	if err := h.service.ConfirmTOTP(context.Background(), h.user.ID, code); err != nil {
		t.Fatalf("confirming: %v", err)
	}

	pending := h.begin(t)
	if _, err := h.service.Complete(context.Background(), pending.Ticket, code); !errors.Is(err, secondfactor.ErrRejected) {
		t.Errorf("the confirming code signed somebody in: %v", err)
	}
}

/*
TestTheNextWindowsCodeStillWorks.

The counterpart to the replay rule, and the way to get it wrong: advancing past
the period the accepted code came from refuses codes that should work. A factor
that rejects correct codes is one people turn off, so this is as much a part of
being usable as the replay rule is of being safe.

Several windows are walked rather than one, because advancing by a single extra
period is invisible to one step — the next code is still later than the mark —
and shows up only when the overshoot accumulates.
*/
func TestTheNextWindowsCodeStillWorks(t *testing.T) {
	h := newHarness(t)
	secret := h.enrol(t)

	for window := 1; window <= 4; window++ {
		pending := h.begin(t)
		if _, err := h.service.Complete(context.Background(), pending.Ticket, h.codeAt(t, secret, *h.at)); err != nil {
			t.Fatalf("the code for window %d was refused: %v", window, err)
		}
		*h.at = h.at.Add(30 * time.Second)
	}
}

// TestARecoveryCodeIsSpent, which is what makes a printed list safe to carry.
func TestARecoveryCodeIsSpent(t *testing.T) {
	h := newHarness(t)
	h.enrol(t)

	codes, err := h.service.IssueRecoveryCodes(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("issuing codes: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}

	accepted, err := h.service.Complete(context.Background(), h.begin(t).Ticket, codes[0])
	if err != nil {
		t.Fatalf("using a recovery code: %v", err)
	}
	if !accepted.UsedRecoveryCode {
		t.Error("using a recovery code was not reported as such")
	}
	if accepted.RecoveryCodesLeft != 9 {
		t.Errorf("%d codes left, want 9", accepted.RecoveryCodesLeft)
	}

	// The same one again.
	if _, err := h.service.Complete(context.Background(), h.begin(t).Ticket, codes[0]); !errors.Is(err, secondfactor.ErrRejected) {
		t.Errorf("reusing a recovery code gave %v, want a refusal", err)
	}

	// And the others are untouched.
	if _, err := h.service.Complete(context.Background(), h.begin(t).Ticket, codes[1]); err != nil {
		t.Errorf("a different recovery code was refused: %v", err)
	}
}

// TestRecoveryCodesAreForgivingOfHowTheyAreTyped: read off paper, by hand.
func TestRecoveryCodesAreForgivingOfHowTheyAreTyped(t *testing.T) {
	h := newHarness(t)
	h.enrol(t)

	codes, err := h.service.IssueRecoveryCodes(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("issuing codes: %v", err)
	}

	for name, typed := range map[string]string{
		"in capitals":        strings.ToUpper(codes[0]),
		"with spaces":        strings.ReplaceAll(codes[1], "-", " "),
		"with no separator":  strings.ReplaceAll(codes[2], "-", ""),
		"with stray spacing": "  " + codes[3] + "  ",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.service.Complete(context.Background(), h.begin(t).Ticket, typed); err != nil {
				t.Errorf("a code %s was refused: %v", name, err)
			}
		})
	}
}

/*
TestIssuingNewCodesReplacesTheOldOnes.

Somebody holding a printed list expects that list to be the whole truth. An old
code that still worked would be a secret they believe they have destroyed.
*/
func TestIssuingNewCodesReplacesTheOldOnes(t *testing.T) {
	h := newHarness(t)
	h.enrol(t)

	old, err := h.service.IssueRecoveryCodes(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	if _, err := h.service.IssueRecoveryCodes(context.Background(), h.user.ID); err != nil {
		t.Fatalf("reissuing: %v", err)
	}

	if _, err := h.service.Complete(context.Background(), h.begin(t).Ticket, old[0]); !errors.Is(err, secondfactor.ErrRejected) {
		t.Errorf("a code from the replaced list still worked: %v", err)
	}
}

/*
TestGuessingIsBounded.

Six digits is a million possibilities, which unlimited attempts reduces to
patience. Counted against the ticket rather than the caller's address, because
the ticket is what identifies this attempt and an attacker can move.
*/
func TestGuessingIsBounded(t *testing.T) {
	h := newHarness(t)
	h.enrol(t)

	pending := h.begin(t)

	for attempt := 0; attempt < 5; attempt++ {
		if _, err := h.service.Complete(context.Background(), pending.Ticket, "000000"); !errors.Is(err, secondfactor.ErrRejected) {
			t.Fatalf("attempt %d gave %v, want a refusal", attempt+1, err)
		}
	}

	// Past the allowance, and now said differently — somebody mistyping their
	// own code needs to know why it stopped answering.
	if _, err := h.service.Complete(context.Background(), pending.Ticket, "000000"); !errors.Is(err, secondfactor.ErrTooManyAttempts) {
		t.Errorf("past the allowance gave %v, want too many attempts", err)
	}
}

/*
TestAWrongCodeDoesNotSpendTheTicket.

One mistyped digit must not mean signing in from the beginning. The ticket is
consumed when a code is accepted, not when one is tried.
*/
func TestAWrongCodeDoesNotSpendTheTicket(t *testing.T) {
	h := newHarness(t)
	secret := h.enrol(t)

	pending := h.begin(t)

	if _, err := h.service.Complete(context.Background(), pending.Ticket, "000000"); !errors.Is(err, secondfactor.ErrRejected) {
		t.Fatalf("a wrong code gave %v", err)
	}

	if _, err := h.service.Complete(context.Background(), pending.Ticket, h.codeAt(t, secret, *h.at)); err != nil {
		t.Errorf("the right code after a wrong one was refused: %v", err)
	}
}

/*
TestATicketBuysOneSignIn.

Spent when a code is accepted, so a ticket that leaked — from a shared machine,
a log, a browser history — cannot be turned into a second session by somebody
who also has a later code.

Deliberately a *different window's* code the second time. Presenting the same
code again is refused by the replay rule instead, which would make this test
pass whether the ticket were consumed or not.
*/
func TestATicketBuysOneSignIn(t *testing.T) {
	h := newHarness(t)
	secret := h.enrol(t)

	pending := h.begin(t)

	if _, err := h.service.Complete(context.Background(), pending.Ticket, h.codeAt(t, secret, *h.at)); err != nil {
		t.Fatalf("signing in: %v", err)
	}

	// A new window, so the code itself is unimpeachable and only the ticket is
	// in question.
	*h.at = h.at.Add(30 * time.Second)

	if _, err := h.service.Complete(context.Background(), pending.Ticket, h.codeAt(t, secret, *h.at)); !errors.Is(err, secondfactor.ErrRejected) {
		t.Errorf("a spent ticket accepted a fresh code: %v", err)
	}
}

/*
TestAnUnconfirmedSecretAsksNothing.

Enrolment is two steps, and between them a row exists that must not be required
at sign-in — otherwise a scan that silently failed locks somebody out of their
own account.
*/
func TestAnUnconfirmedSecretAsksNothing(t *testing.T) {
	h := newHarness(t)

	if _, err := h.service.BeginTOTP(context.Background(), h.user.ID); err != nil {
		t.Fatalf("beginning enrolment: %v", err)
	}

	required, err := h.service.Required(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("asking whether a factor is required: %v", err)
	}
	if required {
		t.Error("an enrolment nobody finished is being required at sign-in")
	}
}

// TestAConfirmedSecretIsRequired, which is the other half of the same rule.
func TestAConfirmedSecretIsRequired(t *testing.T) {
	h := newHarness(t)
	h.enrol(t)

	required, err := h.service.Required(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if !required {
		t.Error("a confirmed factor is not being required")
	}
}

/*
TestTurningItOffTakesTheRecoveryCodesWithIt.

Codes that recover access to a factor nobody has are a list of secrets serving
no purpose — and worse, ones somebody may still be carrying.
*/
func TestTurningItOffTakesTheRecoveryCodesWithIt(t *testing.T) {
	h := newHarness(t)
	h.enrol(t)

	if _, err := h.service.IssueRecoveryCodes(context.Background(), h.user.ID); err != nil {
		t.Fatalf("issuing: %v", err)
	}
	if err := h.service.DisableTOTP(context.Background(), h.user.ID); err != nil {
		t.Fatalf("disabling: %v", err)
	}

	remaining, err := h.service.RemainingRecoveryCodes(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d recovery codes survived the factor they recovered", remaining)
	}
}

// TestASecondAppIsRefused: two would both work, and removing the one somebody
// knows about would leave the other.
func TestASecondAppIsRefused(t *testing.T) {
	h := newHarness(t)
	h.enrol(t)

	if _, err := h.service.BeginTOTP(context.Background(), h.user.ID); !errors.Is(err, secondfactor.ErrAlreadyEnrolled) {
		t.Errorf("enrolling a second app gave %v, want a refusal", err)
	}
}
