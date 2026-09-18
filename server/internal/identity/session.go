package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// ErrAuthenticationFailed is the single error every authentication failure
// returns.
//
// Wrong password, unknown account, no password credential and suspended
// account all produce this one value. Distinguishing them would let an
// unauthenticated caller enumerate accounts, and knowing an account exists is
// itself worth protecting.
var ErrAuthenticationFailed = errors.New("authentication failed")

// ErrSessionInvalid reports a session credential that is unknown, expired or
// revoked. Like authentication failure, it does not say which.
var ErrSessionInvalid = errors.New("session is not valid")

// sessionTokenBytes is the entropy in a session token. 32 bytes is well beyond
// what is guessable and keeps the encoded token a reasonable length.
const sessionTokenBytes = 32

// DefaultSessionLifetime is how long a session lasts without being renewed.
const DefaultSessionLifetime = 30 * 24 * time.Hour

// Credentials identify a person attempting to sign in.
type Credentials struct {
	// Username may be either a username or an email address: people do not
	// reliably remember which they registered with.
	Username string
	Password string
}

// DeviceContext describes where a session is being used, so a person can
// recognise it in a list well enough to decide whether to end it.
type DeviceContext struct {
	UserAgent string
	IPAddress netip.Addr
}

// Session is a newly established session and its token.
//
// The token is returned exactly once, at creation. Only its hash is stored, so
// it cannot be recovered later — a database disclosure does not hand over live
// sessions.
type Session struct {
	Record dbgen.Session
	Token  string
}

// Authenticate verifies credentials and establishes a session.
//
// Every failure path takes comparable time and returns the same error. In
// particular, an unknown account still performs a password verification
// against a dummy hash, so response timing does not reveal whether the account
// exists.
func (s *Service) Authenticate(
	ctx context.Context,
	creds Credentials,
	device DeviceContext,
	lifetime time.Duration,
) (Session, error) {
	user, found, err := s.lookupForAuthentication(ctx, creds.Username)
	if err != nil {
		return Session{}, err
	}

	var storedHash string
	if found {
		credential, err := s.store.GetPasswordCredential(ctx, user.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The account exists but has no password, perhaps because it uses
			// only a passkey. Fall through to the dummy comparison so this is
			// indistinguishable from a wrong password.
		case err != nil:
			return Session{}, fmt.Errorf("read password credential: %w", err)
		default:
			storedHash = string(credential.Secret)
		}
	}

	if storedHash == "" {
		// Spend comparable effort so that a missing account or credential
		// cannot be detected by how quickly the request fails.
		_, _ = VerifyPassword(creds.Password, dummyPasswordHash)
		return Session{}, ErrAuthenticationFailed
	}

	match, err := VerifyPassword(creds.Password, storedHash)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !match {
		return Session{}, ErrAuthenticationFailed
	}

	// A suspended account must not receive a session, and must not be
	// distinguishable from a failed password either.
	if isSuspended(user) {
		return Session{}, ErrAuthenticationFailed
	}

	return s.StartSession(ctx, user.ID, device, lifetime)
}

/*
VerifyPasswordOnly checks a password and stops there.

Authenticate above ends by issuing a session, which is right when the password
is the whole of the sign-in. Where a second factor is owed it is not: a session
that exists before the second factor has been proved is a session somebody could
use, and every route would then have to know that this particular one is not
finished yet.

So this does the same checks and returns the account instead. The caller either
issues a session itself, or asks for the second factor first.

Deliberately the same single error, and the same dummy comparison for an account
that does not exist or has no password, so which path a failure took is not
visible from outside.
*/
func (s *Service) VerifyPasswordOnly(ctx context.Context, creds Credentials) (dbgen.User, error) {
	user, found, err := s.lookupForAuthentication(ctx, creds.Username)
	if err != nil {
		return dbgen.User{}, err
	}

	var storedHash string
	if found {
		credential, err := s.store.GetPasswordCredential(ctx, user.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return dbgen.User{}, fmt.Errorf("read password credential: %w", err)
		default:
			storedHash = string(credential.Secret)
		}
	}

	if storedHash == "" {
		_, _ = VerifyPassword(creds.Password, dummyPasswordHash)
		return dbgen.User{}, ErrAuthenticationFailed
	}

	match, err := VerifyPassword(creds.Password, storedHash)
	if err != nil {
		return dbgen.User{}, fmt.Errorf("verify password: %w", err)
	}
	if !match || isSuspended(user) {
		return dbgen.User{}, ErrAuthenticationFailed
	}

	return user, nil
}

// lookupForAuthentication finds an account by username or email address.
//
// A miss is reported as found=false rather than an error, because the caller
// must treat it exactly like a wrong password.
func (s *Service) lookupForAuthentication(ctx context.Context, identifier string) (dbgen.User, bool, error) {
	folded := NormaliseUsername(identifier)

	user, err := s.store.GetUserByUsername(ctx, folded)
	switch {
	case err == nil:
		return user, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return dbgen.User{}, false, fmt.Errorf("look up user: %w", err)
	}

	email := NormaliseEmail(identifier)
	user, err = s.store.GetUserByEmail(ctx, &email)
	switch {
	case err == nil:
		return user, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return dbgen.User{}, false, nil
	default:
		return dbgen.User{}, false, fmt.Errorf("look up user by email: %w", err)
	}
}

