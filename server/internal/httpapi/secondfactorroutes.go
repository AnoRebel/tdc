package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/secondfactor"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Asking for something held, after something known.

The sign-in route above answers with a ticket where an account owes a second
factor, and the route here exchanges that ticket and a code for a session. The
rest is enrolment: setting up an app, proving it works, getting recovery codes,
and turning the whole thing off.
*/

/*
secondFactorOwed says whether this account must produce one.

Either because they set one up — the ordinary case, and the only one that can be
satisfied — or because the operator requires it of them. Those are different
situations and the sign-in route treats them differently: somebody who has a
factor is asked for it, and somebody who owes one but has not set it up is
signed in and told to, because refusing would lock out an account that has done
nothing wrong.
*/
func (s *Server) secondFactorOwed(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s.secondFactor == nil {
		return false, nil
	}
	return s.secondFactor.Required(ctx, userID)
}

/*
secondFactorRequiredOf says whether the operator's policy demands one of this
account, regardless of whether they have set one up.

Read per request rather than held, so a policy changed at two in the morning
takes effect without a restart — the same way every other policy here does.
*/
func (s *Server) secondFactorRequiredOf(c *echo.Context, userID uuid.UUID) (bool, error) {
	if s.settings == nil || s.secondFactor == nil {
		return false, nil
	}

	ctx := c.Request().Context()

	switch s.settings.Get(ctx, settings.SecondFactorPolicy) {
	case "everybody":
		return true, nil
	case "administrators":
		/*
			Anybody who can configure the server.

			That permission is the one worth protecting: it governs registration
			policy, limits and branding, and an attacker holding it can change
			the rules the rest of the protections rest on.
		*/
		held, err := s.authz.PermissionsFor(ctx, userID)
		if err != nil {
			return false, err
		}
		return held.AllowsServer(authz.PermServerConfigure).Allowed, nil
	default:
		return false, nil
	}
}

type secondFactorRequest struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

// handleSecondFactor exchanges a ticket and a code for a session.
func (s *Server) handleSecondFactor(c *echo.Context) error {
	if s.secondFactor == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not ask for a second factor")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	var request secondFactorRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	accepted, err := s.secondFactor.Complete(ctx, request.Ticket, request.Code)
	switch {
	case errors.Is(err, secondfactor.ErrTooManyAttempts):
		/*
			Said differently from a wrong code, on purpose.

			Somebody mistyping their own code needs to know why it stopped
			answering; without this they would try the next one and see the
			same refusal, and conclude their app had broken.
		*/
		return echo.NewHTTPError(http.StatusTooManyRequests,
			"too many codes have been tried — sign in again to start over")
	case errors.Is(err, secondfactor.ErrRejected):
		if recordErr := s.audit.Record(ctx, audit.Entry{
			Action:    audit.ActionSignInFailed,
			SourceIP:  address,
			UserAgent: c.Request().UserAgent(),
		}); recordErr != nil {
			return recordErr
		}
		// Says nothing about whether the password was right, which the caller
		// already knows, nor about whose account this is.
		return echo.NewHTTPError(http.StatusUnauthorized, "that code was not accepted")
	case err != nil:
		return err
	}

	user, err := s.identity.UserByID(ctx, accepted.UserID)
	if err != nil {
		return err
	}

	/*
		The device from the first request, not this one.

		The session records where the sign-in began. On a phone that has moved
		between networks between typing a password and reading a code, this
		request comes from somewhere else, and a session labelled with that
		would tell its owner something untrue about where they signed in.
	*/
	session, err := s.identity.StartSession(ctx, user.ID,
		identity.DeviceContext{UserAgent: accepted.UserAgent, IPAddress: accepted.Address},
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
		"user":      s.userResponse(user),
		"token":     session.Token,
		"expiresAt": session.Record.ExpiresAt,
		// Worth saying: somebody who reached for the printed list has one
		// fewer, and should know before they need the next.
		"usedRecoveryCode":  accepted.UsedRecoveryCode,
		"recoveryCodesLeft": accepted.RecoveryCodesLeft,
	})
}

// handleSecondFactorStatus is what somebody's own settings screen reads.
func (s *Server) handleSecondFactorStatus(c *echo.Context) error {
	if s.secondFactor == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not ask for a second factor")
	}

	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	enrolled, err := s.secondFactor.Required(ctx, user.ID)
	if err != nil {
		return err
	}

	remaining, err := s.secondFactor.RemainingRecoveryCodes(ctx, user.ID)
	if err != nil {
		return err
	}

	required, err := s.secondFactorRequiredOf(c, user.ID)
	if err != nil {
		return err
	}

	offers, err := s.secondFactor.Available(ctx, user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{
		"enrolled": enrolled,
		/*
			Which factors this account actually has.

			Separate from "enrolled", which is whether there is one at all: a
			settings screen needs to say that a passkey is already doing this
			job, or somebody sets up an authenticator app they did not need.
		*/
		"methods": map[string]any{
			"code":    offers.Code,
			"passkey": offers.Passkey,
			"email":   offers.Email,
		},
		// Whether the operator requires it of this account, so somebody is told
		// why they are being asked rather than left to wonder.
		"required":          required,
		"recoveryCodesLeft": remaining,
	})
}

