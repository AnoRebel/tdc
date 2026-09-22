package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/events"
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
federationTrust reads the operator's answer to "which peers?".

Read whole, once per request, so that one decision cannot see the mode from
before an operator's change and the lists from after it. Settings are cached in
the settings service, so this costs nothing per request and takes effect on the
next one after a change — no restart, which is what the requirement asks.
*/
func (s *Server) federationTrust(ctx context.Context) federation.Trust {
	if s.settings == nil {
		return federation.Trust{Mode: federation.ModeOpen}
	}
	return federation.Trust{
		Mode:    federation.ParseMode(s.settings.Get(ctx, settings.FederationMode)),
		Allowed: federation.SplitList(s.settings.Get(ctx, settings.FederationAllowedServers)),
		Blocked: federation.SplitList(s.settings.Get(ctx, settings.FederationBlockedServers)),
	}
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

		/*
			Refused before the signature is even looked at.

			A peer the operator has refused should cost this server the price
			of a string comparison, not of fetching its keys and verifying
			Ed25519 over a body it was never going to accept.
		*/
		if allowed, why := s.federationTrust(request.Context()).Allows(origin); !allowed {
			return s.refuseFederated(c, "this server does not federate with that peer", why)
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

	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(),
		s.federationTrust(c.Request().Context()), allowPrivate)

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

/*
maxEventsPerBatch bounds one delivery or catch-up response.

A peer that has been away for a week asks repeatedly rather than receiving a
week in one response — which would mean holding it all in memory on both sides
to serve a request that will probably time out anyway.
*/
const maxEventsPerBatch = 200

/*
handleFederatedEvents serves a room's events to a peer participating in it.

The peer is the verified origin of the signed request, never a value it
supplied: taking it from a header would let any authenticated server read any
room by naming a peer that participates in it.
*/
func (s *Server) handleFederatedEvents(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	if origin == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}

	room, err := s.rooms.FederatedRoom(c.Request().Context(), c.Param("slug"))
	if err != nil {
		/*
			A room that is not federated is reported as absent rather than as
			refused, so that asking about rooms in turn does not reveal which
			ones exist.
		*/
		return echo.NewHTTPError(http.StatusNotFound, "no such federated room")
	}

	since, err := strconv.ParseInt(c.QueryParam("since"), 10, 64)
	if err != nil || since < 0 {
		since = 0
	}

	batch, err := s.rooms.EventsForPeer(c.Request().Context(), room.ID, origin, since, maxEventsPerBatch)
	if err != nil {
		s.log.Error("cannot read events for a peer", "peer", origin, "room", room.ID, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "could not read the room")
	}

	cursor := since
	if len(batch) > 0 {
		cursor = batch[len(batch)-1].Sequence
	}

	return c.JSON(http.StatusOK, map[string]any{
		"room":   s.rooms.RoomAddress(room),
		"events": batch,
		"cursor": cursor,
		// Whether asking again would bring more, so a peer catching up knows
		// to continue rather than guessing from the batch size.
		"more": len(batch) == maxEventsPerBatch,
	})
}

/*
handleFederatedDelivery accepts events a home server has pushed to us.

Everything is verified before anything is stored, and one bad event refuses the
whole batch: storing the valid prefix of a delivery containing a forgery would
leave the room half-updated from something nobody should have trusted.
*/
func (s *Server) handleFederatedDelivery(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	if origin == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}

	var delivery struct {
		Events []events.Event `json:"events"`
	}
	if err := c.Bind(&delivery); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the delivery")
	}
	if len(delivery.Events) == 0 {
		return c.JSON(http.StatusOK, map[string]any{"stored": 0})
	}
	if len(delivery.Events) > maxEventsPerBatch {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "too many events in one delivery")
	}

	room, err := s.rooms.FederatedRoom(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such federated room")
	}

	// The keys of the server that signed the request, which is also the
	// server whose signatures must appear on the events inside it.
	peers, _ := s.federationState()
	document, err := peers.Discover(c.Request().Context(), origin)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "could not fetch the origin's keys")
	}
	keyring, err := document.Keyring()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the origin publishes no usable keys")
	}

	stored, err := s.rooms.AcceptForeignEvents(c.Request().Context(), room, origin, keyring, delivery.Events)
	if err != nil {
		s.log.Warn("refused a federated delivery",
			"peer", origin, "room", room.ID, "events", len(delivery.Events), "error", err)
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	// How many were new, not how many arrived: a redelivery stores nothing,
	// and saying otherwise would imply it did something.
	return c.JSON(http.StatusOK, map[string]any{"stored": stored})
}

