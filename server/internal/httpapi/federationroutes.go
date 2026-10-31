package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/moderation"
	"github.com/AnoRebel/tdc/server/internal/rooms"
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

	/*
		This server's own content policy, applied before anything is stored.

		The spec is explicit that a server must not be required to store
		content its policy forbids, and a filter that only ran on locally
		composed messages would leave federation as the way around it.

		Refusal is the only action available here. Masking rewrites the body,
		which would break the signature the event carries — and an event this
		server had altered would fail verification everywhere, including on the
		next server it reached. So a forbidden event is not stored, and the
		peer is told which one; flagging is left to the local moderation queue,
		which sees stored events.
	*/
	if refused := s.refusedByPolicy(c.Request().Context(), delivery.Events); refused != "" {
		s.log.Info("refused a federated event under this server's content policy",
			"peer", origin, "room", room.ID, "event", refused)
		return echo.NewHTTPError(http.StatusUnprocessableEntity,
			"event "+refused+" was refused by this server's content policy")
	}

	stored, err := s.rooms.AcceptForeignEvents(c.Request().Context(), room, origin, keyring, peers, delivery.Events)
	if err != nil {
		s.log.Warn("refused a federated delivery",
			"peer", origin, "room", room.ID, "events", len(delivery.Events), "error", err)
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}
	// Every event verified and is stored: what they say about files follows.
	for _, event := range delivery.Events {
		s.recordFederatedFiles(c.Request().Context(), room, event)
	}
	s.arrived(c.Request().Context(), room, stored)

	// How many were new, not how many arrived: a redelivery stores nothing,
	// and saying otherwise would imply it did something.
	return c.JSON(http.StatusOK, map[string]any{"stored": len(stored)})
}

/*
arrived shows the people here what another server has just told this one: each
newly stored event goes out live, as one written here would, and a project
held here for that server is brought up to what its stream now says.

Only what is new — a redelivery shows nobody anything twice.
*/
func (s *Server) arrived(ctx context.Context, room dbgen.Room, stored []dbgen.Event) {
	for _, event := range stored {
		s.publishEvent(ctx, event)
	}
	// A project held for another server is rebuilt from what arrived.
	if len(stored) > 0 && room.Kind == "project" && s.projects != nil {
		if err := s.projects.Replicate(ctx, room); err != nil {
			s.log.Warn("a project replica could not follow its stream", "project", room.ID, "error", err.Error())
		}
	}
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
		if peer.Failures > 0 {
			entry["failures"] = peer.Failures
		}
		if peer.Abandoned {
			// The one thing an operator most needs to see, because a peer
			// that has been given up on will not recover by itself.
			entry["abandoned"] = true
		}
		if peer.NextAttemptAt != nil {
			entry["nextAttemptAt"] = peer.NextAttemptAt.UTC()
		}
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
handleDeliverRoom pushes a room's outstanding events to its peers, now.

The server delivers by itself (see deliverFederationPeriodically); this is for
an operator who wants to see exactly what happens with each peer, and it
reports that peer by peer.
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

	results := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		if peer.Abandoned {
			// Given up on. An operator resumes it deliberately; until then it
			// costs nothing, which is the point of abandoning it.
			results = append(results, map[string]any{
				"peer": peer.Peer, "abandoned": true, "lastError": peer.LastError,
			})
			continue
		}
		results = append(results, s.deliverToPeer(ctx, sender, room, peer))
	}

	return c.JSON(http.StatusOK, map[string]any{"peers": results})
}

