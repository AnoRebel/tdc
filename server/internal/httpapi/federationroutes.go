package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
What this server publishes to peers, and what it refuses them.

The discovery document is served whether or not federation is enabled, and
that is deliberate. A server with federation off still has an identity and
still has to be able to say so: a peer that gets a connection refused learns
nothing it can act on, where a peer told "this server does not federate" has
an answer an operator can read in a log. Everything else federated is gated —
the refusal happens before a handler, so a disabled server does no work on a
federated request beyond saying no.
*/

// federationEnabled reports whether the operator turned federation on.
func (s *Server) federationEnabled(c *echo.Context) bool {
	if s.settings == nil {
		return false
	}
	return s.settings.Get(c.Request().Context(), settings.FederationEnabled) == "true"
}

/*
requireFederation refuses every federated route while federation is off.

Applied as middleware rather than checked inside each handler, so that adding a
federated endpoint cannot accidentally omit the check — the gate is a property
of the route group, not a line somebody has to remember to write.
*/
func (s *Server) requireFederation(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !s.federationEnabled(c) {
			return echo.NewHTTPError(http.StatusNotFound, "this server does not federate")
		}
		return next(c)
	}
}

/*
handleWellKnown serves this server's identity to anyone who asks.

Unauthenticated and ungated by design: it is the document a peer reads before
it has any way to authenticate to us, and the only thing that makes it
trustworthy is that it is served from the domain it claims. It publishes
nothing private — an address the operator already chose to publish, public
keys, and which protocol versions this server speaks.
*/
func (s *Server) handleWellKnown(c *echo.Context) error {
	document := federation.Document{
		CanonicalAddress: s.config.CanonicalAddress,
		Versions:         federation.SupportedVersions(),
		Enabled:          s.federationEnabled(c),
	}

	// The keys are the event log's own published set, retired ones included:
	// a peer that could not verify what a retired key signed would find the
	// history it already holds becoming unverifiable.
	if s.rooms != nil {
		if keyring := s.rooms.Keyring(); keyring != nil {
			document.Keys = keyring.Published()
		}
	}

	return c.JSON(http.StatusOK, document)
}

/*
handleFederationVersion agrees a protocol version with a peer.

The first genuinely federated route, and so the first thing requireFederation
guards: a server with federation off has no version to agree on, and refusing
here is how a peer learns that rather than by inferring it from a timeout.

A peer sends the versions it speaks; the answer is the newest both sides
accept. Our own preference order decides, so that the same pair of servers
reaches the same answer regardless of which one asked.
*/
func (s *Server) handleFederationVersion(c *echo.Context) error {
	var asked struct {
		Versions []string `json:"versions"`
	}
	if err := c.Bind(&asked); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the versions offered")
	}

	agreed, err := federation.Negotiate(federation.SupportedVersions(), asked.Versions)
	if err != nil {
		// Logged as well as returned: an operator diagnosing why two servers
		// will not talk needs this on the server, not only in the caller's
		// response body.
		s.log.Warn("refused a peer with no protocol version in common",
			"peer", c.RealIP(),
			"offered", asked.Versions,
			"supported", federation.SupportedVersions())
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"version": agreed})
}
