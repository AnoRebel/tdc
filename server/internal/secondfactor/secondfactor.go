/*
Package secondfactor asks for something held, after something known.

A password proves knowledge and can be read over a shoulder, guessed, or typed
into a page pretending to be this one. A second factor is the answer to that: a
code from an app on a device somebody has, or one of a printed list they keep.

Optional per account and requirable by an operator. Nothing here needs a paid
service — TOTP is a shared secret and some arithmetic, and the recovery codes
are random values this server generates.

Not asked of a passkey sign-in. A passkey already proves possession of the
device and, where the authenticator checks a fingerprint or a PIN, knowledge
too; asking again would be ceremony rather than security.
*/
package secondfactor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
ErrRejected is every way a second factor can fail to be accepted.

A wrong code, a replayed one, a spent recovery code, a ticket that has expired.
One error for all of them, and deliberately the same one a wrong password
produces at the layer above: telling somebody which half of a sign-in was right
tells them the password was right.
*/
var ErrRejected = errors.New("that code was not accepted")

// ErrTooManyAttempts reports a ticket that has been guessed at enough times.
// Distinguishable from a wrong code on purpose: somebody mistyping their own
// code needs to know why it stopped answering.
var ErrTooManyAttempts = errors.New("too many codes have been tried for this sign-in")

// ErrNotEnrolled reports an account with no second factor to ask for.
var ErrNotEnrolled = errors.New("no second factor is set up on this account")

// ErrAlreadyEnrolled reports a second TOTP secret on an account that has one.
// Two would be two apps that both work, where removing the one somebody knows
// about leaves the other.
var ErrAlreadyEnrolled = errors.New("this account already has an authenticator app set up")

/*
pendingLifetime is how long somebody has to enter a code.

Long enough to unlock a phone, find the app and read six digits — including for
somebody doing it slowly or for the first time. Short enough that a ticket left
behind on a shared machine is not a way in an hour later.
*/
const pendingLifetime = 10 * time.Minute

/*
maxAttempts is how many codes may be tried against one ticket.

Six digits is a million possibilities, so unlimited attempts would reduce the
factor to patience. Five is generous for somebody mistyping and useless to
somebody guessing.
*/
const maxAttempts = 5

/*
skew is how many thirty-second periods either side of now are accepted.

One, which is what every authenticator app assumes: a phone's clock drifts, and
somebody who starts typing at the end of a window finishes in the next one.
Widening it would make each code good for longer, which is the opposite of what
these are for.
*/
const skew = 1

// recoveryCodeCount is how many are issued at once. Ten is enough that losing a
// phone does not mean losing the account, and few enough to write down.
const recoveryCodeCount = 10

// Store is the database surface this package needs.
type Store interface {
	CreateTOTPCredential(ctx context.Context, arg dbgen.CreateTOTPCredentialParams) (dbgen.Credential, error)
	GetTOTPCredential(ctx context.Context, userID uuid.UUID) (dbgen.Credential, error)
	ConfirmTOTPCredential(ctx context.Context, arg dbgen.ConfirmTOTPCredentialParams) error
	AdvanceTOTPPeriod(ctx context.Context, arg dbgen.AdvanceTOTPPeriodParams) (dbgen.Credential, error)
	DeleteTOTPCredential(ctx context.Context, userID uuid.UUID) error

	CreateRecoveryCode(ctx context.Context, arg dbgen.CreateRecoveryCodeParams) (dbgen.Credential, error)
	CountUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) (int64, error)
	SpendRecoveryCode(ctx context.Context, arg dbgen.SpendRecoveryCodeParams) (dbgen.Credential, error)
	DeleteRecoveryCodes(ctx context.Context, userID uuid.UUID) error

	StartPendingSignIn(ctx context.Context, arg dbgen.StartPendingSignInParams) (dbgen.PendingSignIn, error)
	GetPendingSignIn(ctx context.Context, ticketHash []byte) (dbgen.PendingSignIn, error)
	CountPendingAttempt(ctx context.Context, id uuid.UUID) (dbgen.PendingSignIn, error)
	FinishPendingSignIn(ctx context.Context, ticketHash []byte) (dbgen.PendingSignIn, error)
	DeleteExpiredPendingSignIns(ctx context.Context) (int64, error)

	HasConfirmedSecondFactor(ctx context.Context, userID uuid.UUID) (bool, error)
	HasPasskey(ctx context.Context, userID uuid.UUID) (bool, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (dbgen.User, error)
}