/*
deliverToPeer sends a peer the next batch of a room's events it has not had, and
records what happened: the cursor advanced on success, the failure counted and
the next attempt pushed back on failure, the peer abandoned past the ceiling.
Returns what happened, for an operator who asked.
*/
func (s *Server) deliverToPeer(
	ctx context.Context,
	sender *federation.Sender,
	room dbgen.Room,
	peer dbgen.RoomPeer,
) map[string]any {
	result := map[string]any{"peer": peer.Peer}

	batch, err := s.rooms.EventsForPeer(ctx, room.ID, peer.Peer, peer.Cursor, maxEventsPerBatch)
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	if len(batch) == 0 {
		result["upToDate"] = true
		return result
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

	stored, err := sender.DeliverEvents(ctx, reachable, rooms.LocalPart(room), batch)
	if err != nil {
		/*
			The cursor stays where it was, so the same events are tried
			again rather than skipped, and the failure is counted so the
			next attempt waits longer than this one did.
		*/
		state, recordErr := s.rooms.DeliveryFailed(ctx, room.ID, peer.Peer, err.Error(), int(peer.Failures))
		if recordErr != nil {
			s.log.Error("cannot record a delivery failure", "peer", peer.Peer, "error", recordErr)
		}

		result["error"] = err.Error()
		result["failures"] = state.Failures
		if state.Abandoned {
			// Past the ceiling: nothing further is attempted until an
			// operator says so, and they are told rather than left to
			// notice a room quietly stopped replicating.
			result["abandoned"] = true
			s.log.Warn("abandoned a federation peer after repeated failures",
				"peer", peer.Peer, "room", room.ID,
				"failures", state.Failures, "error", err)
		} else if state.NextAttemptAt != nil {
			result["nextAttemptAt"] = state.NextAttemptAt.UTC()
		}

		return result
	}

	furthest := batch[len(batch)-1].Sequence
	if err := s.rooms.AdvancePeer(ctx, room.ID, peer.Peer, furthest); err != nil {
		s.log.Error("cannot advance a peer's cursor", "peer", peer.Peer, "error", err)
	}

	if err := s.rooms.DeliverySucceeded(ctx, room.ID, peer.Peer); err != nil {
		s.log.Error("cannot clear a peer's failures", "peer", peer.Peer, "error", err)
	}

	result["sent"] = len(batch)
	result["stored"] = stored
	result["cursor"] = furthest
	return result
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
	if s.rooms.HostedHere(room) {
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
	slug := rooms.LocalPart(room)

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

	stored, err := s.rooms.AcceptForeignEvents(ctx, room, home.Peer, keyring, peers2, batch)
	if err != nil {
		s.log.Warn("refused events fetched during catch-up",
			"room", roomID, "peer", home.Peer, "error", err)
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}
	s.arrived(ctx, room, stored)

	return c.JSON(http.StatusOK, map[string]any{"fetched": len(batch), "stored": len(stored)})
}

/*
handleFederatedJoin admits somebody from a peer into a room this server owns.

The member is derived from the verified origin of the signed request: a peer
may ask to join on behalf of its own users and nobody else's, so the address
must name the server that signed. Without that check any server could join any
address it liked to any room, which is impersonation with extra steps.
*/
func (s *Server) handleFederatedJoin(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	if origin == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}

	var request struct {
		Member string `json:"member"`
	}
	if err := c.Bind(&request); err != nil || strings.TrimSpace(request.Member) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name the member joining")
	}

	if err := s.peerSpeaksFor(origin, request.Member); err != nil {
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}

	ctx := c.Request().Context()
	room, err := s.rooms.FederatedRoom(ctx, c.Param("slug"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such federated room")
	}

	if err := s.rooms.JoinRemote(ctx, room, request.Member); err != nil {
		return federationRefusal(err)
	}

	// The joining server becomes a participant, so what it just joined starts
	// reaching it. A member of a room whose server receives nothing about it
	// would have joined a conversation they cannot see.
	host, hostErr := federation.Host(origin)
	if hostErr == nil {
		if err := s.rooms.AddPeer(ctx, room.ID, origin); err != nil {
			s.log.Error("cannot add a joining peer", "peer", host, "room", room.ID, "error", err)
		}
	}

	return c.JSON(http.StatusOK, map[string]any{"joined": true, "member": request.Member})
}

// handleFederatedLeave removes somebody on a peer from a room this server owns.
func (s *Server) handleFederatedLeave(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	if origin == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}

	var request struct {
		Member string `json:"member"`
	}
	if err := c.Bind(&request); err != nil || strings.TrimSpace(request.Member) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name the member leaving")
	}
	if err := s.peerSpeaksFor(origin, request.Member); err != nil {
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}

	ctx := c.Request().Context()
	room, err := s.rooms.FederatedRoom(ctx, c.Param("slug"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such federated room")
	}
	if err := s.rooms.LeaveRemote(ctx, room, request.Member); err != nil {
		return federationRefusal(err)
	}

	return c.JSON(http.StatusOK, map[string]any{"left": true, "member": request.Member})
}

