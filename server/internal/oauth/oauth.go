// Package oauth signs people in through a provider somebody else runs.
//
// The flow has two halves that happen at different moments and, on the desktop
// client, in different processes: a request that sends somebody to the provider,
// and a callback that receives what the provider decided. Between them this
// package remembers, in the database, that the state value it issued was its
// own — which is the only thing standing between a real sign-in and a forged
// callback.
//
// Providers are read from the configuration rather than discovered over the
// network, so a self-hosted Forgejo or Keycloak works exactly like a commercial
// provider and an offline server starts as readily as a connected one.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/config"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// ErrUnknownProvider is returned for a provider this server does not offer.
// The name is echoed back because it came from the caller's own request.
var ErrUnknownProvider = errors.New("no such sign-in provider")

// ErrAuthorizationInvalid covers every way a callback can fail to match a
// sign-in this server started: an unknown state, one already used, one that has
// expired. They are one error for the same reason authentication failures are —
// distinguishing them tells a forger which guess was closer.
var ErrAuthorizationInvalid = errors.New("this sign-in did not come from here, or has expired")

// ErrProviderRefused means the provider did not complete the exchange. It
// wraps whatever it said, for the log, but what reaches the caller is that the
// sign-in did not happen.
var ErrProviderRefused = errors.New("the provider did not complete the sign-in")

// authorizationLifetime is how long somebody has to finish at the provider.
//
// Long enough to read a consent screen, create an account there, or find a
// second factor; short enough that an abandoned attempt is not a credential
// lying about. Ten minutes is what the OAuth2 security guidance suggests for
// exactly this value.
const authorizationLifetime = 10 * time.Minute

// Store is the database surface this package needs. Named rather than taking
// the generated queries directly so a test can supply a substitute.
type Store interface {
	StartOAuthAuthorization(ctx context.Context, arg dbgen.StartOAuthAuthorizationParams) (dbgen.OauthAuthorization, error)
	TakeOAuthAuthorization(ctx context.Context, stateHash []byte) (dbgen.OauthAuthorization, error)
	DeleteExpiredOAuthAuthorizations(ctx context.Context) (int64, error)
}

// Service runs sign-ins through configured providers.
type Service struct {
	providers map[string]config.OAuthProvider
	// order preserves what the operator wrote, so the buttons appear in the
	// order they configured rather than whatever the map iterates in.
	order  []string
	store  Store
	client *http.Client
	log    *slog.Logger
}

// NewService builds a provider service. A nil client means the default one,
// with a timeout, because a provider that never answers must not hold a request
// open indefinitely.
func NewService(providers []config.OAuthProvider, store Store, client *http.Client, log *slog.Logger) *Service {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	byName := make(map[string]config.OAuthProvider, len(providers))
	order := make([]string, 0, len(providers))
	for _, provider := range providers {
		byName[provider.Name] = provider
		order = append(order, provider.Name)
	}

	return &Service{providers: byName, order: order, store: store, client: client, log: log}
}

// Enabled says whether any provider is configured, so callers can leave the
// whole feature out of a response rather than sending an empty list.
func (s *Service) Enabled() bool { return s != nil && len(s.order) > 0 }

// Offered is what a sign-in screen needs to draw its buttons: a name to put in
// the URL and a label to show. Nothing secret is in it.
type Offered struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// Offered lists the providers this server signs people in through.
func (s *Service) Offered() []Offered {
	if !s.Enabled() {
		return nil
	}
	list := make([]Offered, 0, len(s.order))
	for _, name := range s.order {
		provider := s.providers[name]
		list = append(list, Offered{Name: provider.Name, Label: provider.Label})
	}
	return list
}

// Start describes a sign-in about to leave for the provider.
type Start struct {
	// URL is where the browser is sent.
	URL string
	// State is the value that must come back with the callback. It is returned
	// so a caller can show it or hand it to a desktop client; only its hash is
	// stored.
	State string
	// ExpiresAt is when this stops being accepted.
	ExpiresAt time.Time
}

