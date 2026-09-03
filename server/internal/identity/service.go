package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
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
	if displayName == "" {
		displayName = username
	}

	params := dbgen.CreateUserParams{
		ID:             uuid.New(),
		Username:       username,
		UsernameFolded: NormaliseUsername(username),
		DisplayName:    displayName,
		EmailVerified:  false,
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

	return user, nil
}

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
