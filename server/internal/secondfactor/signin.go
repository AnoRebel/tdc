package secondfactor

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/netip"
	"time"
	"uuid"

	"github.com/pquerna/otp/totp"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/mail"
)

// Pending is a sign-in that has passed the first factor and owes a second.
type Pending struct {
	/*
		Ticket is what the client presents with the code.

		Not a session and not a credential: it buys nothing except the right to
		try a second factor for this one account, expires in minutes, and is
		spent when a code is accepted.
	*/
	Ticket    string
	ExpiresAt time.Time
	// RecoveryCodesLeft lets a client warn somebody who is nearly out before
	// they are, rather than after.
	RecoveryCodesLeft int64
}

/*
Begin records that somebody has passed the first factor.

Called only after the password was right. The device is recorded here rather
than read at the second request, so the session that eventually issues says
where the sign-in began — which on a phone changing networks is not where it
finished.
*/
func (s *Service) Begin(
	ctx context.Context,
	userID uuid.UUID,
	userAgent string,
	address netip.Addr,
) (Pending, error) {
	ticket, err := newTicket()
	if err != nil {
		return Pending{}, err
	}

	params := dbgen.StartPendingSignInParams{
		ID:         uuid.New(),
		TicketHash: hashTicket(ticket),
		UserID:     userID,
		UserAgent:  truncate(userAgent, 512),
		ExpiresAt:  s.now().Add(pendingLifetime),
	}
	if address.IsValid() {
		params.IpAddress = &address
	}

	record, err := s.store.StartPendingSignIn(ctx, params)
	if err != nil {
		return Pending{}, fmt.Errorf("start the pending sign-in: %w", err)
	}

	remaining, err := s.store.CountUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return Pending{}, fmt.Errorf("count the recovery codes: %w", err)
	}

	return Pending{
		Ticket:            ticket,
		ExpiresAt:         record.ExpiresAt,
		RecoveryCodesLeft: remaining,
	}, nil
}

// Accepted is a sign-in that has now passed both factors.
type Accepted struct {
	UserID    uuid.UUID
	UserAgent string
	Address   netip.Addr
	// UsedRecoveryCode says a code from the printed list was spent, which is
	// worth telling somebody about: it means one fewer, and it means somebody
	// used the fallback rather than the app.
	UsedRecoveryCode bool
	// RecoveryCodesLeft after this one, so the count shown is the count after
	// the spending rather than before it.
	RecoveryCodesLeft int64
}

/*
Complete checks a code against a pending sign-in.

Either kind of code: the app's six digits, or one from the printed list. Both
are tried because somebody reaching for their recovery codes has already lost
access to the app, and asking them to say which they are using first would be
asking them to explain their own emergency.
*/
func (s *Service) Complete(ctx context.Context, ticket, code string) (Accepted, error) {
	pending, err := s.store.GetPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return Accepted{}, ErrRejected
	}
	if err != nil {
		return Accepted{}, fmt.Errorf("find the pending sign-in: %w", err)
	}

	/*
		Counted before the code is checked, not after.

		Counting afterwards would mean a request that fails partway — or one an
		attacker abandons on seeing the response take too long — costs nothing,
		and the limit could be evaded by never letting the check finish.
	*/
	counted, err := s.store.CountPendingAttempt(ctx, pending.ID)
	if err != nil {
		return Accepted{}, fmt.Errorf("count the attempt: %w", err)
	}
	if counted.Attempts > maxAttempts {
		return Accepted{}, ErrTooManyAttempts
	}

	accepted, usedRecovery, err := s.check(ctx, pending.UserID, code)
	if err != nil {
		return Accepted{}, err
	}
	if !accepted {
		return Accepted{}, ErrRejected
	}

	// The ticket is spent, so one ticket buys one session even if two correct
	// codes arrive together.
	spent, err := s.store.FinishPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return Accepted{}, ErrRejected
	}
	if err != nil {
		return Accepted{}, fmt.Errorf("finish the pending sign-in: %w", err)
	}

	remaining, err := s.store.CountUnusedRecoveryCodes(ctx, pending.UserID)
	if err != nil {
		return Accepted{}, fmt.Errorf("count the recovery codes: %w", err)
	}

	result := Accepted{
		UserID:            spent.UserID,
		UserAgent:         spent.UserAgent,
		UsedRecoveryCode:  usedRecovery,
		RecoveryCodesLeft: remaining,
	}
	if spent.IpAddress != nil {
		result.Address = *spent.IpAddress
	}
	return result, nil
}