/*
handleFederateRoom opens a room to peers, or closes it.

Server-level permission rather than a room moderator's, because the decision is
not about the room's contents but about exposing them beyond this server —
which is the operator's to make, and is audited as such.
*/
func (s *Server) handleFederateRoom(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request struct {
		Federated bool     `json:"federated"`
		Peers     []string `json:"peers"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the request")
	}

	ctx := c.Request().Context()
	if err := s.rooms.Federate(ctx, roomID, request.Federated); err != nil {
		s.log.Error("cannot federate a room", "room", roomID, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "could not change the room")
	}

	for _, peer := range request.Peers {
		if err := s.rooms.AddPeer(ctx, roomID, peer); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	if s.audit != nil {
		if err := s.audit.Record(ctx, audit.Entry{
			ActorID:    &user.ID,
			ActorLabel: user.Username,
			Action:     audit.ActionRoomFederationChanged,
			TargetKind: "room",
			TargetID:   roomID.String(),
			SourceIP:   requestIP(c),
			UserAgent:  c.Request().UserAgent(),
			Detail:     map[string]any{"federated": request.Federated, "peers": request.Peers},
		}); err != nil {
			s.log.Error("cannot record a room federation change", "room", roomID, "error", err)
		}
	}

	return s.describeRoomPeers(c, roomID)
}

// handleRoomPeers reports which servers participate in a room, and how far
// each has been told.
func (s *Server) handleRoomPeers(c *echo.Context) error {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	return s.describeRoomPeers(c, roomID)
}

func (s *Server) describeRoomPeers(c *echo.Context, roomID uuid.UUID) error {
	peers, err := s.rooms.Peers(c.Request().Context(), roomID)
	if err != nil {
		s.log.Error("cannot read a room's peers", "room", roomID, "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "could not read the peers")
	}

	described := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		entry := map[string]any{"peer": peer.Peer, "cursor": peer.Cursor}
		// Only when there is something to say: an empty error field on every
		// healthy peer is noise that hides the one that is failing.
		if peer.LastError != "" {
			entry["lastError"] = peer.LastError
		}
		if peer.DeliveredAt != nil {
			entry["deliveredAt"] = peer.DeliveredAt.UTC()
		}
		described = append(described, entry)
	}

	return c.JSON(http.StatusOK, map[string]any{"peers": described})
}

/*
handleDeliverRoom pushes a room's outstanding events to its peers.

Triggered rather than automatic, in this phase. Delivery on every accepted
event, with retry and backoff, is what Phase 5 builds; doing it now would mean
a background scheduler nobody can yet observe failing. An operator — and the
demonstration — can push explicitly and see exactly what happened.
*/
func (s *Server) handleDeliverRoom(c *echo.Context) error {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	ctx := c.Request().Context()
	if !s.federationEnabled(c) {
		return echo.NewHTTPError(http.StatusConflict, "federation is not enabled on this server")
	}
	if s.config.CanonicalAddress == "" {
		return echo.NewHTTPError(http.StatusConflict, "this server has no canonical address")
	}

	peers, err := s.rooms.Peers(ctx, roomID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not read the peers")
	}

	room, err := s.rooms.ByID(ctx, roomID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	allowPrivate := s.settings != nil &&
		s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true"
	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(),
		s.federationTrust(ctx), allowPrivate)

	slug := ""
	if room.Slug != nil {
		slug = *room.Slug
	}

	results := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		result := map[string]any{"peer": peer.Peer}

		batch, err := s.rooms.EventsForPeer(ctx, roomID, peer.Peer, peer.Cursor, maxEventsPerBatch)
		if err != nil {
			result["error"] = err.Error()
			results = append(results, result)
			continue
		}
		if len(batch) == 0 {
			result["upToDate"] = true
			results = append(results, result)
			continue
		}

		/*
			Reached at the address the peer publishes, not at its bare host.

			A peer is stored as a host because that is its identity — a server
			on :8443 is the same server as on :443 — but an identity is not an
			address, and assuming https on the default port makes every
			deployment behind another port unreachable. The discovery document
			already says where the peer actually is.
		*/
		reachable := peer.Address
		if reachable == "" {
			reachable = "https://" + peer.Peer
		}

		stored, err := sender.DeliverEvents(ctx, reachable, slug, batch)
		if err != nil {
			// The cursor stays where it was, so the same events are tried
			// again rather than skipped.
			if recordErr := s.rooms.RecordPeerFailure(ctx, roomID, peer.Peer, err.Error()); recordErr != nil {
				s.log.Error("cannot record a delivery failure", "peer", peer.Peer, "error", recordErr)
			}
			result["error"] = err.Error()
			results = append(results, result)
			continue
		}

		furthest := batch[len(batch)-1].Sequence
		if err := s.rooms.AdvancePeer(ctx, roomID, peer.Peer, furthest); err != nil {
			s.log.Error("cannot advance a peer's cursor", "peer", peer.Peer, "error", err)
		}

		result["sent"] = len(batch)
		result["stored"] = stored
		result["cursor"] = furthest
		results = append(results, result)
	}

	return c.JSON(http.StatusOK, map[string]any{"peers": results})
}

/*
handleAdoptRoom creates the local replica of a room another server owns.

The counterpart to federating a room: one server opens a room to a peer, and
the peer adopts it. Two deliberate steps rather than one server creating rooms
on another — which would mean a peer could make this server hold whatever it
liked.
*/
func (s *Server) handleAdoptRoom(c *echo.Context) error {
	user, _ := currentUser(c)

	var request struct {
		Slug       string `json:"slug"`
		Name       string `json:"name"`
		HomeServer string `json:"homeServer"`
		Visibility string `json:"visibility"`
		Peer       string `json:"peer"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the request")
	}
	if strings.TrimSpace(request.Slug) == "" || strings.TrimSpace(request.HomeServer) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "a slug and a home server are required")
	}
	if request.Name == "" {
		request.Name = request.Slug
	}

	ctx := c.Request().Context()
	room, err := s.rooms.Adopt(ctx, request.Slug, request.Name, request.HomeServer, request.Visibility)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// The home server is also the peer this replica talks to, unless the
	// operator named a different address to reach it at.
	peer := request.Peer
	if peer == "" {
		peer = request.HomeServer
	}
	if err := s.rooms.AddPeer(ctx, room.ID, peer); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if s.audit != nil {
		if err := s.audit.Record(ctx, audit.Entry{
			ActorID:    &user.ID,
			ActorLabel: user.Username,
			Action:     audit.ActionRoomFederationChanged,
			TargetKind: "room",
			TargetID:   room.ID.String(),
			SourceIP:   requestIP(c),
			UserAgent:  c.Request().UserAgent(),
			Detail:     map[string]any{"adopted": true, "homeServer": room.HomeServer},
		}); err != nil {
			s.log.Error("cannot record a room adoption", "room", room.ID, "error", err)
		}
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"id":         room.ID,
		"slug":       room.Slug,
		"homeServer": room.HomeServer,
	})
}