/*
handleFederatedPost accepts a message from a member on a peer.

This is where a remote message gets its position in the room's order. The
peer's own server does not sequence anything — it forwards, and this server
answers with the event it created, including the sequence it assigned.
*/
func (s *Server) handleFederatedPost(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	if origin == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}

	var request struct {
		Event events.Event `json:"event"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the event")
	}
	if strings.TrimSpace(request.Event.Sender) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "the event names no sender")
	}
	if err := s.peerSpeaksFor(origin, request.Event.Sender); err != nil {
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}

	ctx := c.Request().Context()
	room, err := s.rooms.FederatedRoom(ctx, c.Param("slug"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such federated room")
	}

	/*
		Verified against the sender's server's own published keys.

		The request signature proved which server is asking; this proves the
		event inside was really made by them, which is what a third server
		will check later when this one relays it on.
	*/
	peers, _ := s.federationState()
	document, err := peers.Discover(ctx, origin)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "could not fetch the origin's keys")
	}
	keyring, err := document.Keyring()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the origin publishes no usable keys")
	}

	stored, err := s.rooms.PostRemote(ctx, room, keyring, request.Event)
	if err != nil {
		return federationRefusal(err)
	}
	// Its files, described in what its author's server signed, are recorded
	// here to be copied from that server when somebody opens one.
	s.recordFederatedFiles(ctx, room, request.Event)

	return c.JSON(http.StatusCreated, map[string]any{
		"id":       stored.ID,
		"sequence": stored.Sequence,
		"sender":   stored.SenderAddress,
	})
}

/*
peerSpeaksFor refuses a peer acting for somebody it does not host.

A server may say anything about its own users and nothing about anybody
else's. The request signature proves which server is asking; this is what makes
that proof mean something about the person named inside.
*/
func (s *Server) peerSpeaksFor(origin, address string) error {
	peer, err := federation.Host(origin)
	if err != nil {
		return err
	}
	claimed, err := federation.ServerOf(address)
	if err != nil {
		return err
	}
	if !strings.EqualFold(peer, claimed) {
		return fmt.Errorf("%s may not act for %s", peer, address)
	}
	return nil
}

/*
federationRefusal turns a room service error into the right status.

A room the caller may not see is reported as missing rather than forbidden, so
that a peer cannot learn which rooms exist, or which are private, by being
refused differently.
*/
func federationRefusal(err error) error {
	switch {
	case errors.Is(err, rooms.ErrRoomNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such federated room")
	case errors.Is(err, rooms.ErrArchived):
		return echo.NewHTTPError(http.StatusConflict, "that room is archived")
	case errors.Is(err, rooms.ErrNotAMember):
		return echo.NewHTTPError(http.StatusForbidden, "not a member of that room")
	case errors.Is(err, rooms.ErrNotHome):
		return echo.NewHTTPError(http.StatusConflict, "this server does not own that room")
	default:
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}
}

/*
handleJoinRemoteRoom joins one of this server's people to a room elsewhere.

The person asks their own server, which asks the room's home server, which
decides. Three steps rather than the person's client talking to a server it has
no account on — which would mean every client holding credentials for every
server hosting a room it wanted to read.
*/
func (s *Server) handleJoinRemoteRoom(c *echo.Context) error {
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	ctx := c.Request().Context()
	room, sender, home, err := s.remoteRoomContext(ctx, c, roomID)
	if err != nil {
		return err
	}

	member := s.rooms.UserAddress(user.Username)
	slug := ""
	if room.Slug != nil {
		slug = *room.Slug
	}

	if err := sender.JoinRoom(ctx, home, slug, member); err != nil {
		s.log.Warn("a remote join was refused", "room", roomID, "member", member, "error", err)
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}

	/*
		Joined there, so joined here too.

		The local membership is what makes the room appear in this person's
		list and what the ordinary read path checks. It is a cache of the home
		server's decision, not a second source of truth — the home server
		refusing later is what counts.
	*/
	if err := s.rooms.Join(ctx, roomID, user.ID); err != nil {
		s.log.Error("joined remotely but not locally", "room", roomID, "error", err)
	}

	return c.JSON(http.StatusOK, map[string]any{"joined": true, "member": member})
}

/*
handleSendToRemoteRoom posts one of this server's people into a room elsewhere.

Nothing is stored here on the way out. The home server sequences the message
and replicates it back through the ordinary delivery path, so the message a
person sees in the room is the one everybody else sees — rather than a local
copy that might differ from what was actually accepted.
*/
func (s *Server) handleSendToRemoteRoom(c *echo.Context) error {
	user, ok := currentUser(c)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request struct {
		Body          string   `json:"body"`
		AttachmentIDs []string `json:"attachmentIds,omitempty"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	attachmentIDs, err := parseAttachmentIDs(request.AttachmentIDs)
	if err != nil {
		return err
	}
	// Files alone are a message, here as anywhere.
	if strings.TrimSpace(request.Body) == "" && len(attachmentIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, rooms.ErrEmptyMessage.Error())
	}

	ctx := c.Request().Context()
	room, sender, home, err := s.remoteRoomContext(ctx, c, roomID)
	if err != nil {
		return err
	}

	// Uploaded here, where the person has an account, and described in the
	// message so the home server and everybody else copy them from here.
	var files []rooms.FileRef
	if len(attachmentIDs) > 0 {
		if err := s.attachments.CheckBatch(ctx, attachmentIDs); err != nil {
			return attachmentError(err)
		}
		if files, err = s.describeUploads(ctx, user.ID, roomID, attachmentIDs); err != nil {
			return attachmentError(err)
		}
	}

	slug := ""
	if room.Slug != nil {
		slug = *room.Slug
	}

	payload := map[string]any{"body": strings.TrimSpace(request.Body)}
	if len(files) > 0 {
		payload["attachments"] = files
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not encode the message")
	}

	posted, err := sender.PostMessage(ctx, home, slug, s.rooms.RoomAddress(room),
		s.rooms.UserAddress(user.Username), content)
	if err != nil {
		s.log.Warn("a remote post was refused", "room", roomID, "error", err)
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"id":       posted.ID,
		"sequence": posted.Sequence,
		"sender":   posted.Sender,
	})
}

