package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Errors a caller is expected to handle.
var (
	// ErrUsernameTaken and ErrEmailTaken are safe to return during
	// registration, where the person is choosing their own name and needs to
	// know it is unavailable. They must never leak from an authentication
	// path, where they would confirm an account exists.
	ErrUsernameTaken = errors.New("username is already taken")
	ErrEmailTaken    = errors.New("email address is already registered")

	// ErrUserNotFound reports an account that does not exist. Authentication
	// must not surface it: see ErrAuthenticationFailed.
	ErrUserNotFound = errors.New("user not found")

	// ErrSetupComplete reports first-run setup attempted after the first
	// administrator exists.
	ErrSetupComplete = errors.New("first-run setup has already been completed")
)

// Store is the database surface the identity service needs.
//
// Named rather than taking *dbgen.Queries directly so that tests can supply a
// substitute and so the service does not depend on generated code beyond the
// operations it actually uses.
type Store interface {
	dbgen.Querier
}

// Service creates and looks up accounts.
type Service struct {
	store Store
	log   *slog.Logger
	// formerNameHold is how long a given-up username stays its old owner's.
	// See HoldFormerNames.
	formerNameHold func(context.Context) time.Duration
	// created is told about each new account. See OnCreated.
	created func(context.Context, dbgen.User)
}

// NewService builds an identity service over the given store.
func NewService(store Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

// NewUser describes an account to create.
type NewUser struct {
	Username string
	// Email is optional: a server may be configured to require only a
	// username and a passkey.
	Email string
	// Password is optional. An account may instead be created with a passkey
	// or an external identity as its only credential.
	Password    string
	DisplayName string
	/*
		EmailVerified marks the address as already proved.

		Only ever true when something else did the proving — today, an OAuth2
		provider that says it verified the address. Ordinary registration leaves
		it false, because typing an address into a form proves nothing about
		holding it.
	*/
	EmailVerified bool
}

// CreateUser registers an account.
//
// Uniqueness is enforced by the database rather than by checking first: a
// check followed by an insert is a race, and under concurrent registration
// two requests would both pass the check.
func (s *Service) CreateUser(ctx context.Context, input NewUser) (dbgen.User, error) {
	username := strings.TrimSpace(input.Username)
	if err := ValidateUsername(username); err != nil {
		return dbgen.User{}, err
	}

	email := NormaliseEmail(input.Email)
	if email != "" {
		if err := ValidateEmail(email); err != nil {
			return dbgen.User{}, err
		}
	}

	// Hash before inserting, so a weak password is rejected without leaving a
	// half-created account behind.
	var passwordHash string
	if input.Password != "" {
		hash, err := HashPassword(input.Password)
		if err != nil {
			return dbgen.User{}, err
		}
		passwordHash = hash
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if err := ValidateDisplayName(displayName); err != nil {
		return dbgen.User{}, err
	}
	if displayName == "" {
		displayName = username
	}

	// A name somebody else gave up recently is still theirs. Reported as
	// taken, which is true, rather than as somebody's old name, which would
	// say more than the person registering needs to know.
	held, err := s.UsernameHeld(ctx, NormaliseUsername(username), uuid.Nil())
	if err != nil {
		return dbgen.User{}, err
	}
	if held {
		return dbgen.User{}, ErrUsernameTaken
	}

	params := dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       username,
		UsernameFolded: NormaliseUsername(username),
		DisplayName:    displayName,
		EmailVerified:  input.EmailVerified && email != "",
	}
	if email != "" {
		params.Email = &email
		params.EmailFolded = &email
	}

	user, err := s.store.CreateUser(ctx, params)
	if err != nil {
		return dbgen.User{}, translateUniqueViolation(err)
	}

	if passwordHash != "" {
		if _, err := s.store.CreateCredential(ctx, dbgen.CreateCredentialParams{
			ID:     uuid.New(),
			UserID: user.ID,
			Kind:   "password",
			Secret: []byte(passwordHash),
		}); err != nil {
			// The account exists but cannot be signed into. Report the failure
			// rather than returning a half-built account as success.
			return dbgen.User{}, fmt.Errorf("store password credential: %w", err)
		}
	}

	if s.created != nil {
		s.created(ctx, user)
	}
	return user, nil
}

/*
OnCreated sets what is told about each account once it exists.

Here rather than in each of the ways an account comes about — registering,
setting up the server, signing in through a provider, an application being
approved — so that none of them can be the one that forgot. Called once while
the server is being built.
*/
func (s *Service) OnCreated(tell func(context.Context, dbgen.User)) { s.created = tell }

// translateUniqueViolation converts a database uniqueness error into the
// specific error a caller can act on, so registration can tell the person
// which field to change.
func translateUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return err
	}

	switch {
	case strings.Contains(pgErr.ConstraintName, "username"):
		return ErrUsernameTaken
	case strings.Contains(pgErr.ConstraintName, "email"):
		return ErrEmailTaken
	default:
		return err
	}
}

