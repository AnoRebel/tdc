package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/oauth"
)

/*
Signing in through a provider somebody else runs.

Three routes, and the shape of them is dictated by where each half happens. The
start is an ordinary request from the client. The callback is a navigation from
the provider — it arrives at this server as a browser hitting a URL, with no way
to carry a JSON body or read one, so it answers with a redirect. The finish is
the client coming back to collect what the callback decided.

That last step exists because the callback cannot always answer the client
directly. On the desktop the callback lands in the system browser while the
application waits somewhere else entirely, and even on the web the outcome may
be a question — link this to an existing account? — rather than a session.
*/

// oauthStartPerAddress bounds how many sign-ins one caller may begin.
//
// Each start writes a row and each row costs a little until it expires. The
// allowance is generous because starting one is not an attempt at anything:
// people change their minds at the consent screen, and a phone that loses
// signal mid-flow starts again.
var oauthStartPerAddress = cache.RateLimit{
	Attempts: 20,
	Window:   10 * time.Minute,
}

// oauthCallbackPerAddress bounds guessing at state values. Unlike starting, a
// wrong callback is always either a mistake or an attempt, so this is tight.
var oauthCallbackPerAddress = cache.RateLimit{
	Attempts:     20,
	Window:       10 * time.Minute,
	LockoutAfter: 10 * time.Minute,
}

