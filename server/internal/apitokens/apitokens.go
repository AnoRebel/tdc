/*
Package apitokens issues and resolves the credentials a program uses.

Everything the web client does is an HTTP call somebody else can make. A token
is what lets them make it: carried in the same Authorization header a session
uses, on the same routes, answering identically.

The difference from a session is authority. A session carries everything its
holder can do, because it is that person at a keyboard. A token carries only
what it was given, and what the account still has — the two are intersected on
every request rather than frozen when the token was issued, so revoking
somebody's role takes it from their tokens in the same moment.
*/
package apitokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// ErrTokenInvalid is returned for a token that does not exist, has expired, or
// has been revoked.
//
// One error for all three deliberately: a caller holding a token that no
// longer works learns that it does not work, not which of the reasons applies.
var ErrTokenInvalid = errors.New("that token is not valid")

// ErrUnknownPermission is returned when a scope names something this server
// does not define.
//
// Refused rather than ignored: a typo that silently produced a token with less
// authority than asked for would be found weeks later, by a program failing at
// the one thing it was made to do.
var ErrUnknownPermission = errors.New("that is not a permission this server has")

// ErrNotGranted is returned when a scope asks for something its account does
// not hold.
//
// A token is a way to delegate authority somebody has, never a way to acquire
// authority they do not: allowing it would make an ordinary account able to
// mint an administrator.
var ErrNotGranted = errors.New("you cannot give a token a permission you do not hold")

// ErrLabelRequired is returned for a token with no label.
var ErrLabelRequired = errors.New("a token needs a label saying what it is for")

/*
tokenBytes is how much randomness a token carries.

Thirty-two bytes, matching sessions. The token is the entire credential — there
is no second factor behind it and no password to also know — so it has to be
infeasible to guess and stay that way for as long as the token lives, which may
be years.
*/
const tokenBytes = 32

/*
prefix marks a TDC API token in logs, code and secret scanners.

A token that looks like anonymous base64 is one nobody recognises when it is
pasted somewhere it should not be. A recognisable prefix is what lets a scanner
say "this is a credential" rather than "this is a string", which is the
difference between a leak somebody catches and one nobody does.
*/
const prefix = "tdc_"

/*
Looks reports whether a presented credential is shaped like an API token.

By prefix, not by lookup. It lets the authentication path tell a token from a
session before touching the database, so the ordinary case — a browser's
session — costs no extra query, and a session token is never searched for among
the tokens where it could not be.
*/
func Looks(presented string) bool {
	return strings.HasPrefix(presented, prefix)
}

// Token is one issued credential, as its owner sees it.
//
// Never carries the secret: it exists once, in the response to the request
// that created it, and is not recoverable afterwards by anybody.
type Token struct {
	ID    uuid.UUID
	Label string
	// Scope is what this token may exercise, subject to the account still
	// holding it.
	Scope      []authz.Permission
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time

	/*
		ApplicationID is set on a token an application holds, rather than a
		person.

		Such a token's authority is the application's grant, resolved by
		whoever authenticates the request — never the account's roles and
		never the permissions of whoever approved it. Nil on every token a
		person issues, which is every token that existed before applications.
	*/
	ApplicationID *uuid.UUID
}

// Store is the database surface this package needs.
type Store interface {
	dbgen.Querier
}

// Service issues and resolves tokens.
type Service struct {
	store Store
}

// NewService builds a token service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

/*
Issue creates a token and returns it once.

The returned string is the only time the secret exists outside the caller's
hands. It is not stored, cannot be recovered, and a lost one is replaced rather
than found.

held is what the account can currently do, which bounds what the token may be
given. Passing it in rather than looking it up keeps this package free of a
dependency on how permissions are resolved, and lets the caller — which has
already resolved them to authorise the request — avoid a second query.
*/
func (s *Service) Issue(
	ctx context.Context,
	userID uuid.UUID,
	label string,
	scope []authz.Permission,
	expiresAt *time.Time,
	held authz.Permissions,
) (Token, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return Token{}, "", ErrLabelRequired
	}

	if err := checkScope(scope, held); err != nil {
		return Token{}, "", err
	}

	secret, err := newToken()
	if err != nil {
		return Token{}, "", err
	}

	// Marshalled rather than assembled by hand so the column always holds a
	// JSON array, which its CHECK constraint requires.
	encoded, err := json.Marshal(names(scope))
	if err != nil {
		return Token{}, "", fmt.Errorf("encode scope: %w", err)
	}

	row, err := s.store.CreateAPIToken(ctx, dbgen.CreateAPITokenParams{
		ID:        uuid.New(),
		UserID:    userID,
		Label:     label,
		TokenHash: hash(secret),
		Scope:     encoded,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return Token{}, "", fmt.Errorf("create token: %w", err)
	}

	return asToken(row.ID, row.Label, row.Scope, row.ExpiresAt, row.LastUsedAt, row.CreatedAt), secret, nil
}