/*
emailedCodeLifetime is how long a code sent by mail works.

Five minutes, against the ticket's ten. A ticket lasts long enough to find a
phone and open an app; a code that arrived by mail should not still work ten
minutes after somebody read it over a shoulder, and the mail has already
arrived by the time it matters.
*/
const emailedCodeLifetime = 5 * time.Minute

/*
emailedCodeDigits is how long the code is.

Six, matching the authenticator app's, so somebody typing one into the same box
is not surprised by the length. The allowance on a ticket is what makes six
digits enough: five attempts against a million possibilities.
*/
const emailedCodeDigits = 6

/*
PendingFor says whose sign-in a ticket belongs to.

So a caller can look the account up — to find its address, or to decide whether
it has one worth sending to — without this package needing to know what an
account is.
*/
func (s *Service) PendingFor(ctx context.Context, ticket string) (uuid.UUID, error) {
	pending, err := s.store.GetPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return uuid.Nil(), ErrRejected
	}
	if err != nil {
		return uuid.Nil(), fmt.Errorf("find the pending sign-in: %w", err)
	}
	return pending.UserID, nil
}

/*
SendEmailedCode issues a code for a pending sign-in and returns the message.

Returned rather than sent, like the recovery mail: the caller sends it in the
background, and the response does not wait on a mail server.

Replaces any outstanding code, so somebody who lost the first message asks again
rather than being stuck with a code they cannot read.
*/
func (s *Service) SendEmailedCode(ctx context.Context, ticket, address, serverName string) (mail.Message, error) {
	pending, err := s.store.GetPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return mail.Message{}, ErrRejected
	}
	if err != nil {
		return mail.Message{}, fmt.Errorf("find the pending sign-in: %w", err)
	}

	code, err := newEmailedCode()
	if err != nil {
		return mail.Message{}, err
	}

	if err := s.store.SetEmailedCode(ctx, dbgen.SetEmailedCodeParams{
		ID:                   pending.ID,
		EmailedCodeHash:      hashCode(code),
		EmailedCodeExpiresAt: pointerTo(s.now().Add(emailedCodeLifetime)),
	}); err != nil {
		return mail.Message{}, fmt.Errorf("record the code: %w", err)
	}

	/*
		Says what to do if it was not them.

		A code arriving unasked means somebody has their password, which is the
		one thing the message can usefully tell them — and it is more urgent
		than the code itself.
	*/
	body := fmt.Sprintf(`Your sign-in code for %s is:

%s

It works once, for the next five minutes.

If you did not just try to sign in, somebody else has your password. Change it
as soon as you can.
`, serverName, code)

	return mail.Message{
		To:      address,
		Subject: "Your sign-in code for " + serverName,
		Body:    body,
	}, nil
}

