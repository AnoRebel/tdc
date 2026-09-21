package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/apitokens"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
)

/*
The credentials a program carries.

Managed the way passkeys are — listed, created, revoked, all under /api/me,
because they belong to the account rather than to the server. What differs is
that the secret exists exactly once, in the response to the request that made
it, and nobody can recover it afterwards.
*/

// tokenView is one token as its owner sees it. Never the secret.
type tokenView struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Scope []string `json:"scope"`
	// Absent when it does not expire, which is legitimate for a deployment
	// that runs for years.
	Expires *time.Time `json:"expires,omitempty"`
	// Absent on one that has never been used, which is how somebody spots a
	// token they issued and forgot.
	LastUsed *time.Time `json:"lastUsed,omitempty"`
	Created  time.Time  `json:"created"`
}

// issuedTokenResponse is the one and only time the secret is sent.
type issuedTokenResponse struct {
	tokenView
	/*
		Token is the secret, shown once.

		Named plainly rather than hidden in a nested object: a client that has
		to dig for it is a client that stores the wrong thing. The accompanying
		warning is the interface's job, and it is the only chance anybody gets.
	*/
	Token string `json:"token"`
}

type createTokenRequest struct {
	Label string   `json:"label"`
	Scope []string `json:"scope"`
	// ExpiresAt is optional. Absent means it does not expire on its own.
	ExpiresAt *time.Time `json:"expiresAt"`
}

// handleListTokens returns the tokens an account holds.
func (s *Server) handleListTokens(c *echo.Context) error {
	user, _ := currentUser(c)

	if s.apiTokens == nil {
		return tokensUnavailable()
	}

	tokens, err := s.apiTokens.List(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	views := make([]tokenView, 0, len(tokens))
	for _, token := range tokens {
		views = append(views, viewOfToken(token))
	}
	return c.JSON(http.StatusOK, views)
}

/*
handleCreateToken issues one, and shows it once.

Refused to a caller that is itself a token. A program that can mint a new
credential can mint one outliving its own revocation, which would make
revoking a token something less than final — and the person who issued it would
have no way to know it had happened.
*/
func (s *Server) handleCreateToken(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	if s.apiTokens == nil {
		return tokensUnavailable()
	}
	if _, byToken := currentAPIToken(c); byToken {
		return echo.NewHTTPError(http.StatusForbidden,
			"a token cannot issue another token; sign in to do that")
	}

	var request createTokenRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	scope := make([]authz.Permission, 0, len(request.Scope))
	for _, name := range request.Scope {
		scope = append(scope, authz.Permission(name))
	}

	token, secret, err := s.apiTokens.Issue(ctx, user.ID, request.Label, scope,
		request.ExpiresAt, currentPermissions(c))
	switch {
	case errors.Is(err, apitokens.ErrLabelRequired):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, apitokens.ErrUnknownPermission):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, apitokens.ErrNotGranted):
		// Forbidden rather than bad request: the scope is a real permission,
		// and the refusal is about this account's authority.
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case err != nil:
		return err
	}

	s.recordModeration(c, user, audit.ActionTokenIssued, "api_token", token.ID.String(),
		map[string]any{"label": token.Label, "scope": request.Scope})

	return c.JSON(http.StatusCreated, issuedTokenResponse{
		tokenView: viewOfToken(token),
		Token:     secret,
	})
}

// handleRevokeToken stops one working, immediately.
func (s *Server) handleRevokeToken(c *echo.Context) error {
	user, _ := currentUser(c)

	if s.apiTokens == nil {
		return tokensUnavailable()
	}

	tokenID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such token")
	}

	label, err := s.apiTokens.Revoke(c.Request().Context(), user.ID, tokenID)
	if errors.Is(err, apitokens.ErrTokenInvalid) {
		// Missing rather than forbidden: a token belonging to somebody else
		// must not be confirmed to exist by the shape of the refusal.
		return echo.NewHTTPError(http.StatusNotFound, "no such token")
	}
	if err != nil {
		return err
	}

	s.recordModeration(c, user, audit.ActionTokenRevoked, "api_token", tokenID.String(),
		map[string]any{"label": label})

	return c.NoContent(http.StatusNoContent)
}

func viewOfToken(token apitokens.Token) tokenView {
	scope := make([]string, 0, len(token.Scope))
	for _, permission := range token.Scope {
		scope = append(scope, string(permission))
	}

	return tokenView{
		ID:       token.ID.String(),
		Label:    token.Label,
		Scope:    scope,
		Expires:  token.ExpiresAt,
		LastUsed: token.LastUsedAt,
		Created:  token.CreatedAt,
	}
}

func tokensUnavailable() error {
	return echo.NewHTTPError(http.StatusNotFound, "this server does not offer API tokens")
}