/*
remoteRoomContext gathers what a call to a room's home server needs.

One place, because every one of these calls needs the same four things and
would otherwise repeat the same five refusals — each of which is a chance for
one path to refuse differently from another.
*/
func (s *Server) remoteRoomContext(
	ctx context.Context,
	c *echo.Context,
	roomID uuid.UUID,
) (dbgen.Room, *federation.Sender, string, error) {
	if !s.federationEnabled(c) {
		return dbgen.Room{}, nil, "", echo.NewHTTPError(http.StatusConflict,
			"federation is not enabled on this server")
	}

	room, err := s.rooms.ByID(ctx, roomID)
	if err != nil {
		return dbgen.Room{}, nil, "", echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if s.rooms.HostedHere(room) {
		return dbgen.Room{}, nil, "", echo.NewHTTPError(http.StatusConflict,
			"this server owns that room; use the ordinary path")
	}

	peers, err := s.rooms.Peers(ctx, roomID)
	if err != nil || len(peers) == 0 {
		return dbgen.Room{}, nil, "", echo.NewHTTPError(http.StatusConflict,
			"that room has no home server to ask")
	}

	home := peers[0].Address
	if home == "" {
		home = "https://" + peers[0].Peer
	}

	allowPrivate := s.settings != nil &&
		s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true"

	return room, federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(),
		s.federationTrust(ctx), allowPrivate), home, nil
}

// handleResumePeer tries an abandoned peer again, at an operator's word.
func (s *Server) handleResumePeer(c *echo.Context) error {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request struct {
		Peer string `json:"peer"`
	}
	if err := c.Bind(&request); err != nil || strings.TrimSpace(request.Peer) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name the peer to resume")
	}

	host, err := federation.Host(request.Peer)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := s.rooms.ResumePeer(c.Request().Context(), roomID, host); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not resume the peer")
	}

	return s.describeRoomPeers(c, roomID)
}

// maxPublishedRooms bounds a browse response, so listing a large server is a
// page rather than everything it holds.
const maxPublishedRooms = 100

/*
handleFederatedRoomList serves this server's public rooms to a peer.

Two checks in series, neither substituting for the other. First whether this
server is discoverable by this peer at all — mayDiscover, reading the identity
the signature established rather than a header. Then which rooms are public,
which the query decides.

A limited server that has not listed this peer answers with an empty list
rather than a refusal: the peer learns nothing about what is here, which is
what limited visibility means, and a distinct refusal would itself disclose
that something exists to be refused.
*/
func (s *Server) handleFederatedRoomList(c *echo.Context) error {
	if !s.mayDiscover(c) {
		return c.JSON(http.StatusOK, map[string]any{"rooms": []any{}})
	}

	published, err := s.rooms.Published(c.Request().Context(), maxPublishedRooms)
	if err != nil {
		s.log.Error("cannot list published rooms", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "could not list the rooms")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"server": s.config.CanonicalAddress,
		"rooms":  published,
	})
}