/*
CompleteWithEmailedCode spends a ticket that an emailed code has answered.

Attempts are counted first, as they are for an authenticator app's code: six
digits is a million possibilities, and unlimited guessing makes that patience
rather than security.
*/
func (s *Service) CompleteWithEmailedCode(ctx context.Context, ticket, code string) (Accepted, error) {
	pending, err := s.store.GetPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return Accepted{}, ErrRejected
	}
	if err != nil {
		return Accepted{}, fmt.Errorf("find the pending sign-in: %w", err)
	}

	counted, err := s.store.CountPendingAttempt(ctx, pending.ID)
	if err != nil {
		return Accepted{}, fmt.Errorf("count the attempt: %w", err)
	}
	if counted.Attempts > maxAttempts {
		return Accepted{}, ErrTooManyAttempts
	}

	/*
		The spend is the check.

		Matched, unexpired and cleared in one statement, so a code is good once
		under two simultaneous attempts rather than by convention.
	*/
	if _, err := s.store.SpendEmailedCode(ctx, dbgen.SpendEmailedCodeParams{
		TicketHash:      hashTicket(ticket),
		EmailedCodeHash: hashCode(normaliseDigits(code)),
	}); err != nil {
		if notFound(err) {
			return Accepted{}, ErrRejected
		}
		return Accepted{}, fmt.Errorf("spend the code: %w", err)
	}

	spent, err := s.store.FinishPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return Accepted{}, ErrRejected
	}
	if err != nil {
		return Accepted{}, fmt.Errorf("finish the pending sign-in: %w", err)
	}

	remaining, err := s.store.CountUnusedRecoveryCodes(ctx, spent.UserID)
	if err != nil {
		return Accepted{}, fmt.Errorf("count the recovery codes: %w", err)
	}

	result := Accepted{
		UserID:            spent.UserID,
		UserAgent:         spent.UserAgent,
		RecoveryCodesLeft: remaining,
	}
	if spent.IpAddress != nil {
		result.Address = *spent.IpAddress
	}
	return result, nil
}

// newEmailedCode is six digits, zero-padded so it is always the same length.
func newEmailedCode() (string, error) {
	limit := big.NewInt(1)
	for index := 0; index < emailedCodeDigits; index++ {
		limit.Mul(limit, big.NewInt(10))
	}

	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return fmt.Sprintf("%0*d", emailedCodeDigits, value), nil
}

/*
CompleteWithPasskey spends a ticket that a passkey has answered.

The passkey ceremony is verified by the passkeys package, which says whose
account it proved. This checks that the account it proved is the account the
ticket is for — without which a passkey on any account would complete a pending
sign-in for any other, which is the whole of the second factor undone.

No attempt is counted. A passkey is not guessable, so an allowance would protect
nothing, and spending one on a ceremony the person abandoned would punish
somebody whose authenticator prompt timed out.
*/
func (s *Service) CompleteWithPasskey(ctx context.Context, ticket string, provedUserID uuid.UUID) (Accepted, error) {
	pending, err := s.store.GetPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return Accepted{}, ErrRejected
	}
	if err != nil {
		return Accepted{}, fmt.Errorf("find the pending sign-in: %w", err)
	}

	if pending.UserID != provedUserID {
		/*
			A passkey belonging to somebody else.

			Refused with the same error a wrong code gives: which of the two
			went wrong is not something the caller needs, and saying would
			confirm that the ticket is real.
		*/
		return Accepted{}, ErrRejected
	}

	spent, err := s.store.FinishPendingSignIn(ctx, hashTicket(ticket))
	if notFound(err) {
		return Accepted{}, ErrRejected
	}
	if err != nil {
		return Accepted{}, fmt.Errorf("finish the pending sign-in: %w", err)
	}

	remaining, err := s.store.CountUnusedRecoveryCodes(ctx, pending.UserID)
	if err != nil {
		return Accepted{}, fmt.Errorf("count the recovery codes: %w", err)
	}

	result := Accepted{
		UserID:            spent.UserID,
		UserAgent:         spent.UserAgent,
		RecoveryCodesLeft: remaining,
	}
	if spent.IpAddress != nil {
		result.Address = *spent.IpAddress
	}
	return result, nil
}