// handleBeginSecondFactor issues a secret for an authenticator app.
func (s *Server) handleBeginSecondFactor(c *echo.Context) error {
	if s.secondFactor == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not ask for a second factor")
	}

	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	enrolment, err := s.secondFactor.BeginTOTP(c.Request().Context(), user.ID)
	if errors.Is(err, secondfactor.ErrAlreadyEnrolled) {
		return echo.NewHTTPError(http.StatusConflict, "an authenticator app is already set up")
	}
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{
		// Shown so it can be typed where a camera will not work, and encoded
		// as a QR code by the client — which knows the screen it has to fit.
		"secret": enrolment.Secret,
		"url":    enrolment.URL,
	})
}

type confirmSecondFactorRequest struct {
	Code string `json:"code"`
}

/*
handleConfirmSecondFactor finishes enrolment and issues recovery codes.

The codes come back here and nowhere else, because only their hashes are stored.
Somebody who does not write them down at this moment cannot be shown them again;
they can only be replaced.
*/
func (s *Server) handleConfirmSecondFactor(c *echo.Context) error {
	if s.secondFactor == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not ask for a second factor")
	}

	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	var request confirmSecondFactorRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	err := s.secondFactor.ConfirmTOTP(ctx, user.ID, request.Code)
	switch {
	case errors.Is(err, secondfactor.ErrNotEnrolled):
		return echo.NewHTTPError(http.StatusConflict, "start setting up an authenticator app first")
	case errors.Is(err, secondfactor.ErrAlreadyEnrolled):
		return echo.NewHTTPError(http.StatusConflict, "an authenticator app is already set up")
	case errors.Is(err, secondfactor.ErrRejected):
		return echo.NewHTTPError(http.StatusBadRequest,
			"that code did not match — check your device's clock is right")
	case err != nil:
		return err
	}

	codes, err := s.secondFactor.IssueRecoveryCodes(ctx, user.ID)
	if err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionCredentialAdded,
		TargetKind: "credential",
		TargetID:   "totp",
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, map[string]any{"recoveryCodes": codes})
}

// handleReissueRecoveryCodes replaces the batch, for somebody who has used
// several or lost the paper.
func (s *Server) handleReissueRecoveryCodes(c *echo.Context) error {
	if s.secondFactor == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not ask for a second factor")
	}

	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	enrolled, err := s.secondFactor.Required(ctx, user.ID)
	if err != nil {
		return err
	}
	if !enrolled {
		return echo.NewHTTPError(http.StatusConflict,
			"there is no second factor on this account to recover")
	}

	codes, err := s.secondFactor.IssueRecoveryCodes(ctx, user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, map[string]any{"recoveryCodes": codes})
}

type disableSecondFactorRequest struct {
	// Password, because turning off a protection is exactly the thing somebody
	// who has borrowed an unlocked screen would do.
	Password string `json:"password"`
}

// handleDisableSecondFactor removes the authenticator app and its codes.
func (s *Server) handleDisableSecondFactor(c *echo.Context) error {
	if s.secondFactor == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not ask for a second factor")
	}

	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	var request disableSecondFactorRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	/*
		The password again, through the same path signing in uses.

		A session is not enough to remove a factor: an unlocked laptop is a
		session, and the whole point of the factor is that holding one thing is
		not sufficient.
	*/
	if _, err := s.identity.VerifyPasswordOnly(ctx,
		identity.Credentials{Username: user.Username, Password: request.Password}); err != nil {
		if !errors.Is(err, identity.ErrAuthenticationFailed) {
			return err
		}
		return echo.NewHTTPError(http.StatusUnauthorized, "the password is incorrect")
	}

	/*
		An operator requiring it cannot have it turned off.

		Otherwise the policy would be a suggestion: anybody it applied to could
		remove the factor and carry on, which is not what an operator setting
		it meant.
	*/
	required, err := s.secondFactorRequiredOf(c, user.ID)
	if err != nil {
		return err
	}
	if required {
		return echo.NewHTTPError(http.StatusConflict,
			"this server requires a second factor on your account")
	}

	if err := s.secondFactor.DisableTOTP(ctx, user.ID); err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionCredentialRemoved,
		TargetKind: "credential",
		TargetID:   "totp",
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

