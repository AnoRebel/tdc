package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// Sign-in limits.
//
// Per account and per source address, because either alone is evadable: an
// attacker with many addresses would defeat a per-address limit, and one
// spraying a common password across many accounts would defeat a per-account
// limit.
var (
	signInPerAccount = cache.RateLimit{
		Attempts:     8,
		Window:       15 * time.Minute,
		LockoutAfter: 15 * time.Minute,
	}
	signInPerAddress = cache.RateLimit{
		Attempts:     30,
		Window:       15 * time.Minute,
		LockoutAfter: 5 * time.Minute,
	}
	registrationPerAddress = cache.RateLimit{
		Attempts: 5,
		Window:   time.Hour,
	}
)

type signInRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	// Invitation is the code or token somebody was given, under a policy that
	// requires one.
	Invitation string `json:"invitation,omitempty"`
}

// userResponse is the public shape of an account.
//
// Deliberately narrow: it carries no email, no suspension detail and nothing
// else that is not the requester's business.
type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// Address is the federated form, user@server, which stays unambiguous once
	// servers exchange messages.
	Address string `json:"address"`
}

func (s *Server) userResponse(user dbgen.User) userResponse {
	return userResponse{
		ID:          user.ID.String(),
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Address:     user.Username + "@" + s.serverName(),
	}
}

// serverName is the host part of federated addresses.
func (s *Server) serverName() string {
	if s.config.CanonicalAddress != "" {
		return s.config.CanonicalAddress
	}
	return "localhost"
}

func (s *Server) handleRegister(c *echo.Context) error {
	ctx := c.Request().Context()
	address := requestIP(c)

	// Registration is rate limited per address so the account table cannot be
	// filled by one caller.
	if address.IsValid() {
		decision, err := s.limiter.Allow(ctx, "register:"+address.String(), registrationPerAddress)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	var request registerRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	// The operator's policy, checked before anything is created. Existing
	// accounts are untouched by a policy change; this governs making one.
	if err := s.mayRegister(ctx, request.Email, request.Invitation); err != nil {
		return policyRefusal(err)
	}

	// A server at its limit refuses cleanly rather than part way through.
	if err := s.withinUserLimit(ctx); err != nil {
		return err
	}

	user, err := s.identity.CreateUser(ctx, identity.NewUser{
		Username:    request.Username,
		Email:       request.Email,
		Password:    request.Password,
		DisplayName: request.DisplayName,
	})
	if err != nil {
		return registrationError(err)
	}

	// Recorded before the response, so an account never exists without the
	// record of its creation.
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionUserCreated,
		TargetKind: "user",
		TargetID:   user.ID.String(),
		SourceIP:   address,
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, s.userResponse(user))
}

// registrationError maps a registration failure to a response.
//
// Unlike authentication, registration may say that a name is taken: the person
// is choosing their own name and cannot proceed without knowing.
func registrationError(err error) error {
	switch {
	case errors.Is(err, identity.ErrUsernameTaken):
		return echo.NewHTTPError(http.StatusConflict, "that username is already taken")
	case errors.Is(err, identity.ErrEmailTaken):
		return echo.NewHTTPError(http.StatusConflict, "that email address is already registered")
	case errors.Is(err, identity.ErrInvalidUsername),
		errors.Is(err, identity.ErrWeakPassword):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	default:
		return err
	}
}