/*
Resolve identifies the account a token acts as, and what it may do.

Returns the account and the token's scope. The caller intersects that scope
with the account's own permissions — this package does not, because the
intersection belongs where permissions are resolved and doing it in two places
is how the two come to disagree.
*/
func (s *Service) Resolve(ctx context.Context, secret string) (dbgen.User, Token, error) {
	if !strings.HasPrefix(secret, prefix) {
		// Not one of ours. Refused without a query, which also keeps a session
		// token from being looked up here on every request.
		return dbgen.User{}, Token{}, ErrTokenInvalid
	}

	row, err := s.store.GetAPITokenByHash(ctx, hash(secret))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, Token{}, ErrTokenInvalid
	}
	if err != nil {
		return dbgen.User{}, Token{}, fmt.Errorf("look up token: %w", err)
	}

	/*
		A suspension applied after the token was issued takes effect at once.

		Checked here as well as at sign-in for the same reason sessions check
		it: a credential that kept working through a suspension would make the
		suspension advisory.
	*/
	if row.User.SuspendedAt != nil {
		return dbgen.User{}, Token{}, ErrTokenInvalid
	}

	token := asToken(row.ApiToken.ID, row.ApiToken.Label, row.ApiToken.Scope,
		row.ApiToken.ExpiresAt, row.ApiToken.LastUsedAt, row.ApiToken.CreatedAt)
	token.ApplicationID = row.ApiToken.ApplicationID

	return row.User, token, nil
}

/*
IssueForApplication creates a credential an application holds, and returns it
once.

The application is the owning subject: the token is tied to it, revoked with
it, and resolved against its grant. accountID is the account the application
acts through, which is what membership, suspension and moderation see.

No held permissions are passed, unlike Issue. A person's token is bounded by
what that person holds; an application's authority is what it was approved for,
and bounding it by anybody's permissions — the approver's above all — is
exactly the inheritance it must not have. Whoever approved it was checked for
holding each permission at approval; that check belongs there, not here.
*/
func (s *Service) IssueForApplication(
	ctx context.Context,
	applicationID, accountID uuid.UUID,
	label string,
	scope []authz.Permission,
) (Token, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return Token{}, "", ErrLabelRequired
	}
	for _, permission := range scope {
		if _, known := authz.Lookup(permission); !known {
			return Token{}, "", fmt.Errorf("%w: %q", ErrUnknownPermission, permission)
		}
	}

	secret, err := newToken()
	if err != nil {
		return Token{}, "", err
	}
	encoded, err := json.Marshal(names(scope))
	if err != nil {
		return Token{}, "", fmt.Errorf("encode scope: %w", err)
	}

	row, err := s.store.CreateApplicationToken(ctx, dbgen.CreateApplicationTokenParams{
		ID:            uuid.New(),
		UserID:        accountID,
		Label:         label,
		TokenHash:     hash(secret),
		Scope:         encoded,
		ApplicationID: &applicationID,
	})
	if err != nil {
		return Token{}, "", fmt.Errorf("create application token: %w", err)
	}

	token := asToken(row.ID, row.Label, encoded, nil, nil, row.CreatedAt)
	token.ApplicationID = &applicationID
	return token, secret, nil
}

/*
Touch records that a token was used, at minute resolution.

Rounded rather than exact, and skipped when the minute has not changed, because
the alternative is a write per request to support a list that says "last used
yesterday".
*/
func (s *Service) Touch(ctx context.Context, token Token) error {
	now := time.Now().Truncate(time.Minute)
	if token.LastUsedAt != nil && !token.LastUsedAt.Before(now) {
		return nil
	}

	if err := s.store.TouchAPIToken(ctx, dbgen.TouchAPITokenParams{
		ID:         token.ID,
		LastUsedAt: &now,
	}); err != nil {
		return fmt.Errorf("record token use: %w", err)
	}
	return nil
}