/*
requireSecondFactorEnrolment stands between an account the operator requires a
factor of and the rest of the server.

The spec asks that somebody without one be asked to set one up before they can
use the server, and that an existing session not bypass it — which is the part
that needs middleware rather than a check at sign-in. A policy that only applied
to new sign-ins would leave every session opened before it was set unaffected,
for as long as those sessions last.

What is left reachable is what somebody needs to comply: their own account, the
enrolment routes, and signing out. Everything else answers with a refusal that
names the reason, so a client can send them to the right screen rather than
showing an error it cannot explain.
*/
func (s *Server) requireSecondFactorEnrolment(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		user, ok := currentUser(c)
		if !ok || s.secondFactor == nil {
			return next(c)
		}

		// The routes that exist to get somebody out of this state.
		if allowedWithoutSecondFactor(c.Request().URL.Path) {
			return next(c)
		}

		/*
			A program carrying a token is not somebody who can enrol.

			The requirement exists so that a person signing in proves two
			things; a token cannot open an authenticator app, so blocking it
			here would not add a factor, it would only stop every integration
			working on the day an operator turns the policy on. The second
			factor was proved by the session that issued the token, and the
			token carries only what it was granted.
		*/
		if _, byToken := currentAPIToken(c); byToken {
			return next(c)
		}

		// Nor is a guest of a call, who has no account to secure — only one
		// call, which the guest gate already confines them to.
		if user.GuestCall != nil {
			return next(c)
		}

		required, err := s.secondFactorRequiredOf(c, user.ID)
		if err != nil {
			return err
		}
		if !required {
			return next(c)
		}

		enrolled, err := s.secondFactor.Required(c.Request().Context(), user.ID)
		if err != nil {
			return err
		}
		if enrolled {
			return next(c)
		}

		return echo.NewHTTPError(http.StatusForbidden,
			"this server requires a second factor on your account — set one up to continue")
	}
}

/*
allowedWithoutSecondFactor is the short list somebody must still reach.

Their own account, so a client can draw the screen that tells them what is
wrong; the second-factor routes, so they can comply; and signing out, because
being unable to leave is not a protection.

Read as a prefix rather than an exact match, since the enrolment routes are
several and all of them are needed.
*/
func allowedWithoutSecondFactor(path string) bool {
	for _, allowed := range []string{
		"/api/me/second-factor",
		"/api/me/permissions",
		"/api/auth/signout",
		"/api/server",
		"/api/version",
		"/api/health",
	} {
		if path == allowed || strings.HasPrefix(path, allowed+"/") {
			return true
		}
	}
	// Exactly /api/me, not everything under it: the rest of somebody's account
	// is the server they are being kept out of.
	return path == "/api/me"
}

/*
Answering a second-factor challenge with a passkey.

Two requests, like every other passkey ceremony: one for a challenge, one with
what the authenticator did with it. What makes these different from signing in
with a passkey alone is the ticket — the password has already been checked, and
this is the second half rather than the whole.

Somebody who carries a passkey has the thing a second factor is for. Requiring
them to carry an authenticator app as well is ceremony, and ceremony is what
makes people turn two-step sign-in off.
*/

// handleBeginSecondFactorPasskey issues a challenge for the second step.
func (s *Server) handleBeginSecondFactorPasskey(c *echo.Context) error {
	if s.secondFactor == nil || !s.passkeys.Enabled() {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not offer that")
	}

	assertion, err := s.passkeys.BeginSignIn(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, assertion.Options)
}

/*
handleFinishSecondFactorPasskey spends a ticket that a passkey answered.

The ticket travels in the query rather than the body, because the body is the
authenticator's own response — parsed by the library from the request itself —
and adding a field would mean reshaping what the browser just made.
*/
func (s *Server) handleFinishSecondFactorPasskey(c *echo.Context) error {
	if s.secondFactor == nil || !s.passkeys.Enabled() {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not offer that")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	ticket := c.QueryParam("ticket")
	if ticket == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "this sign-in cannot be completed")
	}

	/*
		The passkey is verified first, and says whose account it proved.

		Which account it is allowed to prove is the next question, and a
		separate one: a valid passkey belonging to somebody else must not
		complete this ticket.
	*/
	proved, err := s.passkeys.FinishSignIn(ctx, c.Request())
	if err != nil {
		if recordErr := s.audit.Record(ctx, audit.Entry{
			Action:    audit.ActionSignInFailed,
			SourceIP:  address,
			UserAgent: c.Request().UserAgent(),
		}); recordErr != nil {
			return recordErr
		}
		return echo.NewHTTPError(http.StatusUnauthorized, "that passkey was not accepted")
	}

	accepted, err := s.secondFactor.CompleteWithPasskey(ctx, ticket, proved.ID)
	if errors.Is(err, secondfactor.ErrRejected) {
		// Covers both a ticket that is not usable and a passkey for the wrong
		// account, deliberately: which of the two is not the caller's business.
		return echo.NewHTTPError(http.StatusUnauthorized, "that passkey was not accepted")
	}
	if err != nil {
		return err
	}

	user, err := s.identity.UserByID(ctx, accepted.UserID)
	if err != nil {
		return err
	}

	// The device from the first request, as the code path does: the session
	// records where the sign-in began rather than where it finished.
	session, err := s.identity.StartSession(ctx, user.ID,
		identity.DeviceContext{UserAgent: accepted.UserAgent, IPAddress: accepted.Address},
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
		"user":              s.userResponse(user),
		"token":             session.Token,
		"expiresAt":         session.Record.ExpiresAt,
		"recoveryCodesLeft": accepted.RecoveryCodesLeft,
	})
}