// Begin starts a sign-in through the named provider.
//
// userID is set only when somebody already signed in is adding this provider to
// their account. Left nil, the callback is a sign-in or a sign-up, and the
// identity that comes back may not be attached to an account on the strength of
// an email alone.
func (s *Service) Begin(
	ctx context.Context,
	providerName string,
	callbackURL string,
	redirectTo string,
	userID *uuid.UUID,
) (Start, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return Start{}, fmt.Errorf("%w: %q", ErrUnknownProvider, providerName)
	}

	state, err := randomToken()
	if err != nil {
		return Start{}, err
	}

	/*
		PKCE, on every provider, whether or not it is a public client.

		The exchange happens on the server, so the classic reason for PKCE —
		a client that cannot keep a secret — does not apply. It is used anyway
		because it also defeats an authorization code injected into this
		server's callback: without the verifier, a code obtained elsewhere
		cannot be exchanged here. A provider that does not support PKCE ignores
		these two parameters, so nothing is lost by always sending them.
	*/
	verifier, err := randomToken()
	if err != nil {
		return Start{}, err
	}

	record, err := s.store.StartOAuthAuthorization(ctx, dbgen.StartOAuthAuthorizationParams{
		ID:         uuid.New(),
		StateHash:  hashToken(state),
		Verifier:   verifier,
		Provider:   provider.Name,
		RedirectTo: redirectTo,
		UserID:     userID,
		ExpiresAt:  time.Now().Add(authorizationLifetime),
	})
	if err != nil {
		return Start{}, fmt.Errorf("start authorization: %w", err)
	}

	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {provider.ClientID},
		"redirect_uri":          {callbackURL},
		"state":                 {state},
		"code_challenge":        {challengeFor(verifier)},
		"code_challenge_method": {"S256"},
	}
	if len(provider.Scopes) > 0 {
		query.Set("scope", strings.Join(provider.Scopes, " "))
	}

	return Start{
		URL:       provider.AuthURL + separatorFor(provider.AuthURL) + query.Encode(),
		State:     state,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

// separatorFor appends to an endpoint that may already carry a query string,
// which self-hosted providers often do.
func separatorFor(endpoint string) string {
	if strings.Contains(endpoint, "?") {
		return "&"
	}
	return "?"
}

// Identity is what a provider said about the person who just signed in.
type Identity struct {
	// Provider is the configured name this came from.
	Provider string
	// Subject is the provider's own identifier for the account. It is what the
	// credential is keyed on, because it is the only field a provider promises
	// not to reuse — an email or a username there may change or be released to
	// somebody else.
	Subject string
	// Email is what the provider asserted, which may be empty.
	Email string
	// EmailVerified is the provider's claim to have verified it. Every linking
	// decision turns on this, and it is never assumed.
	EmailVerified bool
	// Username and DisplayName are suggestions for a new account, not
	// guarantees. Either may be empty or already taken here.
	Username    string
	DisplayName string
}

// Completed is a callback that matched a sign-in this server started.
type Completed struct {
	Identity Identity
	// UserID is set when this was somebody already signed in adding a provider
	// to their account, and nil when it was a sign-in or a sign-up.
	UserID *uuid.UUID
	// RedirectTo is where to send them once this resolves, as it was recorded
	// when the sign-in began.
	RedirectTo string
}

// Complete exchanges a callback for what the provider knows about the person.
//
// The authorization is consumed as it is read, so a state value presented twice
// fails the second time even if both arrive at once.
func (s *Service) Complete(ctx context.Context, state, code, callbackURL string) (Completed, error) {
	if state == "" || code == "" {
		return Completed{}, ErrAuthorizationInvalid
	}

	record, err := s.store.TakeOAuthAuthorization(ctx, hashToken(state))
	if errors.Is(err, pgx.ErrNoRows) {
		return Completed{}, ErrAuthorizationInvalid
	}
	if err != nil {
		return Completed{}, fmt.Errorf("take authorization: %w", err)
	}

	// The provider is read from the stored row, not from the callback, so a
	// state issued for one provider cannot be redeemed at another.
	provider, ok := s.providers[record.Provider]
	if !ok {
		// Configured when the sign-in started, gone by the time it came back.
		return Completed{}, fmt.Errorf("%w: %q", ErrUnknownProvider, record.Provider)
	}

	token, err := s.exchange(ctx, provider, code, record.Verifier, callbackURL)
	if err != nil {
		return Completed{}, err
	}

	identity, err := s.fetchIdentity(ctx, provider, token)
	if err != nil {
		return Completed{}, err
	}

	return Completed{Identity: identity, UserID: record.UserID, RedirectTo: record.RedirectTo}, nil
}

// SweepExpired removes authorizations nobody completed. Called by the same
// periodic cleanup that expires invitations.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	if !s.Enabled() {
		return 0, nil
	}
	return s.store.DeleteExpiredOAuthAuthorizations(ctx)
}

// randomToken produces a value nobody can guess, encoded so it survives a URL
// and a database column unchanged.
func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// hashToken is what is stored, so a database read yields nothing usable. SHA-256
// rather than a password hash because the input is 256 bits of randomness:
// there is no dictionary to slow down.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// challengeFor is the PKCE S256 challenge for a verifier.
func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
