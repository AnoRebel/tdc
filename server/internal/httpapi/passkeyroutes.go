package httpapi

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/passkeys"
)

/*
Signing in with something held rather than something known.

Two ceremonies, two requests each. The enrolment pair is for somebody already
signed in — adding a passkey adds a way into an account, so it is done by
somebody who has proved they hold it. The sign-in pair is open, because it is
how somebody gets in.

The bodies here are what the browser's own WebAuthn API produces and consumes,
passed through rather than reshaped: they are defined by the specification, and
a client that has to translate them is a client that can translate them wrongly.
*/

// passkeySignInPerAddress bounds guessing at a sign-in.
//
// Tighter than passwords, because there is no account name to spread attempts
// across: every attempt is against whatever this server holds.
var passkeySignInPerAddress = cache.RateLimit{
	Attempts:     20,
	Window:       15 * time.Minute,
	LockoutAfter: 15 * time.Minute,
}

// passkeyEnrolPerUser bounds how many ceremonies one account may start. Each
// writes a row that lives five minutes; the allowance is generous because
// abandoning one is ordinary.
var passkeyEnrolPerUser = cache.RateLimit{
	Attempts: 30,
	Window:   15 * time.Minute,
}

// handleBeginPasskeyEnrolment asks an authenticator to make a passkey.
func (s *Server) handleBeginPasskeyEnrolment(c *echo.Context) error {
	if !s.passkeys.Enabled() {
		return passkeysUnavailable()
	}

	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	decision, err := s.limiter.Allow(ctx, "passkey:enrol:"+user.ID.String(), passkeyEnrolPerUser)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}

	enrolment, err := s.passkeys.BeginEnrolment(ctx, user.ID)
	if err != nil {
		return err
	}

	// The options as the specification defines them, for
	// navigator.credentials.create.
	return c.JSON(http.StatusOK, enrolment.Options)
}

// handleFinishPasskeyEnrolment stores the passkey that was just made.
func (s *Server) handleFinishPasskeyEnrolment(c *echo.Context) error {
	if !s.passkeys.Enabled() {
		return passkeysUnavailable()
	}

	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	/*
		The name goes in the query rather than the body.

		The body is the authenticator's response, exactly as the browser
		produced it, and the library parses it from the request itself; adding
		a field would mean reshaping what the browser made or reading the body
		twice. A header would do as well, but the shared transport both clients
		use carries a method, a path and a body — and widening that contract for
		one string is a worse trade than putting the string in the path.

		Not sensitive: it is a name somebody chose for their own device, and it
		goes to the same server in the same request as the credential itself.
	*/
	label := c.QueryParam("label")

	stored, err := s.passkeys.FinishEnrolment(ctx, c.Request(), label)
	if errors.Is(err, passkeys.ErrCeremonyInvalid) {
		return echo.NewHTTPError(http.StatusBadRequest, "that did not come from here, or has expired")
	}
	if err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionCredentialAdded,
		TargetKind: "credential",
		TargetID:   stored.ID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, passkeyResponse(stored))
}

