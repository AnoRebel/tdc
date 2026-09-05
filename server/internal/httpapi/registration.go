package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Registration policy: who may create an account here.

An operator's decision, changeable while the server runs. Closing registration
during a flood of spam accounts should not require a deployment — that is the
difference between a setting and a redeployment, and the person dealing with it
at two in the morning is the reason this is not a configuration file entry.
*/

var (
	// errRegistrationClosed is returned when nobody may register.
	errRegistrationClosed = errors.New("this server is not accepting new accounts")
	// errInvitationRequired is returned when only invited people may.
	errInvitationRequired = errors.New("an invitation is required to create an account here")
	// errDomainNotAllowed is returned when an address is outside the permitted
	// domains.
	errDomainNotAllowed = errors.New("accounts here are limited to particular email domains")
)

/*
mayRegister reports why registration is refused, or nil.

Existing accounts are never affected: this governs creating one, and somebody
who registered under an open policy keeps their account when it closes. That is
what makes a policy change safe to try.
*/
func (s *Server) mayRegister(ctx context.Context, email, invitation string) error {
	if s.settings == nil {
		// No settings service, so no policy: the default is open, and a server
		// that refused everybody because a service was not wired would be a
		// worse failure than one that accepted them.
		return nil
	}

	switch s.settings.Get(ctx, settings.RegistrationPolicy) {
	case "closed":
		return errRegistrationClosed

	case "invitation":
		// Invitations are not built yet, so this refuses everybody rather than
		// pretending to check one. An operator choosing it today is choosing
		// to close registration, which is at least what it looks like.
		if invitation == "" {
			return errInvitationRequired
		}
		return errInvitationRequired

	case "domain":
		return s.emailDomainAllowed(ctx, email)

	default:
		// "open", and anything unrecognised. A policy nobody can parse falls
		// back to the documented default rather than locking a server out of
		// its own registration.
		return nil
	}
}

/*
emailDomainAllowed checks an address against the permitted domains.

Compared case-insensitively on the domain only. Subdomains are not matched:
"example.com" permits somebody@example.com and not somebody@mail.example.com,
because an operator listing a domain is naming the one they control and a
subdomain may be somebody else entirely.
*/
func (s *Server) emailDomainAllowed(ctx context.Context, email string) error {
	permitted := s.settings.Get(ctx, settings.RegistrationDomains)
	if strings.TrimSpace(permitted) == "" {
		// The domain policy with no domains permits nobody. Reported as the
		// domain refusal rather than as closed, because that is what it is:
		// an operator who has chosen this and not finished configuring it.
		return errDomainNotAllowed
	}

	at := strings.LastIndex(email, "@")
	if at < 0 {
		return errDomainNotAllowed
	}
	domain := strings.ToLower(strings.TrimSpace(email[at+1:]))

	for _, allowed := range strings.Split(permitted, ",") {
		if domain != "" && domain == strings.ToLower(strings.TrimSpace(allowed)) {
			return nil
		}
	}

	return errDomainNotAllowed
}

/*
policyRefusal maps a policy refusal to a response.

Every one is 403 with its own message. A person refused registration needs to
know whether to ask for an invitation, use a different address, or give up —
and none of those is a client error to be corrected by retrying.
*/
func policyRefusal(err error) error {
	switch {
	case errors.Is(err, errRegistrationClosed):
		return echo.NewHTTPError(http.StatusForbidden,
			"this server is not accepting new accounts")
	case errors.Is(err, errInvitationRequired):
		return echo.NewHTTPError(http.StatusForbidden,
			"an invitation is required to create an account here")
	case errors.Is(err, errDomainNotAllowed):
		return echo.NewHTTPError(http.StatusForbidden,
			"accounts here are limited to particular email domains")
	default:
		return err
	}
}