// UserByUsername looks up an account by name, matched case-insensitively.
func (s *Service) UserByUsername(ctx context.Context, username string) (dbgen.User, error) {
	user, err := s.store.GetUserByUsername(ctx, NormaliseUsername(username))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, ErrUserNotFound
	}
	return user, err
}

// UserByID looks up an account by identifier.
func (s *Service) UserByID(ctx context.Context, id uuid.UUID) (dbgen.User, error) {
	user, err := s.store.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, ErrUserNotFound
	}
	return user, err
}

// SetupNeeded reports whether the server still has no accounts, and therefore
// still needs its first administrator.
func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count == 0, nil
}

// CompleteSetup creates the first administrator.
//
// It refuses once any account exists, which is what closes the path: the
// window is open only on a genuinely empty server, so the endpoint cannot be
// used later to mint an administrator.
func (s *Service) CompleteSetup(ctx context.Context, input NewUser) (dbgen.User, error) {
	needed, err := s.SetupNeeded(ctx)
	if err != nil {
		return dbgen.User{}, err
	}
	if !needed {
		return dbgen.User{}, ErrSetupComplete
	}

	// The first administrator must be able to sign in, so unlike an ordinary
	// account this one requires a password.
	if input.Password == "" {
		return dbgen.User{}, errors.New("the first administrator requires a password")
	}

	user, err := s.CreateUser(ctx, input)
	if err != nil {
		return dbgen.User{}, err
	}

	s.log.Info("first administrator created",
		slog.String("username", user.Username),
		slog.String("userId", user.ID.String()),
	)

	return user, nil
}

/*
Suspend bars somebody from the server and ends their sessions.

Two steps, both necessary. The flag stops a new sign-in and is checked on every
request, so an open connection stops working at once. Revoking the sessions is
what makes that immediate rather than merely eventual — without it, a client
holding a valid session would keep being told its session is invalid on every
request, which works but leaves a row that looks live.

A reason is required. Somebody suspended is entitled to know why, and a
decision nobody can explain is one nobody can review.
*/
func (s *Service) Suspend(
	ctx context.Context,
	userID, by uuid.UUID,
	reason string,
	until *time.Time,
) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}

	if err := s.store.SuspendUser(ctx, dbgen.SuspendUserParams{
		ID:             userID,
		SuspendedUntil: until,
		SuspendedBy:    &by,
		SuspensionNote: &reason,
	}); err != nil {
		return fmt.Errorf("suspend: %w", err)
	}

	// Every session, on every device. A suspension that left one device signed
	// in would be no suspension at all.
	if err := s.store.RevokeAllSessionsForUser(ctx, userID); err != nil {
		return fmt.Errorf("suspend: revoking sessions: %w", err)
	}

	return nil
}

// LiftSuspension restores an account.
//
// Sessions are not restored: they were revoked, and somebody returning signs in
// again. Reviving them would mean a device somebody had lost access to comes
// back with it.
func (s *Service) LiftSuspension(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.LiftUserSuspension(ctx, userID); err != nil {
		return fmt.Errorf("lift suspension: %w", err)
	}
	return nil
}

// ErrReasonRequired is returned when a suspension carries no explanation.
var ErrReasonRequired = errors.New("a reason is required")

// guestSessionLifetime is how long a guest's session lasts at most; it stops
// sooner, the moment their call ends.
const guestSessionLifetime = 12 * time.Hour

/*
CreateGuest makes the account a guest of one call holds: a generated name,
the name they gave to show, no password, and the call it belongs to. It
cannot sign in; the session started for it is the only way it is used.
*/
func (s *Service) CreateGuest(ctx context.Context, callID uuid.UUID, displayName string, device DeviceContext) (dbgen.User, Session, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return dbgen.User{}, Session{}, fmt.Errorf("%w: a guest needs a name to be shown by", ErrInvalidDisplayName)
	}
	if err := ValidateDisplayName(displayName); err != nil {
		return dbgen.User{}, Session{}, err
	}
	var user dbgen.User
	for attempt := 0; ; attempt++ {
		raw := make([]byte, 6)
		if _, err := rand.Read(raw); err != nil {
			return dbgen.User{}, Session{}, err
		}
		username := "guest-" + hex.EncodeToString(raw)
		created, err := s.store.CreateGuestUser(ctx, dbgen.CreateGuestUserParams{
			ID: uuid.New(), Username: username, UsernameFolded: NormaliseUsername(username),
			DisplayName: displayName, GuestCall: &callID,
		})
		if err == nil {
			user = created
			break
		}
		if attempt >= 2 {
			return dbgen.User{}, Session{}, fmt.Errorf("create guest: %w", err)
		}
	}
	session, err := s.StartSession(ctx, user.ID, device, guestSessionLifetime)
	if err != nil {
		return dbgen.User{}, Session{}, err
	}
	return user, session, nil
}
