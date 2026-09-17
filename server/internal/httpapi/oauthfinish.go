package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/oauth"
)

/*
outcomeLifetime is how long a resolved sign-in waits to be collected.

The client is already navigating back when this is written, so the wait is
measured in seconds rather than minutes. Short because what may be parked here
is a session token: the window in which an intercepted handle is worth anything
should be about as long as it takes a page to load.
*/
const outcomeLifetime = 2 * time.Minute

// oauthOutcome is what the callback decided, before it is parked.
type oauthOutcome struct {
	Status     string
	RedirectTo string
	Identity   oauth.Identity
	// Token and ExpiresAt are set when the outcome is a session.
	Token     string
	ExpiresAt *time.Time
	User      *dbgen.User
	// LinkTo is the account a link is being offered for.
	LinkTo *dbgen.User
}

// parkedDetail is the JSON shape stored beside the status.
type parkedDetail struct {
	RedirectTo string     `json:"redirectTo,omitempty"`
	Token      string     `json:"token,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	UserID     string     `json:"userId,omitempty"`
	LinkToID   string     `json:"linkToId,omitempty"`

	Provider string `json:"provider,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Email    string `json:"email,omitempty"`
	// EmailVerified is carried through because accepting a link is only
	// offered on a verified address, and the finish route checks rather than
	// trusts that the callback got it right.
	EmailVerified bool   `json:"emailVerified,omitempty"`
	Username      string `json:"username,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
}

/*
finishAtClient parks the outcome and sends the browser back to the application.

The handle goes in the URL fragment rather than the query string. A fragment is
never sent to the server, never written to a proxy log and not included in a
referer, so the one part of this that is worth intercepting travels in the one
part that stays in the browser.
*/
func (s *Server) finishAtClient(c *echo.Context, outcome oauthOutcome) error {
	handle, err := randomHandle()
	if err != nil {
		return err
	}

	detail := parkedDetail{
		RedirectTo:    outcome.RedirectTo,
		Token:         outcome.Token,
		ExpiresAt:     outcome.ExpiresAt,
		Provider:      outcome.Identity.Provider,
		Subject:       outcome.Identity.Subject,
		Email:         outcome.Identity.Email,
		EmailVerified: outcome.Identity.EmailVerified,
		Username:      outcome.Identity.Username,
		DisplayName:   outcome.Identity.DisplayName,
	}
	if outcome.User != nil {
		detail.UserID = outcome.User.ID.String()
	}
	if outcome.LinkTo != nil {
		detail.LinkToID = outcome.LinkTo.ID.String()
	}

	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode the outcome: %w", err)
	}

	if _, err := s.queries.ParkOAuthOutcome(c.Request().Context(), dbgen.ParkOAuthOutcomeParams{
		ID:         uuid.New(),
		HandleHash: hashHandle(handle),
		Status:     outcome.Status,
		Detail:     encoded,
		ExpiresAt:  time.Now().Add(outcomeLifetime),
	}); err != nil {
		return fmt.Errorf("park the outcome: %w", err)
	}

	return c.Redirect(http.StatusFound, "/signin#oauth="+url.QueryEscape(handle))
}

// oauthFinishRequest is the client collecting what the callback decided.
type oauthFinishRequest struct {
	Handle string `json:"handle"`
	// Accept is the answer to a link offer. Sent only in reply to one, and
	// meaningless otherwise.
	Accept bool `json:"accept,omitempty"`
	// Password proves the person holds the account a link was offered for.
	// Without it, a verified email at a provider would be enough to take over
	// an account here, which is exactly what the offer exists to prevent.
	Password string `json:"password,omitempty"`
	// Username and DisplayName are what somebody chose for a new account,
	// having been shown the provider's suggestions.
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

/*
handleOAuthFinish collects a resolved sign-in and acts on it.

Separate from the callback because the callback answers a browser navigation and
this answers a client. It is also where the decisions that need an answer get
one: whether to link, and what a new account should be called.
*/
func (s *Server) handleOAuthFinish(c *echo.Context) error {
	if s.oauth == nil || !s.oauth.Enabled() {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not sign people in through other services")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	var request oauthFinishRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Handle == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "this sign-in cannot be completed")
	}

	parked, err := s.queries.CollectOAuthOutcome(ctx, hashHandle(request.Handle))
	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown, already collected, or expired. One answer for all three, for
		// the same reason authentication failures are one answer.
		return echo.NewHTTPError(http.StatusBadRequest, "this sign-in has already been completed, or has expired")
	}
	if err != nil {
		return err
	}

	var detail parkedDetail
	if err := json.Unmarshal(parked.Detail, &detail); err != nil {
		return fmt.Errorf("decode the outcome: %w", err)
	}

	switch parked.Status {
	case string(oauth.OutcomeSignedIn), string(oauth.OutcomeLinked):
		return s.respondWithParkedSession(c, parked.Status, detail)

	case string(oauth.OutcomeLinkOffered):
		return s.finishLink(c, request, detail, address)

	case string(oauth.OutcomeNewAccount):
		return s.finishNewAccount(c, request, detail, address)

	case "declined", "expired", "failed", "already-attached":
		return c.JSON(http.StatusOK, map[string]any{
			"status":     parked.Status,
			"redirectTo": detail.RedirectTo,
		})

	default:
		return echo.NewHTTPError(http.StatusBadRequest, "this sign-in cannot be completed")
	}
}

// respondWithParkedSession hands back a session the callback already issued.
func (s *Server) respondWithParkedSession(c *echo.Context, status string, detail parkedDetail) error {
	userID, err := uuid.Parse(detail.UserID)
	if err != nil {
		return fmt.Errorf("the parked outcome names no account: %w", err)
	}

	user, err := s.identity.UserByID(c.Request().Context(), userID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status": status,
		"user":   s.userResponse(user),
		// Returned in the body as well as the cookie, for clients that cannot
		// use cookies — which is what the desktop application is.
		"token":      detail.Token,
		"expiresAt":  detail.ExpiresAt,
		"redirectTo": detail.RedirectTo,
	})
}

/*
finishLink attaches a provider identity to an existing account.

The password is the point. A verified email at a provider says the person
controls that address; it does not say they hold the account here that uses it.
Requiring the password means the link is made by somebody who can already sign
in — which makes this a convenience, as it should be, rather than a way in.
*/
func (s *Server) finishLink(
	c *echo.Context,
	request oauthFinishRequest,
	detail parkedDetail,
	address netip.Addr,
) error {
	ctx := c.Request().Context()

	if !request.Accept {
		// Declining is a real answer: the person wants a separate account, not
		// the one that happens to share their address.
		return s.createAccountFromIdentity(c, request, detail, address)
	}

	// Never offered for an unverified address, and checked again here rather
	// than trusted from the parked row.
	if !detail.EmailVerified || detail.Email == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "this sign-in cannot be linked to an existing account")
	}

	linkTo, err := uuid.Parse(detail.LinkToID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "this sign-in cannot be linked to an existing account")
	}

	account, err := s.identity.UserByID(ctx, linkTo)
	if err != nil {
		return err
	}

	/*
		Proving the account, through the ordinary sign-in path.

		Deliberately the same code as signing in, so this inherits its rate
		limits, its suspension check and its single indistinguishable failure.
		A separate check here would be a second front door with different locks
		on it.
	*/
	if _, err := s.identity.Authenticate(ctx,
		identity.Credentials{Username: account.Username, Password: request.Password},
		identity.DeviceContext{UserAgent: c.Request().UserAgent(), IPAddress: address},
		s.config.SessionLifetime,
	); err != nil {
		if !errors.Is(err, identity.ErrAuthenticationFailed) {
			return err
		}
		if recordErr := s.audit.Record(ctx, audit.Entry{
			ActorLabel: account.Username,
			Action:     audit.ActionSignInFailed,
			SourceIP:   address,
			UserAgent:  c.Request().UserAgent(),
		}); recordErr != nil {
			return recordErr
		}
		return echo.NewHTTPError(http.StatusUnauthorized, "the password is incorrect")
	}

	if _, err := s.oauth.Attach(ctx, s.queries, account.ID, identityFrom(detail)); err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &account.ID,
		ActorLabel: account.Username,
		Action:     audit.ActionSignInSucceeded,
		SourceIP:   address,
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return s.issueSession(c, account, detail.RedirectTo, string(oauth.OutcomeLinked), address)
}

// finishNewAccount creates an account for an identity that matched nothing.
func (s *Server) finishNewAccount(
	c *echo.Context,
	request oauthFinishRequest,
	detail parkedDetail,
	address netip.Addr,
) error {
	return s.createAccountFromIdentity(c, request, detail, address)
}

func (s *Server) createAccountFromIdentity(
	c *echo.Context,
	request oauthFinishRequest,
	detail parkedDetail,
	address netip.Addr,
) error {
	ctx := c.Request().Context()

	// The operator's policy governs this as it governs any other registration:
	// signing in through a provider is a way of proving who you are, not a way
	// past who may join.
	if err := s.mayRegister(ctx, detail.Email, ""); err != nil {
		return policyRefusal(err)
	}
	if err := s.withinUserLimit(ctx); err != nil {
		return err
	}

	username := request.Username
	if username == "" {
		username = detail.Username
	}
	displayName := request.DisplayName
	if displayName == "" {
		displayName = detail.DisplayName
	}

	/*
		The email is carried onto the account only when the provider verified
		it.

		An unverified address would otherwise arrive here as a verified one by
		another route: the account is created with it, and the next sign-in
		finds a matching address on an account with no password.
	*/
	email := ""
	if detail.EmailVerified {
		email = detail.Email
	}

	user, err := s.identity.CreateUser(ctx, identity.NewUser{
		Username:    username,
		Email:       email,
		DisplayName: displayName,
		// Set in the same breath as the address itself, and by the same
		// condition, so the account cannot end up holding an address it is not
		// marked as having proved.
		EmailVerified: detail.EmailVerified,
	})
	if err != nil {
		return registrationError(err)
	}

	if _, err := s.oauth.Attach(ctx, s.queries, user.ID, identityFrom(detail)); err != nil {
		return err
	}

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

	return s.issueSession(c, user, detail.RedirectTo, string(oauth.OutcomeSignedIn), address)
}

// issueSession signs somebody in and answers in the shape every sign-in uses.
func (s *Server) issueSession(
	c *echo.Context,
	user dbgen.User,
	redirectTo, status string,
	address netip.Addr,
) error {
	session, err := s.identity.StartSession(c.Request().Context(), user.ID,
		identity.DeviceContext{UserAgent: c.Request().UserAgent(), IPAddress: address},
		s.config.SessionLifetime)
	if err != nil {
		return err
	}

	setSessionCookie(c, session.Token, s.config.SecureCookies,
		int(time.Until(session.Record.ExpiresAt).Seconds()))

	return c.JSON(http.StatusOK, map[string]any{
		"status":     status,
		"user":       s.userResponse(user),
		"token":      session.Token,
		"expiresAt":  session.Record.ExpiresAt,
		"redirectTo": redirectTo,
	})
}

// identityFrom reassembles what the provider said from the parked row.
func identityFrom(detail parkedDetail) oauth.Identity {
	return oauth.Identity{
		Provider:      detail.Provider,
		Subject:       detail.Subject,
		Email:         detail.Email,
		EmailVerified: detail.EmailVerified,
		Username:      detail.Username,
		DisplayName:   detail.DisplayName,
	}
}

// sweepOAuth removes authorizations and outcomes nobody came back for.
func (s *Server) sweepOAuth(ctx context.Context) {
	if s.oauth == nil || !s.oauth.Enabled() {
		return
	}
	if _, err := s.oauth.SweepExpired(ctx); err != nil {
		s.log.Warn("sweeping expired sign-in authorizations", "error", err)
	}
	if _, err := s.queries.DeleteExpiredOAuthOutcomes(ctx); err != nil {
		s.log.Warn("sweeping expired sign-in outcomes", "error", err)
	}
}

func randomHandle() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashHandle(handle string) []byte {
	sum := sha256.Sum256([]byte(handle))
	return sum[:]
}

/*
oauthSweepInterval is how often abandoned sign-ins are cleared.

Longer than either lifetime, because nothing depends on the sweep being prompt:
an expired row is already refused by the query that would have read it, so this
reclaims space rather than enforcing anything.
*/
const oauthSweepInterval = 5 * time.Minute

// sweepOAuthPeriodically clears abandoned sign-ins until the context ends.
func (s *Server) sweepOAuthPeriodically(ctx context.Context) {
	ticker := time.NewTicker(oauthSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOAuth(ctx)
		}
	}
}