/*
check tries a code as both kinds.

The app first, because that is what almost every sign-in uses. A recovery code
is tried only when the digits did not work, which also means a six-digit string
that happens to be wrong does not spend anything.
*/
func (s *Service) check(ctx context.Context, userID uuid.UUID, code string) (bool, bool, error) {
	if digits := normaliseDigits(code); len(digits) == 6 {
		accepted, err := s.checkTOTP(ctx, userID, digits)
		if err != nil {
			return false, false, err
		}
		if accepted {
			return true, false, nil
		}
	}

	accepted, err := s.checkRecovery(ctx, userID, code)
	return accepted, accepted, err
}

/*
checkTOTP verifies the app's code, and spends the period it belongs to.

The verification is the easy half. The important half is the advance: a code is
valid for its whole window and one either side, so without recording which
period was accepted the same six digits work again for up to ninety seconds —
long enough for somebody who read them over a shoulder or off a phishing page.
*/
func (s *Service) checkTOTP(ctx context.Context, userID uuid.UUID, digits string) (bool, error) {
	credential, err := s.store.GetTOTPCredential(ctx, userID)
	if notFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load the secret: %w", err)
	}
	// An enrolment somebody began and never proved is not a factor.
	if credential.ConfirmedAt == nil {
		return false, nil
	}

	at := s.now()
	valid, err := totp.ValidateCustom(digits, string(credential.Secret), at, totpOptions())
	if err != nil || !valid {
		return false, nil
	}

	/*
		Which period this code actually came from.

		Not necessarily the current one: the window either side is accepted, so
		a code from the previous period is valid and must advance the counter
		to that period rather than to now — otherwise the current period's code
		would be refused as stale a moment later.
	*/
	period, found := s.periodOf(digits, string(credential.Secret), at)
	if !found {
		return false, nil
	}

	if _, err := s.store.AdvanceTOTPPeriod(ctx, dbgen.AdvanceTOTPPeriodParams{
		ID:         credential.ID,
		LastPeriod: pointerTo(period),
	}); err != nil {
		if notFound(err) {
			// The update matched nothing, which means this period is not later
			// than the last accepted one: a replay.
			return false, nil
		}
		return false, fmt.Errorf("advance the period: %w", err)
	}

	return true, nil
}

/*
periodOf finds which window a valid code belongs to.

The library says whether a code is valid across the accepted range but not which
period produced it, and the difference matters: advancing to the wrong one
either refuses a code that should work or accepts one that should not.

Scanning from the earliest offset upward is deliberate. A code is valid in three
consecutive windows, so more than one candidate matches, and taking the earliest
keeps the mark as low as it can safely be — which leaves the following windows'
codes acceptable. Taking the latest would mark the sign-in a period ahead of
where it happened, and although the arithmetic recovers on the next attempt, the
code that has just been used would remain valid in the window after it. That is
the replay this whole mechanism exists to stop.
*/
func (s *Service) periodOf(digits, secret string, at time.Time) (int64, bool) {
	options := totpOptions()
	// Each candidate is checked with no skew, so exactly one window is tried at
	// a time and the match names the period.
	options.Skew = 0

	for offset := -int64(skew); offset <= int64(skew); offset++ {
		moment := at.Add(time.Duration(offset) * 30 * time.Second)
		valid, err := totp.ValidateCustom(digits, secret, moment, options)
		if err == nil && valid {
			return periodFor(moment), true
		}
	}
	return 0, false
}

// checkRecovery spends a code from the printed list, if it matches one.
func (s *Service) checkRecovery(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	normalised := normaliseCode(code)
	if normalised == "" {
		return false, nil
	}

	/*
		The spend is the check.

		Matched on the hash and required to be unused in the same statement, so
		two attempts with the same code produce one success — rather than two,
		found by a read that passed in both before either wrote.
	*/
	if _, err := s.store.SpendRecoveryCode(ctx, dbgen.SpendRecoveryCodeParams{
		UserID: userID,
		Secret: hashCode(code),
	}); err != nil {
		if notFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("spend the recovery code: %w", err)
	}

	return true, nil
}

// truncate bounds what is stored from a header somebody else controls.
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