func (s *Server) handleSignIn(c *echo.Context) error {
	ctx := c.Request().Context()
	address := requestIP(c)

	var request signInRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	// The account key is normalised, so varying the case of an identifier
	// cannot be used to get a fresh allowance.
	accountKey := "signin:account:" + identity.NormaliseUsername(request.Username)

	decision, err := s.limiter.Allow(ctx, accountKey, signInPerAccount)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		s.recordRateLimited(ctx, c, request.Username, address)
		return retryAfter(c, decision.RetryAfter)
	}

	if address.IsValid() {
		addressDecision, err := s.limiter.Allow(ctx, "signin:address:"+address.String(), signInPerAddress)
		if err != nil {
			return err
		}
		if !addressDecision.Allowed {
			s.recordRateLimited(ctx, c, request.Username, address)
			return retryAfter(c, addressDecision.RetryAfter)
		}
	}

	session, err := s.identity.Authenticate(ctx,
		identity.Credentials{Username: request.Username, Password: request.Password},
		identity.DeviceContext{UserAgent: c.Request().UserAgent(), IPAddress: address},
		s.config.SessionLifetime,
	)
	if err != nil {
		if !errors.Is(err, identity.ErrAuthenticationFailed) {
			return err
		}

		// The attempted identifier is recorded, not the password.
		if recordErr := s.audit.Record(ctx, audit.Entry{
			ActorLabel: request.Username,
			Action:     audit.ActionSignInFailed,
			SourceIP:   address,
			UserAgent:  c.Request().UserAgent(),
		}); recordErr != nil {
			return recordErr
		}

		return echo.NewHTTPError(http.StatusUnauthorized, "the username or password is incorrect")
	}

	// A successful sign-in clears the account's count, so earlier failures do
	// not count against someone who then signs in correctly.
	if err := s.limiter.Reset(ctx, accountKey); err != nil {
		return err
	}

	user, err := s.identity.UserByID(ctx, session.Record.UserID)
	if err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionSignInSucceeded,
		SourceIP:   address,
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	setSessionCookie(c, session.Token, s.config.SecureCookies,
		int(time.Until(session.Record.ExpiresAt).Seconds()))

	return c.JSON(http.StatusOK, map[string]any{
		"user": s.userResponse(user),
		// The token is also returned in the body for clients that cannot use
		// cookies, such as the desktop application.
		"token":     session.Token,
		"expiresAt": session.Record.ExpiresAt,
	})
}

func (s *Server) recordRateLimited(ctx context.Context, c *echo.Context, username string, address netip.Addr) {
	// Failure to record a rate-limit event is logged but does not change the
	// outcome: the caller is being refused either way, and returning a
	// different error would tell them something about the server's state.
	if err := s.audit.Record(ctx, audit.Entry{
		ActorLabel: username,
		Action:     audit.ActionRateLimited,
		SourceIP:   address,
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		s.log.Error("rate limit event not recorded", "error", err)
	}
}

// retryAfter refuses a request and says when to try again.
func retryAfter(c *echo.Context, wait time.Duration) error {
	seconds := int(wait.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	c.Response().Header().Set("Retry-After", strconv.Itoa(seconds))
	return echo.NewHTTPError(http.StatusTooManyRequests, "too many attempts; try again later")
}

func (s *Server) handleSignOut(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	session, ok := currentSession(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication is required")
	}

	if err := s.identity.RevokeSession(ctx, user.ID, session.ID); err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionSignedOut,
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	clearSessionCookie(c, s.config.SecureCookies)
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleCurrentUser(c *echo.Context) error {
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication is required")
	}
	return c.JSON(http.StatusOK, s.userResponse(user))
}

type sessionResponse struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"userAgent"`
	IPAddress  string    `json:"ipAddress,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	// Current marks the session the request was made with, so a person can
	// tell which one they are about to end.
	Current bool `json:"current"`
}

func (s *Server) handleListSessions(c *echo.Context) error {
	user, _ := currentUser(c)
	current, _ := currentSession(c)

	sessions, err := s.identity.ListSessions(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		entry := sessionResponse{
			ID:         session.ID.String(),
			UserAgent:  session.UserAgent,
			CreatedAt:  session.CreatedAt,
			LastSeenAt: session.LastSeenAt,
			ExpiresAt:  session.ExpiresAt,
			Current:    session.ID == current.ID,
		}
		if session.IpAddress != nil {
			entry.IPAddress = session.IpAddress.String()
		}
		response = append(response, entry)
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleRevokeSession(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a valid session identifier")
	}

	if err := s.identity.RevokeSession(ctx, user.ID, sessionID); err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionSessionRevoked,
		TargetKind: "session",
		TargetID:   sessionID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
