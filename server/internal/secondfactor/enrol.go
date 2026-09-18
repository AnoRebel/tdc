package secondfactor

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Enrolment is what somebody needs to set up an authenticator app.
type Enrolment struct {
	/*
		Secret is the base32 value, shown so it can be typed where a camera is
		not available or does not work.
	*/
	Secret string
	/*
		URL is the otpauth:// value a QR code encodes.

		Rendered by the client rather than here: a server that returned a PNG
		would be deciding the size and the colours of something that has to fit
		a particular screen, and the value is short enough to encode anywhere.
	*/
	URL string
}

/*
BeginTOTP issues a secret for an authenticator app.

Unconfirmed until a code proves the app holds it. That two-step shape is what
stops a scan that silently failed from locking somebody out: until they have
produced a working code, nothing is asked of them at sign-in.
*/
func (s *Service) BeginTOTP(ctx context.Context, userID uuid.UUID) (Enrolment, error) {
	existing, err := s.store.GetTOTPCredential(ctx, userID)
	switch {
	case err == nil && existing.ConfirmedAt != nil:
		return Enrolment{}, ErrAlreadyEnrolled
	case err == nil:
		/*
			An unconfirmed secret from a previous attempt.

			Replaced rather than reused: somebody starting again is somebody
			whose last attempt did not work, and handing back the same secret
			would hand back whatever went wrong with it.
		*/
		if err := s.store.DeleteTOTPCredential(ctx, userID); err != nil {
			return Enrolment{}, fmt.Errorf("clear the unfinished enrolment: %w", err)
		}
	case !notFound(err):
		return Enrolment{}, fmt.Errorf("look for an existing secret: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return Enrolment{}, fmt.Errorf("load the account: %w", err)
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: s.issuer,
		// What the app shows beside the issuer, so somebody with two accounts
		// on one server can tell the codes apart.
		AccountName: user.Username,
		Period:      30,
		Digits:      totpOptions().Digits,
		Algorithm:   totpOptions().Algorithm,
	})
	if err != nil {
		return Enrolment{}, fmt.Errorf("generate a secret: %w", err)
	}

	if _, err := s.store.CreateTOTPCredential(ctx, dbgen.CreateTOTPCredentialParams{
		ID:     uuid.New(),
		UserID: userID,
		Label:  "Authenticator app",
		// The secret itself, which unlike a password must be recoverable: every
		// code is derived from it, so there is nothing to hash it into.
		Secret: []byte(key.Secret()),
	}); err != nil {
		return Enrolment{}, fmt.Errorf("store the secret: %w", err)
	}

	return Enrolment{Secret: key.Secret(), URL: key.URL()}, nil
}

/*
ConfirmTOTP finishes enrolment, given a code the app produced.

The period that code belonged to is recorded as it is confirmed, so the same
code cannot then be used to sign in: proving the app works and using it to get
in are two separate uses, and a code is good for one.
*/
func (s *Service) ConfirmTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	credential, err := s.store.GetTOTPCredential(ctx, userID)
	if notFound(err) {
		return ErrNotEnrolled
	}
	if err != nil {
		return fmt.Errorf("load the secret: %w", err)
	}
	if credential.ConfirmedAt != nil {
		return ErrAlreadyEnrolled
	}

	at := s.now()
	valid, err := totp.ValidateCustom(normaliseDigits(code), string(credential.Secret), at, totpOptions())
	if err != nil || !valid {
		return ErrRejected
	}

	return s.store.ConfirmTOTPCredential(ctx, dbgen.ConfirmTOTPCredentialParams{
		ID:         credential.ID,
		LastPeriod: pointerTo(periodFor(at)),
	})
}

// DisableTOTP removes the authenticator app, and the recovery codes with it:
// codes that recover access to a factor nobody has are a list of secrets
// serving no purpose.
func (s *Service) DisableTOTP(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.DeleteTOTPCredential(ctx, userID); err != nil {
		return fmt.Errorf("remove the authenticator app: %w", err)
	}
	if err := s.store.DeleteRecoveryCodes(ctx, userID); err != nil {
		return fmt.Errorf("remove the recovery codes: %w", err)
	}
	return nil
}

/*
IssueRecoveryCodes replaces the batch with a fresh one.

Returned in full exactly once, here, because only their hashes are kept: a
server that could show them again would be a server that stored them, and the
point of a recovery code is that reading the database does not yield one.

Replacing rather than adding, because somebody holding a printed list expects
that list to be the whole truth.
*/
func (s *Service) IssueRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if err := s.store.DeleteRecoveryCodes(ctx, userID); err != nil {
		return nil, fmt.Errorf("clear the old codes: %w", err)
	}

	codes := make([]string, 0, recoveryCodeCount)
	for index := 0; index < recoveryCodeCount; index++ {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}

		if _, err := s.store.CreateRecoveryCode(ctx, dbgen.CreateRecoveryCodeParams{
			ID:     uuid.New(),
			UserID: userID,
			Label:  "Recovery code",
			Secret: hashCode(code),
		}); err != nil {
			return nil, fmt.Errorf("store a recovery code: %w", err)
		}

		codes = append(codes, code)
	}

	return codes, nil
}

// RemainingRecoveryCodes is how many are left unspent, so somebody can be told
// before they run out rather than after.
func (s *Service) RemainingRecoveryCodes(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.store.CountUnusedRecoveryCodes(ctx, userID)
}

/*
recoveryAlphabet excludes the characters people confuse when reading aloud or
off paper: 0 and o, 1 and l and i.

The cost is a little entropy per character, which the length more than covers;
the benefit is that a code that does not work is a code that was wrong rather
than a code that was misread.
*/
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// recoveryCodeLength is the characters in one code, excluding the separator.
// Ten from this alphabet is about fifty bits, which is far beyond guessing at
// five attempts a ticket.
const recoveryCodeLength = 10

// newRecoveryCode produces one, grouped so it can be read back without losing
// one's place.
func newRecoveryCode() (string, error) {
	buffer := make([]byte, recoveryCodeLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	var code strings.Builder
	for index, value := range buffer {
		if index == recoveryCodeLength/2 {
			code.WriteByte('-')
		}
		// Modulo bias is negligible at this alphabet size, and the entropy is
		// far above what five attempts could exhaust regardless.
		code.WriteByte(recoveryAlphabet[int(value)%len(recoveryAlphabet)])
	}
	return code.String(), nil
}

// normaliseDigits strips what somebody's app may have put between the digits,
// and what they may have pasted around them.
func normaliseDigits(code string) string {
	var digits strings.Builder
	for _, character := range code {
		if character >= '0' && character <= '9' {
			digits.WriteRune(character)
		}
	}
	return digits.String()
}

func pointerTo[T any](value T) *T { return &value }
