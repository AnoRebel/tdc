package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/recovery"
)

/*
Getting back into an account whose password is gone.

Three routes: ask for a link, check one is still good, and use it. All
unauthenticated, because somebody who could sign in would not be here.

The shape of the first is dictated by one rule — recovery must not disclose
whether an account exists. That is not only about what the response says. It is
about answering the same way, in the same time, whatever was found, which is why
the mail goes out after the response rather than during it.
*/

/*
recoveryPerAddress bounds how often one caller may ask.

Mail costs the operator something and reaches somebody who did not ask for it, so
an unbounded form is a way to send mail through this server at anybody. Counted
per source address because there is no account to count against — that would be
the disclosure.
*/
var recoveryPerAddress = cache.RateLimit{
	Attempts:     5,
	Window:       time.Hour,
	LockoutAfter: time.Hour,
}

// recoveryRedeemPerAddress bounds guessing at a token. The token has far too
// much entropy to guess, so this is about the cost of trying rather than the
// chance of succeeding.
var recoveryRedeemPerAddress = cache.RateLimit{
	Attempts:     20,
	Window:       time.Hour,
	LockoutAfter: time.Hour,
}

type recoveryRequest struct {
	Email string `json:"email"`
}

/*
handleRequestRecovery starts a recovery.

Always answers the same way. Not "usually" — a caller that returned a different
status for an unknown address, or took visibly less time over one, would answer
the question this route exists to refuse.
*/
func (s *Server) handleRequestRecovery(c *echo.Context) error {
	if s.recovery == nil || !s.recovery.Available() {
		return echo.NewHTTPError(http.StatusNotFound, "this server cannot send recovery mail")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	if address.IsValid() {
		decision, err := s.limiter.Allow(ctx, "recovery:"+address.String(), recoveryPerAddress)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	var request recoveryRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	pending, err := s.recovery.Request(ctx, request.Email, address)
	if err != nil {
		return err
	}

	/*
		Sent after the answer, not before it.

		A mail server takes a noticeable moment, and only a request that found
		an account would wait for one — so a caller with a stopwatch could tell
		which addresses have accounts here from a route that discloses nothing
		in its body. Detaching the send removes the difference.
	*/
	if !pending.Nothing() {
		s.inBackground(ctx, "recovery mail", func(ctx context.Context) {
			if err := s.mail.Send(ctx, pending.Message); err != nil {
				// Logged, not returned: the caller has already been answered,
				// and the answer must not depend on this.
				s.log.Error("sending a recovery message", "error", err)
			}
		})

		if err := s.audit.Record(ctx, audit.Entry{
			ActorLabel: pending.To,
			Action:     audit.ActionRecoveryRequested,
			SourceIP:   address,
			UserAgent:  c.Request().UserAgent(),
		}); err != nil {
			return err
		}
	}

	/*
		The same words either way.

		Deliberately about what was done rather than what was found: "if there
		is an account" is the honest phrasing and the one that gives nothing
		away.
	*/
	return c.JSON(http.StatusAccepted, map[string]any{
		"message": "If there is an account with that address, a link to reset the password is on its way.",
	})
}

/*
handleCheckRecovery says whether a link is still worth a form.

Without it, somebody following an expired link types a new password twice and is
then told it was pointless. It spends nothing.
*/
func (s *Server) handleCheckRecovery(c *echo.Context) error {
	if s.recovery == nil || !s.recovery.Available() {
		return echo.NewHTTPError(http.StatusNotFound, "this server cannot send recovery mail")
	}

	err := s.recovery.Check(c.Request().Context(), c.QueryParam("token"))
	if errors.Is(err, recovery.ErrTokenInvalid) {
		return echo.NewHTTPError(http.StatusNotFound, "that link has been used already, or has expired")
	}
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{"usable": true})
}

type redeemRecoveryRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// handleRedeemRecovery spends a link and sets a new password.
func (s *Server) handleRedeemRecovery(c *echo.Context) error {
	if s.recovery == nil || !s.recovery.Available() {
		return echo.NewHTTPError(http.StatusNotFound, "this server cannot send recovery mail")
	}

	ctx := c.Request().Context()
	address := requestIP(c)

	if address.IsValid() {
		decision, err := s.limiter.Allow(ctx, "recovery:redeem:"+address.String(), recoveryRedeemPerAddress)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	var request redeemRecoveryRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	user, err := s.recovery.Redeem(ctx, request.Token, request.Password, s.identity)
	switch {
	case errors.Is(err, recovery.ErrTokenInvalid):
		return echo.NewHTTPError(http.StatusNotFound, "that link has been used already, or has expired")
	case errors.Is(err, identity.ErrWeakPassword):
		// Said plainly, because the link is still good and they can try again.
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case err != nil:
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionPasswordChanged,
		SourceIP:   address,
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	/*
		No session.

		Somebody who has just recovered an account signs in with the password
		they chose, which proves they know it and — where the account has one —
		goes through the second factor. Handing out a session here would skip
		both, and a recovery link would become a way past the second factor
		rather than a way past a forgotten password.
	*/
	return c.JSON(http.StatusOK, map[string]any{
		"message": "Your password has been changed. Sign in with it.",
	})
}

// sweepRecovery removes tokens nobody used.
func (s *Server) sweepRecovery(ctx context.Context) {
	if s.recovery == nil {
		return
	}
	if _, err := s.recovery.SweepExpired(ctx); err != nil {
		s.log.Warn("sweeping expired recovery tokens", "error", err)
	}
}