// Service enrols second factors and checks them.
type Service struct {
	// issuer is what an authenticator app shows beside the account, so somebody
	// with several can tell which server a code belongs to.
	issuer string
	store  Store
	log    *slog.Logger
	// now is time.Now, replaceable so a test can stand at a known moment
	// rather than generating a code and hoping the window does not turn over
	// between making it and checking it.
	now func() time.Time
}

// NewService builds the service. issuer is what appears in the authenticator
// app, which should be what the server calls itself.
func NewService(issuer string, store Store, log *slog.Logger) *Service {
	if strings.TrimSpace(issuer) == "" {
		issuer = "TDC"
	}
	return &Service{issuer: issuer, store: store, log: log, now: time.Now}
}

// Required reports whether an account has a second factor at all.
func (s *Service) Required(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.store.HasConfirmedSecondFactor(ctx, userID)
}

/*
Offers says which second factors an account can actually answer with.

A client needs it to draw the right prompt: asking somebody for six digits when
what they have is a passkey is asking for something they cannot produce, and the
only clue would be that nothing they try works.
*/
type Offers struct {
	// Code is an authenticator app, or a recovery code from its list.
	Code bool
	// Passkey is a device they already carry.
	Passkey bool
}

// Available says what this account can answer a challenge with.
func (s *Service) Available(ctx context.Context, userID uuid.UUID) (Offers, error) {
	credential, err := s.store.GetTOTPCredential(ctx, userID)
	code := err == nil && credential.ConfirmedAt != nil
	if err != nil && !notFound(err) {
		return Offers{}, fmt.Errorf("look for an authenticator app: %w", err)
	}

	passkey, err := s.store.HasPasskey(ctx, userID)
	if err != nil {
		return Offers{}, fmt.Errorf("look for a passkey: %w", err)
	}

	return Offers{Code: code, Passkey: passkey}, nil
}

// periodFor is which thirty-second window a moment belongs to, which is what
// makes a code single-use.
func periodFor(at time.Time) int64 { return at.Unix() / 30 }

// hashCode is the stored form of a recovery code. SHA-256 rather than argon2id:
// the code is this server's own randomness, so there is no dictionary to slow
// down, and a list of ten would otherwise cost ten slow hashes to check.
func hashCode(code string) []byte {
	sum := sha256.Sum256([]byte(normaliseCode(code)))
	return sum[:]
}

/*
normaliseCode forgives how somebody typed it.

A recovery code is read off paper and typed by hand, so the separators and the
case are noise. What is compared is the letters and digits.
*/
func normaliseCode(code string) string {
	var cleaned strings.Builder
	for _, character := range strings.ToLower(code) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			cleaned.WriteRune(character)
		}
	}
	return cleaned.String()
}

// newTicket is the value that stands for a half-finished sign-in.
func newTicket() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer), nil
}

func hashTicket(ticket string) []byte {
	sum := sha256.Sum256([]byte(ticket))
	return sum[:]
}

// SweepExpired removes tickets nobody finished.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.DeleteExpiredPendingSignIns(ctx)
}

// totpOptions are the settings every code is generated and checked with. Stated
// once, because a mismatch between the two is a factor that never accepts
// anything.
func totpOptions() totp.ValidateOpts {
	return totp.ValidateOpts{
		Period:    30,
		Skew:      skew,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
}

// notFound reports the absence of a row rather than a failure to read one.
func notFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