type oauthStartResponse struct {
	// URL is where the client sends the person: a browser navigates to it, the
	// desktop client opens it in the system browser.
	URL string `json:"url"`
	// State is returned so the desktop client knows which sign-in to collect
	// once the person comes back. It is not a credential on its own — the
	// callback must still arrive with a code from the provider.
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleOAuthStart begins a sign-in through a provider.
func (s *Server) handleOAuthStart(c *echo.Context) error {
	if s.oauth == nil || !s.oauth.Enabled() {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not sign people in through other services")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	if address.IsValid() {
		decision, err := s.limiter.Allow(ctx, "oauth:start:"+address.String(), oauthStartPerAddress)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	provider := c.Param("provider")

	/*
		Somebody already signed in is adding a provider to their account, not
		signing in. Recorded now, in the row, rather than read at the callback:
		the callback arrives as a fresh navigation and may carry no session at
		all — on the desktop it lands in a different browser entirely.
	*/
	var userID *uuid.UUID
	if user, ok := currentUser(c); ok {
		id := user.ID
		userID = &id
	}

	start, err := s.oauth.Begin(ctx, provider, s.oauthCallbackURL(), s.safeRedirect(c.QueryParam("next")), userID)
	if errors.Is(err, oauth.ErrUnknownProvider) {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not offer that sign-in provider")
	}
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, oauthStartResponse{
		URL:       start.URL,
		State:     start.State,
		ExpiresAt: start.ExpiresAt,
	})
}

/*
handleOAuthCallback receives the provider's decision.

It answers with a redirect rather than JSON because what arrives here is a
browser navigating, not a client calling: whatever it returns is rendered as a
page. So it resolves the sign-in, parks the result, and sends the browser to the
client with a handle to collect it by.

Nothing about the outcome travels in that URL — not whether an account matched,
not the email the provider asserted. Query strings end up in history, in
referrers and in proxy logs, and "there is already an account with this address"
is not something to write into all three.
*/
func (s *Server) handleOAuthCallback(c *echo.Context) error {
	if s.oauth == nil || !s.oauth.Enabled() {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not sign people in through other services")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	// Carried onto every outcome below, so a client with no browser to receive
	// the redirect can claim the sign-in it started.
	state := c.QueryParam("state")

	if address.IsValid() {
		decision, err := s.limiter.Allow(ctx, "oauth:callback:"+address.String(), oauthCallbackPerAddress)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	// The provider refused, or the person declined at the consent screen.
	// Neither is an error here: they are told, and the sign-in simply did not
	// happen.
	if refusal := c.QueryParam("error"); refusal != "" {
		s.log.Info("a sign-in was declined at the provider", "error", refusal)
		return s.finishAtClient(c, oauthOutcome{Status: "declined", State: state})
	}

	completed, err := s.oauth.Complete(ctx, state, c.QueryParam("code"), s.oauthCallbackURL())
	switch {
	case errors.Is(err, oauth.ErrAuthorizationInvalid):
		return s.finishAtClient(c, oauthOutcome{Status: "expired", State: state})
	case errors.Is(err, oauth.ErrProviderRefused), errors.Is(err, oauth.ErrUnknownProvider):
		s.log.Warn("a sign-in through a provider could not be completed", "error", err)
		return s.finishAtClient(c, oauthOutcome{Status: "failed", State: state})
	case err != nil:
		return err
	}

	resolution, err := s.oauth.Resolve(ctx, s.queries, completed)
	if errors.Is(err, oauth.ErrIdentityBelongsToAnother) {
		return s.finishAtClient(c, oauthOutcome{Status: "already-attached", RedirectTo: completed.RedirectTo, State: state})
	}
	if err != nil {
		return err
	}

	outcome := oauthOutcome{RedirectTo: completed.RedirectTo, Identity: resolution.Identity, State: state}

	switch resolution.Outcome {
	case oauth.OutcomeSignedIn, oauth.OutcomeLinked:
		session, err := s.identity.StartSession(ctx, resolution.User.ID,
			identity.DeviceContext{UserAgent: c.Request().UserAgent(), IPAddress: address},
			s.config.SessionLifetime)
		if err != nil {
			return err
		}

		if err := s.audit.Record(ctx, audit.Entry{
			ActorID:    &resolution.User.ID,
			ActorLabel: resolution.User.Username,
			Action:     audit.ActionSignInSucceeded,
			SourceIP:   address,
			UserAgent:  c.Request().UserAgent(),
		}); err != nil {
			return err
		}

		// Set here as well as handed back at the finish, so a web client is
		// signed in the moment the redirect lands.
		setSessionCookie(c, session.Token, s.config.SecureCookies,
			int(time.Until(session.Record.ExpiresAt).Seconds()))

		outcome.Status = string(resolution.Outcome)
		outcome.Token = session.Token
		outcome.ExpiresAt = &session.Record.ExpiresAt
		outcome.User = &resolution.User

	case oauth.OutcomeLinkOffered:
		/*
			An account with this verified address exists, and the person is
			asked rather than signed in.

			What is parked is the identity and the account it might attach to.
			Accepting means proving they hold that account — the finish route
			will not attach anything on the strength of this outcome alone.
		*/
		outcome.Status = string(resolution.Outcome)
		outcome.LinkTo = &resolution.LinkTo

	case oauth.OutcomeNewAccount:
		outcome.Status = string(resolution.Outcome)
	}

	return s.finishAtClient(c, outcome)
}

// oauthCallbackURL is where providers are told to return to.
//
// Built from the canonical address rather than the request, because the value
// must match what was registered with the provider exactly, and because taking
// it from a header would let a caller choose where the code is delivered.
func (s *Server) oauthCallbackURL() string {
	base := strings.TrimSuffix(s.config.CanonicalAddress, "/")
	if base == "" {
		return "/api/auth/oauth/callback"
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return base + "/api/auth/oauth/callback"
}

/*
safeRedirect keeps a "where to go afterwards" from becoming an open redirect.

Only a path within this application is allowed. Anything absolute, anything
protocol-relative, anything that is not plainly a path here becomes the default,
because the one thing this value must not do is send somebody from a page
carrying this server's name to somewhere else that looks like it.

Checked when the sign-in starts rather than when it finishes, so a row that
exists is a row that is safe to act on.
*/
func (s *Server) safeRedirect(next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return ""
	}
	// "//host" is protocol-relative and goes off-site; "/\host" is the same
	// trick with the other slash, which some parsers normalise.
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return ""
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return ""
	}
	return parsed.EscapedPath() + optionalQuery(parsed)
}

func optionalQuery(parsed *url.URL) string {
	if parsed.RawQuery == "" {
		return ""
	}
	return "?" + parsed.RawQuery
}