// handleBeginPasskeySignIn issues a challenge for signing in.
func (s *Server) handleBeginPasskeySignIn(c *echo.Context) error {
	if !s.passkeys.Enabled() {
		return passkeysUnavailable()
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	if address.IsValid() {
		decision, err := s.limiter.Allow(ctx, "passkey:signin:"+address.String(), passkeySignInPerAddress)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	assertion, err := s.passkeys.BeginSignIn(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, assertion.Options)
}

// handleFinishPasskeySignIn verifies an assertion and starts a session.
func (s *Server) handleFinishPasskeySignIn(c *echo.Context) error {
	if !s.passkeys.Enabled() {
		return passkeysUnavailable()
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	user, err := s.passkeys.FinishSignIn(ctx, c.Request())
	if err != nil {
		if !errors.Is(err, passkeys.ErrCeremonyInvalid) && !errors.Is(err, passkeys.ErrNoPasskey) {
			return err
		}

		if recordErr := s.audit.Record(ctx, audit.Entry{
			Action:    audit.ActionSignInFailed,
			SourceIP:  address,
			UserAgent: c.Request().UserAgent(),
		}); recordErr != nil {
			return recordErr
		}

		// One refusal for every way it can fail, as everywhere else: saying
		// which would tell an unauthenticated caller something.
		return echo.NewHTTPError(http.StatusUnauthorized, "that passkey did not sign anybody in")
	}

	session, err := s.identity.StartSession(ctx, user.ID,
		identity.DeviceContext{UserAgent: c.Request().UserAgent(), IPAddress: address},
		s.config.SessionLifetime)
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
		// Also in the body, for clients that cannot use cookies.
		"token":     session.Token,
		"expiresAt": session.Record.ExpiresAt,
	})
}

// handleListPasskeys is somebody's own list of them.
func (s *Server) handleListPasskeys(c *echo.Context) error {
	if !s.passkeys.Enabled() {
		return passkeysUnavailable()
	}

	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	stored, err := s.passkeys.List(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	listed := make([]passkeyView, 0, len(stored))
	for _, row := range stored {
		listed = append(listed, passkeyResponse(row))
	}
	return c.JSON(http.StatusOK, listed)
}

type renamePasskeyRequest struct {
	Label string `json:"label"`
}

// handleRenamePasskey changes what somebody calls one of theirs.
func (s *Server) handleRenamePasskey(c *echo.Context) error {
	if !s.passkeys.Enabled() {
		return passkeysUnavailable()
	}

	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	credentialID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a passkey")
	}

	var request renamePasskeyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	renamed, err := s.passkeys.Rename(c.Request().Context(), user.ID, credentialID, request.Label)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, passkeyResponse(renamed))
}

// handleRevokePasskey removes one, unless it is the last way in.
func (s *Server) handleRevokePasskey(c *echo.Context) error {
	if !s.passkeys.Enabled() {
		return passkeysUnavailable()
	}

	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	credentialID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a passkey")
	}

	err = s.passkeys.Revoke(ctx, user.ID, credentialID)
	if errors.Is(err, passkeys.ErrLastCredential) {
		/*
			Said plainly, with what to do about it.

			This is the one refusal here that is not about an attacker: it is
			somebody tidying up who would otherwise lock themselves out, and
			they need to know that adding another way in first is what makes
			this possible.
		*/
		return echo.NewHTTPError(http.StatusConflict,
			"this is the only way left into your account — add a password or another passkey first")
	}
	if err != nil {
		return err
	}

	/*
		Every session but this one goes with it.

		A revoked credential must stop working, and a session it established
		outliving it would mean whoever holds that session still has what the
		revocation was meant to take away. This one is kept because the person
		doing the revoking is here and has not asked to be signed out.
	*/
	if session, ok := currentSession(c); ok {
		if err := s.identity.RevokeOtherSessions(ctx, user.ID, session.ID); err != nil {
			return err
		}
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionCredentialRemoved,
		TargetKind: "credential",
		TargetID:   credentialID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// passkeyView is one passkey as its owner sees it. No public key, no
// authenticator detail: what somebody needs is to tell theirs apart.
type passkeyView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Added string `json:"added"`
	// LastUsed is absent on one that never has been, which is how somebody
	// spots a passkey they enrolled and forgot.
	LastUsed *string `json:"lastUsed,omitempty"`
}

func passkeysUnavailable() error {
	return echo.NewHTTPError(http.StatusNotFound, "this server does not offer passkeys")
}

/*
passkeyResponse is what a passkey looks like to the person who enrolled it.

Deliberately narrow. The public key and the authenticator's own detail are not
in it: neither is a secret, but neither is any use to somebody deciding which of
their passkeys to remove, and a response carrying what nobody needs is a
response that grows fields nobody audits.
*/
func passkeyResponse(stored dbgen.Credential) passkeyView {
	view := passkeyView{
		ID:    stored.ID.String(),
		Label: stored.Label,
		Added: stored.CreatedAt.Format(time.RFC3339),
	}
	if stored.LastUsedAt != nil {
		used := stored.LastUsedAt.Format(time.RFC3339)
		view.LastUsed = &used
	}
	return view
}