func isSuspended(user dbgen.User) bool {
	if user.SuspendedAt == nil {
		return false
	}
	// A suspension with no end date is indefinite; one with an end date lapses
	// on its own, so a temporary restriction needs no moderator action to lift.
	if user.SuspendedUntil == nil {
		return true
	}
	return time.Now().Before(*user.SuspendedUntil)
}

// StartSession issues a session for an already-authenticated user.
//
// Separate from Authenticate so that passkey and OAuth2 sign-in, which verify
// identity differently, share one definition of what a session is.
func (s *Service) StartSession(
	ctx context.Context,
	userID uuid.UUID,
	device DeviceContext,
	lifetime time.Duration,
) (Session, error) {
	if lifetime <= 0 {
		lifetime = DefaultSessionLifetime
	}

	token, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}

	params := dbgen.CreateSessionParams{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashSessionToken(token),
		UserAgent: truncate(device.UserAgent, 512),
		ExpiresAt: time.Now().Add(lifetime),
	}
	if device.IPAddress.IsValid() {
		params.IpAddress = &device.IPAddress
	}

	record, err := s.store.CreateSession(ctx, params)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}

	return Session{Record: record, Token: token}, nil
}

// AuthenticatedUser resolves a session token to the user holding it.
//
// Expiry and revocation are enforced in the query rather than here, so a
// revoked session stops working immediately rather than at the next refresh.
func (s *Service) AuthenticatedUser(ctx context.Context, token string) (dbgen.User, dbgen.Session, error) {
	row, err := s.store.GetSessionByTokenHash(ctx, hashSessionToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, dbgen.Session{}, ErrSessionInvalid
	}
	if err != nil {
		return dbgen.User{}, dbgen.Session{}, fmt.Errorf("look up session: %w", err)
	}

	// A suspension applied after sign-in must take effect at once, so it is
	// checked on every request rather than only at authentication.
	if isSuspended(row.User) {
		return dbgen.User{}, dbgen.Session{}, ErrSessionInvalid
	}

	return row.User, row.Session, nil
}

// ListSessions returns a user's active sessions.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]dbgen.Session, error) {
	sessions, err := s.store.ListSessionsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// RevokeSession ends one session. Scoped to the owning user so one person
// cannot end another's session by guessing an identifier.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	if err := s.store.RevokeSession(ctx, dbgen.RevokeSessionParams{
		ID:     sessionID,
		UserID: userID,
	}); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// RevokeAllSessions ends every session for a user, including the current one.
func (s *Service) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.RevokeAllSessionsForUser(ctx, userID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

// ChangePassword replaces a user's password and ends their other sessions.
//
// Ending the others is the point: someone changing their password after a
// suspected compromise expects it to evict whoever else was signed in.
/*
RevokeOtherSessions ends every session but the one making the request.

What a revoked credential requires: a session it established outliving it would
mean whoever holds that session still has what the revocation was meant to take
away. The caller's own session is kept, because somebody removing a credential
has not asked to be signed out of the thing they are doing it from.
*/
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, keepSessionID uuid.UUID) error {
	if err := s.store.RevokeOtherSessionsForUser(ctx, dbgen.RevokeOtherSessionsForUserParams{
		UserID: userID,
		ID:     keepSessionID,
	}); err != nil {
		return fmt.Errorf("revoke other sessions: %w", err)
	}
	return nil
}

func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, newPassword string, keepSessionID uuid.UUID) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	if _, err := s.store.ReplacePasswordCredential(ctx, dbgen.ReplacePasswordCredentialParams{
		ID:     uuid.New(),
		UserID: userID,
		Secret: []byte(hash),
	}); err != nil {
		return fmt.Errorf("replace password: %w", err)
	}

	// The same rule a revoked credential gets: a session established with the
	// old password must not outlive it.
	return s.RevokeOtherSessions(ctx, userID, keepSessionID)
}

// newSessionToken returns a fresh, unguessable session token.
func newSessionToken() (string, error) {
	buffer := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// hashSessionToken returns the stored form of a token.
//
// A plain SHA-256 is right here, unlike for passwords: the token already has
// full entropy, so there is nothing to brute-force, and lookup happens on
// every request where a memory-hard hash would be far too slow.
func hashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// dummyPasswordHash is verified against when no real hash is available, so an
// unknown account costs the same as a wrong password.
//
// It is a valid argon2id hash of a value no one can supply.
var dummyPasswordHash = mustHashDummy()

func mustHashDummy() string {
	token, err := newSessionToken()
	if err != nil {
		// Only reachable if the system random source fails, in which case the
		// server cannot issue sessions either.
		panic("identity: cannot initialise dummy password hash: " + err.Error())
	}
	hash, err := HashPassword(token + token)
	if err != nil {
		panic("identity: cannot initialise dummy password hash: " + err.Error())
	}
	return hash
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
