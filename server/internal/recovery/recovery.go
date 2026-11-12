/*
Package recovery is the way back into an account whose password is gone.

Two rules shape everything here, and both come from the specification.

A token is good once. It is spent in the statement that matches it, expires
quickly, and is invalidated the moment a newer one is issued — so somebody who
asks twice is not left holding two working links, and somebody who saw a request
they did not make can make it stop by asking again.

And asking discloses nothing. A request for an address with no account is
answered exactly as one with an account is: the same response, and no mail. That
costs something real — somebody who mistypes their address gets silence rather
than "no account here" — and it is still right, because the alternative is a
form that tells anybody who asks which addresses have accounts on this server.
*/
package recovery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/mail"
)

/*
ErrTokenInvalid covers every way a link can fail: unknown, already used,
expired, or issued to an address the account no longer has. One error, because
distinguishing them tells somebody holding a guessed token which guess was
closer.
*/
var ErrTokenInvalid = errors.New("that link is not usable")

// ErrNotAvailable reports a server with no mail configured, which cannot offer
// recovery at all.
var ErrNotAvailable = errors.New("this server cannot send recovery mail")

/*
tokenLifetime is how long a link works.

An hour. Long enough for mail to arrive and somebody to get to a computer, short
enough that a message sitting in an inbox somebody else later reads is not a way
in. The specification asks for time-limited; this is what that means here.
*/
const tokenLifetime = time.Hour