// List returns the tokens an account holds, without their secrets.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Token, error) {
	rows, err := s.store.ListAPITokensForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}

	tokens := make([]Token, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, asToken(row.ID, row.Label, row.Scope,
			row.ExpiresAt, row.LastUsedAt, row.CreatedAt))
	}
	return tokens, nil
}

/*
Revoke stops a token working, immediately.

Scoped to its owner, so somebody cannot revoke a token by guessing an
identifier — and a token that was not theirs is reported as not found rather
than as forbidden, which would confirm it exists.
*/
func (s *Service) Revoke(ctx context.Context, userID, tokenID uuid.UUID) (string, error) {
	row, err := s.store.RevokeAPIToken(ctx, dbgen.RevokeAPITokenParams{
		ID:     tokenID,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("revoke token: %w", err)
	}
	return row.Label, nil
}

// RevokeAll ends every token an account holds.
func (s *Service) RevokeAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.RevokeAPITokensForUser(ctx, userID); err != nil {
		return fmt.Errorf("revoke tokens: %w", err)
	}
	return nil
}

/*
Restrict returns what a token may actually do.

The account's permissions with everything outside the token's scope explicitly
denied. Denial rather than a fresh grant set, because an explicit denial beats
even server administration — so a narrow token on an administrator's account is
narrow, which is the whole point of issuing one.

Computed per request rather than stored, so a permission the account loses is
lost to its tokens at the same moment.
*/
func Restrict(held authz.Permissions, scope []authz.Permission) authz.Permissions {
	allowed := make(map[authz.Permission]bool, len(scope))
	for _, permission := range scope {
		allowed[permission] = true
	}

	grants := make([]authz.Grant, 0, len(authz.Catalogue()))
	for _, definition := range authz.Catalogue() {
		if allowed[definition.Name] {
			// Still subject to the account holding it: this asks the account's
			// own permissions rather than granting anything.
			if held.AllowsServer(definition.Name).Allowed {
				grants = append(grants, authz.Grant{Permission: definition.Name, Granted: true})
			}
			continue
		}
		grants = append(grants, authz.Grant{Permission: definition.Name, Granted: false})
	}

	return authz.Resolve(grants)
}

// checkScope refuses a scope naming something unknown or unheld.
func checkScope(scope []authz.Permission, held authz.Permissions) error {
	for _, permission := range scope {
		if _, known := authz.Lookup(permission); !known {
			return fmt.Errorf("%w: %q", ErrUnknownPermission, permission)
		}
		if !held.AllowsServer(permission).Allowed {
			return fmt.Errorf("%w: %q", ErrNotGranted, permission)
		}
	}
	return nil
}

// asToken builds the shape callers see from a stored row.
func asToken(
	id uuid.UUID,
	label string,
	scope []byte,
	expiresAt, lastUsedAt *time.Time,
	createdAt time.Time,
) Token {
	return Token{
		ID:         id,
		Label:      label,
		Scope:      parseScope(scope),
		ExpiresAt:  expiresAt,
		LastUsedAt: lastUsedAt,
		CreatedAt:  createdAt,
	}
}

/*
parseScope reads the stored scope.

Unreadable JSON yields an empty scope rather than an error, and empty means the
token can do nothing. A scope that cannot be understood must not be treated as
permission to do everything, which is what returning the account's full
authority would amount to.
*/
func parseScope(raw []byte) []authz.Permission {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil
	}

	scope := make([]authz.Permission, 0, len(names))
	for _, name := range names {
		scope = append(scope, authz.Permission(name))
	}
	return scope
}

// names renders a scope for storage.
func names(scope []authz.Permission) []string {
	out := make([]string, 0, len(scope))
	for _, permission := range scope {
		out = append(out, string(permission))
	}
	return out
}

// newToken generates one, prefixed so it is recognisable as a credential.
func newToken() (string, error) {
	buffer := make([]byte, tokenBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

/*
hash returns the stored form of a token.

Plain SHA-256, as for sessions: the value already has full entropy so there is
nothing to brute-force, and this runs on every request where a memory-hard hash
would be far too slow.
*/
func hash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