/*
Sending a code to a confirmed address, for somebody with no phone to hand.

The weakest of the three factors, and the one offered last. It is only as good
as the mailbox — somebody who has taken over an inbox has taken over this — and
it exists because the alternative for somebody whose phone is lost is writing to
an administrator.

Offered only where the server can send mail and the account's address has been
confirmed. An unconfirmed address is one somebody typed into a form, and mailing
a way past the second factor to it would undo the factor.
*/

type emailedCodeRequest struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code,omitempty"`
}

// handleSendEmailedCode mails a code for a pending sign-in.
func (s *Server) handleSendEmailedCode(c *echo.Context) error {
	if s.secondFactor == nil || s.mail == nil || !s.mail.Configured() {
		return echo.NewHTTPError(http.StatusNotFound, "this server cannot send a code by mail")
	}

	ctx := c.Request().Context()

	var request emailedCodeRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	pending, err := s.secondFactor.PendingFor(ctx, request.Ticket)
	if errors.Is(err, secondfactor.ErrRejected) {
		return echo.NewHTTPError(http.StatusUnauthorized, "this sign-in cannot be completed")
	}
	if err != nil {
		return err
	}

	user, err := s.identity.UserByID(ctx, pending)
	if err != nil {
		return err
	}

	/*
		Only a confirmed address.

		An unconfirmed one is something somebody typed into a form, and mailing
		a way past the second factor to it would make "put an address in your
		profile" a way round the factor entirely.
	*/
	if !user.EmailVerified || user.Email == nil {
		return echo.NewHTTPError(http.StatusConflict,
			"this account has no confirmed email address to send a code to")
	}

	message, err := s.secondFactor.SendEmailedCode(ctx, request.Ticket, *user.Email, s.serverName())
	if errors.Is(err, secondfactor.ErrRejected) {
		return echo.NewHTTPError(http.StatusUnauthorized, "this sign-in cannot be completed")
	}
	if err != nil {
		return err
	}

	// After the response, as the recovery mail is: the caller must not learn
	// anything from how long this took.
	s.inBackground(ctx, "second factor code", func(ctx context.Context) {
		if err := s.mail.Send(ctx, message); err != nil {
			s.log.Error("sending a sign-in code", "error", err)
		}
	})

	return c.JSON(http.StatusAccepted, map[string]any{
		"message": "A code is on its way to the address on your account.",
	})
}

// handleEmailedCode spends a ticket that an emailed code answered.
func (s *Server) handleEmailedCode(c *echo.Context) error {
	if s.secondFactor == nil || s.mail == nil || !s.mail.Configured() {
		return echo.NewHTTPError(http.StatusNotFound, "this server cannot send a code by mail")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	var request emailedCodeRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	accepted, err := s.secondFactor.CompleteWithEmailedCode(ctx, request.Ticket, request.Code)
	switch {
	case errors.Is(err, secondfactor.ErrTooManyAttempts):
		return echo.NewHTTPError(http.StatusTooManyRequests,
			"too many codes have been tried — sign in again to start over")
	case errors.Is(err, secondfactor.ErrRejected):
		if recordErr := s.audit.Record(ctx, audit.Entry{
			Action:    audit.ActionSignInFailed,
			SourceIP:  address,
			UserAgent: c.Request().UserAgent(),
		}); recordErr != nil {
			return recordErr
		}
		return echo.NewHTTPError(http.StatusUnauthorized, "that code was not accepted")
	case err != nil:
		return err
	}

	user, err := s.identity.UserByID(ctx, accepted.UserID)
	if err != nil {
		return err
	}

	session, err := s.identity.StartSession(ctx, user.ID,
		identity.DeviceContext{UserAgent: accepted.UserAgent, IPAddress: accepted.Address},
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
		"user":              s.userResponse(user),
		"token":             session.Token,
		"expiresAt":         session.Record.ExpiresAt,
		"recoveryCodesLeft": accepted.RecoveryCodesLeft,
	})
}