/*
Store is the database surface this package needs.

Both halves of it: the recovery tokens, and the confirmations that make recovery
reach anybody at all. One interface because they are one service — the same mail,
the same token shape, and an address that is confirmed is the thing recovery
depends on.
*/
type Store interface {
	ConfirmationStore

	CreateRecoveryToken(ctx context.Context, arg dbgen.CreateRecoveryTokenParams) (dbgen.RecoveryToken, error)
	InvalidateRecoveryTokens(ctx context.Context, userID uuid.UUID) (int64, error)
	GetRecoveryToken(ctx context.Context, tokenHash []byte) (dbgen.RecoveryToken, error)
	SpendRecoveryToken(ctx context.Context, tokenHash []byte) (dbgen.RecoveryToken, error)
	DeleteExpiredRecoveryTokens(ctx context.Context) (int64, error)

	GetUserByEmail(ctx context.Context, emailFolded *string) (dbgen.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (dbgen.User, error)
}

// Service issues and redeems recovery links.
type Service struct {
	store Store
	mail  mail.Sender
	// where the link points, which is the operator's canonical address.
	base string
	// serverName is what a message calls this server.
	serverName string
	log        *slog.Logger
	now        func() time.Time
}

// NewService builds the recovery service.
func NewService(store Store, sender mail.Sender, canonicalAddress, serverName string, log *slog.Logger) *Service {
	return &Service{
		store:      store,
		mail:       sender,
		base:       strings.TrimSuffix(strings.TrimSpace(canonicalAddress), "/"),
		serverName: serverName,
		log:        log,
		now:        time.Now,
	}
}

/*
Available says whether recovery can be offered.

Mail and a canonical address: without mail there is nothing to send, and without
an address there is nowhere for the link to point. A client asks before drawing
the "forgotten your password" link, so that it does not offer something that
answers with a refusal.
*/
func (s *Service) Available() bool {
	return s != nil && s.mail != nil && s.mail.Configured() && s.base != ""
}

/*
Request starts a recovery, and says nothing about whether it found anything.

It returns no error for an unknown address. That is the whole point: the caller
answers the same way regardless, and cannot accidentally leak the difference by
branching on what came back.

The mail is sent by the caller in the background for the same reason — a
response that waited for the mail server would disclose by its timing what it
refuses to disclose in its body.
*/
func (s *Service) Request(ctx context.Context, address string, from netip.Addr) (Pending, error) {
	if !s.Available() {
		return Pending{}, ErrNotAvailable
	}

	folded := identity.NormaliseEmail(address)
	if folded == "" || !strings.Contains(folded, "@") {
		// Not an address at all. Still not an error: a caller that treated this
		// differently would answer differently, which is the disclosure.
		return Pending{}, nil
	}

	user, err := s.store.GetUserByEmail(ctx, &folded)
	if errors.Is(err, pgx.ErrNoRows) {
		return Pending{}, nil
	}
	if err != nil {
		return Pending{}, fmt.Errorf("look for the account: %w", err)
	}

	/*
		Only a verified address.

		An unverified one is an address somebody typed into a form, and mailing
		a way into the account to it would make "type somebody else's address
		into your profile" a takeover. Silent, like everything else here.
	*/
	if !user.EmailVerified {
		s.log.Info("recovery requested for an account whose address is unverified",
			"user", user.ID)
		return Pending{}, nil
	}

	/*
		Anything outstanding stops working now.

		The specification asks for it, and the reason is somebody who sees a
		recovery mail they did not ask for: asking for one themselves is how
		they make the first one useless.
	*/
	if _, err := s.store.InvalidateRecoveryTokens(ctx, user.ID); err != nil {
		return Pending{}, fmt.Errorf("invalidate the outstanding tokens: %w", err)
	}

	token, err := newToken()
	if err != nil {
		return Pending{}, err
	}

	record, err := s.store.CreateRecoveryToken(ctx, dbgen.CreateRecoveryTokenParams{
		ID:            uuid.New(),
		TokenHash:     hashToken(token),
		UserID:        user.ID,
		SentTo:        folded,
		RequestedFrom: optionalAddress(from),
		ExpiresAt:     s.now().Add(tokenLifetime),
	})
	if err != nil {
		return Pending{}, fmt.Errorf("record the request: %w", err)
	}

	return Pending{
		Message:   s.compose(user, token),
		To:        folded,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

/*
Pending is a message waiting to be sent.

Returned rather than sent here so the caller decides when — which for a web
request means after answering, in the background, so the response's timing says
nothing.

An empty Message means there was nothing to send, which is what an unknown
address produces. A caller must not treat that differently from a real one.
*/
type Pending struct {
	Message   mail.Message
	To        string
	ExpiresAt time.Time
}

// Nothing says whether this request found an account. Only the caller's
// background sender should ask.
func (p Pending) Nothing() bool { return p.To == "" }

/*
Redeem spends a token and sets a new password.

Every session goes with it. A password reset that left old sessions working
would leave whoever caused the reset still signed in, which is precisely the
situation somebody recovering their account is trying to end.
*/
func (s *Service) Redeem(ctx context.Context, token, newPassword string, people *identity.Service) (dbgen.User, error) {
	if !s.Available() {
		return dbgen.User{}, ErrNotAvailable
	}

	/*
		Checked before it is spent, so a password the server will refuse does
		not burn the link.

		Somebody typing a too-short password into a recovery form should be told
		and allowed to try again, not sent back to the start with a link that no
		longer works.
	*/
	if err := identity.CheckPasswordStrength(newPassword); err != nil {
		return dbgen.User{}, err
	}

	record, err := s.store.SpendRecoveryToken(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, ErrTokenInvalid
	}
	if err != nil {
		return dbgen.User{}, fmt.Errorf("spend the token: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, record.UserID)
	if err != nil {
		return dbgen.User{}, fmt.Errorf("load the account: %w", err)
	}

	/*
		The address must still be the one it was sent to.

		Otherwise a token mailed to an old address remains a way into whatever
		the new address now protects — which matters most for exactly the
		account whose owner changed their address because the old one was
		compromised.
	*/
	if user.EmailFolded == nil || *user.EmailFolded != record.SentTo {
		return dbgen.User{}, ErrTokenInvalid
	}

	/*
		The new password, and every session ended.

		uuid.Nil() keeps nothing: there is no session doing this, and any that
		exist belong either to the person who is about to sign in properly or to
		whoever they are recovering the account from.
	*/
	if err := people.ChangePassword(ctx, user.ID, newPassword, uuid.Nil()); err != nil {
		return dbgen.User{}, err
	}

	return user, nil
}

// Check says whether a link is still worth showing a form for, without spending
// it.
func (s *Service) Check(ctx context.Context, token string) error {
	if !s.Available() {
		return ErrNotAvailable
	}

	if _, err := s.store.GetRecoveryToken(ctx, hashToken(token)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTokenInvalid
		}
		return fmt.Errorf("look up the token: %w", err)
	}
	return nil
}

// SweepExpired removes what nobody used.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.DeleteExpiredRecoveryTokens(ctx)
}

// compose writes the message somebody receives.
func (s *Service) compose(user dbgen.User, token string) mail.Message {
	link := s.base
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	link += "/recover?token=" + url.QueryEscape(token)

	name := user.DisplayName
	if name == "" {
		name = user.Username
	}

	/*
		Short, plain, and saying what to do if it was not them.

		A recovery mail is the one message this server sends that an attacker
		wants somebody to act on without thinking, so it says who it is for,
		what it does, how long it lasts, and — most usefully — that ignoring it
		is safe.
	*/
	body := fmt.Sprintf(`Hello %s,

Somebody asked to reset the password for your account on %s.

To set a new one, open this link within the hour:

%s

If that was not you, you can ignore this message. Your password has not
changed, and the link stops working once it is used or once another is
requested.
`, name, s.serverName, link)

	return mail.Message{
		To:      *user.EmailFolded,
		Subject: "Reset your password on " + s.serverName,
		Body:    body,
	}
}

// newToken is the value that goes in the link: unguessable, and URL-safe so it
// survives being pasted out of a mail client.
func newToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// hashToken is the stored form. SHA-256 rather than a password hash: the token
// is 256 bits of randomness, so there is no dictionary to slow down.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func optionalAddress(from netip.Addr) *netip.Addr {
	if !from.IsValid() {
		return nil
	}
	return &from
}
