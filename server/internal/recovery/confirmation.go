package recovery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/mail"
)

/*
Proving an address belongs to whoever typed it.

Recovery refuses to mail an unverified address, which is right — a way back into
an account, sent somewhere nobody proved, is a way into somebody else's account.
The consequence is that without this, recovery reaches almost nobody: an address
typed into a registration form is a claim, not a fact.

An address in progress is kept apart from the one already working. Somebody who
mistypes a new address would otherwise lose the old one the moment they pressed
save — and with it, the only way back in should they then forget the password.
*/

// ErrAddressTaken reports an address already on another account. Unlike
// recovery, this one is said out loud: the person is signed in, choosing their
// own address, and cannot proceed without knowing.
var ErrAddressTaken = errors.New("that address is already on another account")

/*
confirmationLifetime is how long a confirmation link works.

A day rather than recovery's hour. Nothing is at stake in an unclaimed
confirmation — it proves an address and grants nothing — and somebody adding an
address may reasonably get to their mail tomorrow rather than within the hour.
*/
const confirmationLifetime = 24 * time.Hour

// ConfirmationStore is the database surface confirming an address needs.
type ConfirmationStore interface {
	CreateEmailConfirmation(ctx context.Context, arg dbgen.CreateEmailConfirmationParams) (dbgen.EmailConfirmation, error)
	InvalidateEmailConfirmations(ctx context.Context, userID uuid.UUID) (int64, error)
	SpendEmailConfirmation(ctx context.Context, tokenHash []byte) (dbgen.EmailConfirmation, error)
	DeleteExpiredEmailConfirmations(ctx context.Context) (int64, error)

	SetPendingEmail(ctx context.Context, arg dbgen.SetPendingEmailParams) error
	ConfirmEmail(ctx context.Context, arg dbgen.ConfirmEmailParams) (dbgen.User, error)
	GetUserByEmail(ctx context.Context, emailFolded *string) (dbgen.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (dbgen.User, error)
}

/*
StartConfirmation records an address somebody wants to prove and returns the
message that proves it.

The account keeps whatever address it already had. Only confirming the link
moves the new one into place, so a typo costs nothing but the message.
*/
func (s *Service) StartConfirmation(ctx context.Context, userID uuid.UUID, address string) (Pending, error) {
	if !s.Available() {
		return Pending{}, ErrNotAvailable
	}

	folded := identity.NormaliseEmail(address)
	if err := identity.ValidateEmail(folded); err != nil {
		return Pending{}, err
	}

	/*
		Somebody else's address is refused, and said so.

		Unlike recovery, which must not disclose whether an account exists: here
		the person is signed in and choosing their own address. Silence would
		leave them waiting for a message that is never coming, and the fact they
		learn — that this address is in use — they could learn by trying to
		register with it anyway.
	*/
	existing, err := s.store.GetUserByEmail(ctx, &folded)
	switch {
	case err == nil && existing.ID != userID:
		return Pending{}, ErrAddressTaken
	case err == nil:
		// Already theirs and already confirmed: nothing to do, and saying so is
		// kinder than sending a message that changes nothing.
		if existing.EmailVerified {
			return Pending{}, nil
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return Pending{}, fmt.Errorf("look for that address: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return Pending{}, fmt.Errorf("load the account: %w", err)
	}

	if err := s.store.SetPendingEmail(ctx, dbgen.SetPendingEmailParams{
		ID:           userID,
		PendingEmail: &folded,
	}); err != nil {
		return Pending{}, fmt.Errorf("record the address: %w", err)
	}

	/*
		Anything outstanding stops working, so asking again is how somebody
		corrects an address they got wrong.

		The promotion below is also conditional on the pending address, so
		either alone would refuse an abandoned link. Both are here because they
		guard different things: this one the token, that one the account.
	*/
	if _, err := s.store.InvalidateEmailConfirmations(ctx, userID); err != nil {
		return Pending{}, fmt.Errorf("invalidate the outstanding links: %w", err)
	}

	token, err := newToken()
	if err != nil {
		return Pending{}, err
	}

	record, err := s.store.CreateEmailConfirmation(ctx, dbgen.CreateEmailConfirmationParams{
		ID:        uuid.New(),
		TokenHash: hashToken(token),
		UserID:    userID,
		Address:   folded,
		ExpiresAt: s.now().Add(confirmationLifetime),
	})
	if err != nil {
		return Pending{}, fmt.Errorf("record the confirmation: %w", err)
	}

	return Pending{
		Message:   s.composeConfirmation(user, folded, token),
		To:        folded,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

/*
Confirm spends a link and moves the address into place.

The address on the row has to still be the one the account is waiting on:
somebody who changed their mind and asked about a third address must not have
the link from the second complete it.
*/
func (s *Service) Confirm(ctx context.Context, token string) (dbgen.User, error) {
	if !s.Available() {
		return dbgen.User{}, ErrNotAvailable
	}

	record, err := s.store.SpendEmailConfirmation(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, ErrTokenInvalid
	}
	if err != nil {
		return dbgen.User{}, fmt.Errorf("spend the confirmation: %w", err)
	}

	/*
		Checked again here, not only when the link was issued.

		Between issuing and confirming, somebody else may have proved the same
		address. The unique index on email_folded would refuse the write anyway;
		this turns that into an error that says what happened.
	*/
	if existing, err := s.store.GetUserByEmail(ctx, &record.Address); err == nil && existing.ID != record.UserID {
		return dbgen.User{}, ErrAddressTaken
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, fmt.Errorf("look for that address: %w", err)
	}

	user, err := s.store.ConfirmEmail(ctx, dbgen.ConfirmEmailParams{
		ID:    record.UserID,
		Email: &record.Address,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The account is waiting on a different address now, so this link is
		// about something that is no longer being asked.
		return dbgen.User{}, ErrTokenInvalid
	}
	if err != nil {
		return dbgen.User{}, fmt.Errorf("confirm the address: %w", err)
	}

	return user, nil
}

// SweepExpiredConfirmations removes links nobody used.
func (s *Service) SweepExpiredConfirmations(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.DeleteExpiredEmailConfirmations(ctx)
}

// composeConfirmation writes what somebody receives.
func (s *Service) composeConfirmation(user dbgen.User, address, token string) mail.Message {
	link := s.base
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	link += "/confirm-email?token=" + url.QueryEscape(token)

	name := user.DisplayName
	if name == "" {
		name = user.Username
	}

	/*
		Says what it is for, and what happens if it was not them.

		"Somebody added this address" rather than "confirm your email", because
		the person reading it may not have asked — an address typed wrongly
		reaches whoever really owns it, and they should be told plainly that
		ignoring it is the right thing to do.
	*/
	body := fmt.Sprintf(`Hello %s,

This address was added to an account on %s. Confirming it means the account can
be recovered here if its password is ever forgotten.

To confirm, open this link within the day:

%s

If you did not add it, ignore this message. Nothing has changed, and the address
is not attached to anything until the link is used.
`, name, s.serverName, link)

	return mail.Message{
		To:      address,
		Subject: "Confirm this address for " + s.serverName,
		Body:    body,
	}
}