// connectedServer is a peer as a person browsing this server sees it.
//
// The shape the clients were written against before there was a server to
// answer them: a domain that identifies it, what it calls itself, and whether
// it can currently be reached.
type connectedServer struct {
	Domain string `json:"domain"`
	Name   string `json:"name"`
	// Description is what the peer says it is for, when it publishes one.
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	/*
		Reachable is false when delivery to this peer has been abandoned.

		A listed but unreachable server is still shown rather than omitted, so
		that its absence from a conversation is visible instead of silent —
		somebody wondering why a room has gone quiet deserves to see that the
		server holding it has been unreachable for an hour.
	*/
	Reachable bool `json:"reachable"`
	// PublishedRoomCount is how many rooms this person may browse there,
	// absent when it publishes none or could not be asked.
	PublishedRoomCount *int `json:"publishedRoomCount,omitempty"`
}

/*
handleConnectedServers tells a person which servers this one is connected to.

A session rather than an administrative permission: which servers a community
federates with is not privileged from its own members. It is the shape of the
network they are part of, and hiding it would mean people cannot tell where the
rooms they are in actually live.
*/
func (s *Server) handleConnectedServers(c *echo.Context) error {
	peers, err := s.rooms.KnownPeers(c.Request().Context())
	if err != nil {
		s.log.Error("cannot list peers", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "could not list the connected servers")
	}

	// A bare array: what the clients ask for, and what a list of things
	// ordinarily is.
	described := make([]connectedServer, 0, len(peers))
	for _, peer := range peers {
		described = append(described, connectedServer{
			Domain: peer.Server,
			// Its own name is not known without asking it, which a list of
			// twenty servers should not do twenty times. The domain is what
			// identifies it, and the detail view fills in the rest.
			Name:       peer.Server,
			Visibility: string(VisibilityPublic),
			Reachable:  peer.Reachable,
		})
	}

	return c.JSON(http.StatusOK, described)
}

/*
handleConnectedServer describes one peer, asking it about itself.

Unlike the list, this is one server and one round trip, so it can afford to
fetch what the peer publishes about itself rather than showing only what this
server already knew.
*/
func (s *Server) handleConnectedServer(c *echo.Context) error {
	domain := strings.TrimSpace(c.Param("domain"))
	if domain == "" {
		return echo.NewHTTPError(http.StatusNotFound, "no such server")
	}

	ctx := c.Request().Context()
	known, err := s.knownPeer(ctx, domain)
	if err != nil {
		return err
	}

	described := connectedServer{
		Domain:     known.Server,
		Name:       known.Server,
		Visibility: string(VisibilityPublic),
		Reachable:  known.Reachable,
	}

	// What it publishes about itself, when it can be reached. A peer that
	// cannot be asked is still described, from what we already hold.
	if peers, _ := s.federationState(); peers != nil {
		if document, docErr := peers.Discover(ctx, known.Address); docErr == nil {
			described.Domain = known.Server
			if !document.Enabled {
				described.Reachable = false
			}
		}
	}

	return c.JSON(http.StatusOK, described)
}

// knownPeer finds a peer this server participates with, by host.
func (s *Server) knownPeer(ctx context.Context, domain string) (rooms.Peer, error) {
	host, err := federation.Host(domain)
	if err != nil {
		return rooms.Peer{}, echo.NewHTTPError(http.StatusNotFound, "no such server")
	}

	peers, err := s.rooms.KnownPeers(ctx)
	if err != nil {
		return rooms.Peer{}, echo.NewHTTPError(http.StatusInternalServerError,
			"could not read the connected servers")
	}

	for _, peer := range peers {
		if strings.EqualFold(peer.Server, host) {
			return peer, nil
		}
	}
	return rooms.Peer{}, echo.NewHTTPError(http.StatusNotFound, "no such server")
}