/*
handleCatchUp asks a room's home server for everything after our cursor.

Pull rather than waiting for the home server's retries: a replica that was
unreachable for a day should ask once for what it missed, not receive a day of
individually retried pushes.
*/
func (s *Server) handleCatchUp(c *echo.Context) error {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	ctx := c.Request().Context()
	if !s.federationEnabled(c) {
		return echo.NewHTTPError(http.StatusConflict, "federation is not enabled on this server")
	}

	room, err := s.rooms.ByID(ctx, roomID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if strings.EqualFold(room.HomeServer, s.federationAuthorityHost()) {
		return echo.NewHTTPError(http.StatusConflict, "this server owns that room; there is nothing to catch up on")
	}

	peers, err := s.rooms.Peers(ctx, roomID)
	if err != nil || len(peers) == 0 {
		return echo.NewHTTPError(http.StatusConflict, "that room has no peer to ask")
	}

	allowPrivate := s.settings != nil &&
		s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true"
	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(),
		s.federationTrust(ctx), allowPrivate)

	home := peers[0]
	slug := ""
	if room.Slug != nil {
		slug = *room.Slug
	}

	held, err := s.rooms.HighestSequence(ctx, roomID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not read the room")
	}

	address := home.Address
	if address == "" {
		address = "https://" + home.Peer
	}

	batch, err := sender.FetchEvents(ctx, address, slug, held)
	if err != nil {
		return c.JSON(http.StatusOK, map[string]any{"fetched": 0, "error": err.Error()})
	}
	if len(batch) == 0 {
		return c.JSON(http.StatusOK, map[string]any{"fetched": 0, "upToDate": true})
	}

	peers2, _ := s.federationState()
	document, err := peers2.Discover(ctx, address)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "could not fetch the home server's keys")
	}
	keyring, err := document.Keyring()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the home server publishes no usable keys")
	}

	stored, err := s.rooms.AcceptForeignEvents(ctx, room, home.Peer, keyring, batch)
	if err != nil {
		s.log.Warn("refused events fetched during catch-up",
			"room", roomID, "peer", home.Peer, "error", err)
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]any{"fetched": len(batch), "stored": stored})
}

// federationAuthorityHost is this server's identity as the trust lists and
// room home servers record it: a host, without the port.
func (s *Server) federationAuthorityHost() string {
	host, err := federation.Host(s.config.CanonicalAddress)
	if err != nil {
		return ""
	}
	return host
}
