package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

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

/*
The federation state this server keeps between requests.

Built once, lazily: a server with federation off never needs either, and
building them at construction would mean every test server carrying a cache it
does not use.
*/
func (s *Server) federationState() (*federation.Client, *federation.ReplayCache) {
	s.federationOnce.Do(func() {
		allowPrivate := false
		if s.settings != nil {
			allowPrivate = s.settings.Get(context.Background(), settings.FederationAllowPrivateAddresses) == "true"
		}
		s.federationPeers = federation.NewClient(allowPrivate)
		s.federationSeen = federation.NewReplayCache()
	})
	return s.federationPeers, s.federationSeen
}

/*
federationAuthority is what this server believes it is called.

Read from the operator's configuration, never from the request. The signature
covers the destination precisely so that a captured request cannot be replayed
elsewhere — and comparing it against a value the sender supplied would compare
the sender's claim with itself.
*/
func (s *Server) federationAuthority() string {
	authority, err := federation.Authority(s.config.CanonicalAddress)
	if err != nil {
		return ""
	}
	return authority
}

// peerOrigin is where a verified peer's identity is left on the context, so a
// handler reads a value this server established rather than a header.
const peerOrigin = "federation-peer"

/*
requireSignature refuses any federated request that is not properly signed.

Everything happens before the handler: the body is read and digested, the
peer's keys are fetched, the signature is verified against the destination this
server knows itself to be, and the signature is recorded so it cannot be used
twice. A failure at any point returns without the handler ever running, which
is what "refused and no state changes" requires.
*/
func (s *Server) requireSignature(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		request := c.Request()

		origin := strings.TrimSpace(request.Header.Get("TDC-Origin-Server"))
		if origin == "" {
			return s.refuseFederated(c, "no origin server named", federation.ErrNotSigned)
		}

		// Read the body here: verification needs it whole to check the digest,
		// and the handler needs it afterwards.
		body, err := io.ReadAll(io.LimitReader(request.Body, maxFederatedBody))
		if err != nil {
			return s.refuseFederated(c, "could not read the body", err)
		}
		request.Body = io.NopCloser(bytes.NewReader(body))

		authority := s.federationAuthority()
		if authority == "" {
			// Without a canonical address this server cannot know what it is
			// called, so it cannot check that a request was meant for it.
			return s.refuseFederated(c, "this server has no canonical address configured",
				errors.New("server.canonical_address is unset"))
		}

		peers, seen := s.federationState()

		document, err := peers.Discover(request.Context(), origin)
		if err != nil {
			return s.refuseFederated(c, "could not verify the origin server", err)
		}
		keyring, err := document.Keyring()
		if err != nil {
			return s.refuseFederated(c, "the origin publishes no usable keys", err)
		}

		signature, err := federation.Verify(request, body, keyring, authority, time.Now())
		if errors.Is(err, federation.ErrSignatureInvalid) {
			/*
				An unrecognised key is what a rotation looks like from here, so
				the peer's document is re-fetched once before the request is
				refused. A peer that did the right thing — rotated and
				published both halves — must not have to wait out our cache.
			*/
			if refreshed, refreshErr := peers.Refresh(request.Context(), origin); refreshErr == nil {
				if rotated, keyErr := refreshed.Keyring(); keyErr == nil {
					signature, err = federation.Verify(request, body, rotated, authority, time.Now())
				}
			}
		}
		if err != nil {
			return s.refuseFederated(c, "signature was not accepted", err)
		}

		// Seen once already, inside its window: valid in every other respect
		// and still a replay.
		if !seen.Remember(signature.Raw, signature.Expires) {
			return s.refuseFederated(c, "request has already been delivered",
				errors.New("replayed signature"))
		}

		// The origin is set by this server after verification, never trusted
		// from the header it arrived in.
		c.Set(peerOrigin, document.CanonicalAddress)
		return next(c)
	}
}

// maxFederatedBody caps what a peer can make this server hold in memory before
// it has proved who it is.
const maxFederatedBody = 8 << 20

/*
refuseFederated refuses a request and says why, in the log and to the caller.

Both, deliberately: the spec requires refusals be legible, and an operator
diagnosing why two servers will not talk is usually reading the receiving
server's log rather than the sender's response body.
*/
func (s *Server) refuseFederated(c *echo.Context, reason string, err error) error {
	s.log.Warn("refused a federated request",
		"reason", reason,
		"error", err,
		"path", c.Request().URL.Path,
		"origin", c.Request().Header.Get("TDC-Origin-Server"),
		"from", c.RealIP())
	return echo.NewHTTPError(http.StatusUnauthorized, reason)
}

/*
handlePing answers a signed request with nothing of substance.

Its whole purpose is to exercise signing and verification with no room, event
or membership involved, so that when something real rides on this transport a
failure is unambiguously in the thing riding rather than in the transport. It
is the last thing built in this phase and the first thing to break if the
phase is wrong.
*/
func (s *Server) handlePing(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)

	return c.JSON(http.StatusOK, map[string]any{
		"pong":   true,
		"origin": origin,
		"server": s.config.CanonicalAddress,
	})
}

/*
handleFederationPing asks a peer whether the signed transport works.

An operator's diagnostic, not part of the protocol. Enabling federation, adding
a peer and then waiting to see whether rooms replicate is a long way to
discover a clock is wrong or a key is unpublished; this answers that question
directly, in one request, before anything real depends on it.
*/
func (s *Server) handleFederationPing(c *echo.Context) error {
	var asked struct {
		Peer string `json:"peer"`
	}
	if err := c.Bind(&asked); err != nil || strings.TrimSpace(asked.Peer) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name the peer to ping")
	}

	if !s.federationEnabled(c) {
		return echo.NewHTTPError(http.StatusConflict, "federation is not enabled on this server")
	}
	if s.rooms == nil || s.rooms.Keyring() == nil {
		return echo.NewHTTPError(http.StatusConflict, "this server has no signing key")
	}
	if s.config.CanonicalAddress == "" {
		return echo.NewHTTPError(http.StatusConflict,
			"this server has no canonical address, so a peer could not verify it")
	}

	allowPrivate := s.settings != nil &&
		s.settings.Get(c.Request().Context(), settings.FederationAllowPrivateAddresses) == "true"

	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(), allowPrivate)

	if err := sender.Ping(c.Request().Context(), asked.Peer); err != nil {
		s.log.Warn("federation ping failed", "peer", asked.Peer, "error", err)
		return c.JSON(http.StatusOK, map[string]any{
			"reached": false,
			"peer":    asked.Peer,
			"reason":  err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{"reached": true, "peer": asked.Peer})
}