/*
handleBrowsePeer lists a peer's public rooms, asked on a person's behalf.

This server does the asking, because the person's browser holds no credential
the peer would recognise and the peer decides what to show based on which
server is asking. So the request is signed by this server, and what comes back
is what the peer chose to publish to us — not to them.
*/
func (s *Server) handleBrowsePeer(c *echo.Context) error {
	peer := strings.TrimSpace(c.Param("domain"))
	if peer == "" {
		return echo.NewHTTPError(http.StatusNotFound, "no such server")
	}

	ctx := c.Request().Context()
	if !s.federationEnabled(c) {
		return echo.NewHTTPError(http.StatusConflict, "federation is not enabled on this server")
	}
	if s.config.CanonicalAddress == "" {
		return echo.NewHTTPError(http.StatusConflict, "this server has no canonical address")
	}

	/*
		Reached at the address we know for it where we have one.

		A peer is named by host, which is its identity rather than its
		address — assuming https on the default port would make every peer
		behind another port unbrowsable.
	*/
	address := peer
	if known, err := s.knownPeer(ctx, peer); err == nil && known.Address != "" {
		address = known.Address
	}

	allowPrivate := s.settings != nil &&
		s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true"
	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(),
		s.federationTrust(ctx), allowPrivate)

	published, err := sender.BrowseRooms(ctx, address)
	if err != nil {
		/*
			An empty list rather than an error.

			A peer that does not list us is indistinguishable from one that
			publishes nothing, deliberately — and a peer that is simply down
			should leave the page saying "nothing to show" rather than
			throwing, since the person browsing can do nothing about either.
		*/
		s.log.Warn("could not browse a peer", "peer", peer, "error", err)
		return c.JSON(http.StatusOK, []any{})
	}

	// Described the way a room is described everywhere else, so the client
	// renders them with the components it already has.
	described := make([]map[string]any, 0, len(published))
	for _, room := range published {
		described = append(described, map[string]any{
			"id":         room.Address,
			"slug":       room.Slug,
			"name":       room.Name,
			"topic":      room.Topic,
			"kind":       "channel",
			"visibility": "public",
			"address":    room.Address,
			"archived":   false,
			"member":     false,
		})
	}

	return c.JSON(http.StatusOK, described)
}

// PublishedRoomsForTest exposes what this server publishes to peers, so a test
// can check the filtering against a real database rather than against a mock
// peer that was never given a private room to hide.
func (s *Server) PublishedRoomsForTest(ctx context.Context) ([]rooms.PublishedRoom, error) {
	return s.rooms.Published(ctx, maxPublishedRooms)
}

// MayDiscoverForTest answers the visibility question for a given verified peer,
// so a test can check the rule without standing up a second signing server.
func (s *Server) MayDiscoverForTest(ctx context.Context, verifiedPeer string) bool {
	if s.visibility(ctx) != VisibilityLimited {
		return s.visibility(ctx) == VisibilityPublic
	}
	if strings.TrimSpace(verifiedPeer) == "" {
		return false
	}
	return s.listsPeer(ctx, verifiedPeer)
}

// EventsForPeerTest exposes what would be delivered to a peer, so a test can
// inspect every field of it rather than trust that nothing was added.
func (s *Server) EventsForPeerTest(ctx context.Context, roomID, peer string) ([]events.Event, error) {
	id, err := uuid.Parse(roomID)
	if err != nil {
		return nil, err
	}
	return s.rooms.EventsForPeer(ctx, id, peer, 0, maxEventsPerBatch)
}

/*
refusedByPolicy names the first event this server's filter forbids, if any.

Only the blocking action applies to arriving events: masking would rewrite a
body the signature covers, leaving an event that verifies nowhere — including
on the next server this one relays it to. Flagging is for events that are
stored, which these are not.

Returns the identifier rather than a boolean so the peer can be told which of
its batch was the problem, since a batch refused as a whole is otherwise a
thing it cannot act on.
*/
func (s *Server) refusedByPolicy(ctx context.Context, batch []events.Event) string {
	if s.settings == nil {
		return ""
	}
	if moderation.Action(s.settings.Get(ctx, settings.FilterAction)) != moderation.ActionBlock {
		return ""
	}

	words := splitList(s.settings.Get(ctx, settings.FilterWords))
	if len(words) == 0 {
		return ""
	}
	filter := moderation.NewFilter(words)

	for _, event := range batch {
		var content struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(event.Content, &content); err != nil {
			// Content this server cannot read is content it cannot filter.
			// Verification has already accepted it, and refusing what we
			// simply do not understand would refuse every future event type.
			continue
		}
		if content.Body == "" {
			continue
		}
		if _, matched := filter.Check(content.Body); matched {
			return event.ID
		}
	}
	return ""
}

// RefusedByPolicyForTest exposes the content-policy decision for arriving
// events, so a test can check it without standing up a signing peer.
func (s *Server) RefusedByPolicyForTest(ctx context.Context, batch []events.Event) string {
	return s.refusedByPolicy(ctx, batch)
}
