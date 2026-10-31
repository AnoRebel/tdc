package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Calls between people on different servers.

A call is held by one server, its home: the server of whoever rang, or the home
server of a room shared between servers. The home keeps the call — who is in
it, what each is sending — and relays negotiation, as it does for its own
people. Every other server is a go-between for its own people: it authorises
them, carries their negotiation to the home over signed requests, and passes
on what the home sends them, accepting it only from the server it recorded as
that call's home for that person.

Somebody from another server is in a call here as a row standing for them —
their address, the server that vouched for them, and the identifier their own
server knows them by — so the call's machinery needs nothing new to hold them,
and their own client recognises itself in the call's state. Media never passes
through any of this: it goes between the people, or to the home's forwarding
unit, and each person's client gets its relay credentials from its own server.

Both servers must have calls.federated on. It is checked before anybody's own
setting, so an operator's no is final; and a server with it off neither sends
nor answers anything here.
*/

func init() {
	registerRoutes(func(s *Server) {
		if s.rooms == nil {
			return
		}
		calls := s.echo.Group("/api/federation/calls", s.requireFederation, s.requireSignature, s.requireFederatedCalls)
		// From a call's home, to the server of somebody in it or rung into it.
		calls.POST("/ring", s.handleFederatedRing)
		calls.POST("/deliver", s.handleFederatedDeliver)
		// From somebody's own server, to the call's home.
		calls.POST("/join", s.handleFederatedCallJoin)
		calls.POST("/view", s.handleFederatedCallView)
		calls.POST("/media", s.handleFederatedCallMedia)
		calls.POST("/leave", s.handleFederatedCallLeave)
		calls.POST("/present", s.handleFederatedCallPresent)
		calls.POST("/signal", s.handleFederatedCallSignal)
	})

	documentRoutes(map[string]entry{
		"POST /api/federation/calls/ring":    {Summary: "A peer rings somebody here into a call it holds", Authentication: "federation signature"},
		"POST /api/federation/calls/deliver": {Summary: "A peer passes on what its call sends somebody here", Authentication: "federation signature"},
		"POST /api/federation/calls/join":    {Summary: "A peer's user joins a call held here", Authentication: "federation signature"},
		"POST /api/federation/calls/view":    {Summary: "A peer's user reads the call in a shared room", Authentication: "federation signature"},
		"POST /api/federation/calls/media":   {Summary: "A peer's user says what they send in a call", Authentication: "federation signature"},
		"POST /api/federation/calls/leave":   {Summary: "A peer's user leaves a call held here", Authentication: "federation signature"},
		"POST /api/federation/calls/present": {Summary: "A peer says its user is still in a call", Authentication: "federation signature"},
		"POST /api/federation/calls/signal":  {Summary: "A peer's user passes on call negotiation", Authentication: "federation signature"},
	})
}

// federatedCallRequest is the body of every request of the call family; each
// uses the fields it needs.
type federatedCallRequest struct {
	// action is which request of the family this is; not sent.
	action string
	Call   string `json:"call"`
	RoomID string `json:"roomId"`
	// Room is a shared room's slug on its home server.
	Room        string `json:"room"`
	Member      string `json:"member"`
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Audio       bool   `json:"audio"`
	Video       bool   `json:"video"`
	Held        bool   `json:"held"`
	// To is a participant in the call (signal), or a person rung (ring) or
	// sent to (deliver), by address.
	To     string          `json:"to"`
	Signal json.RawMessage `json:"signal"`
	From   struct {
		Address     string `json:"address"`
		DisplayName string `json:"displayName"`
	} `json:"from"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// The frames a call's home may pass on to somebody here, and nothing else.
var deliverable = map[string]bool{
	realtime.FrameCallState: true, realtime.FrameCallSignal: true, realtime.FrameCallRingEnded: true,
	realtime.FrameCallDeclined: true, realtime.FrameCallReaction: true,
}

// callsFederated is whether calls cross servers here at all.
func (s *Server) callsFederated(ctx context.Context) bool {
	return s.settings != nil &&
		s.settings.Get(ctx, settings.FederationEnabled) == "true" &&
		s.settings.Enabled(ctx, settings.FeatureCalls) &&
		s.settings.Get(ctx, settings.CallsFederated) == "true"
}

// requireFederatedCalls refuses the call family while calls do not cross
// servers here, before any handler runs.
func (s *Server) requireFederatedCalls(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !s.callsFederated(c.Request().Context()) {
			return echo.NewHTTPError(http.StatusNotFound, "this server does not take calls from other servers")
		}
		return next(c)
	}
}

func (s *Server) callSender(ctx context.Context) *federation.Sender {
	allowPrivate := s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true"
	return federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(), s.federationTrust(ctx), allowPrivate)
}

// verifiedOrigin is the signing server, as this server established it.
func verifiedOrigin(c *echo.Context) (string, string) {
	address, _ := c.Get(peerOrigin).(string)
	host, _ := federation.Host(address)
	return address, host
}

func readCallRequest(c *echo.Context) (federatedCallRequest, error) {
	var request federatedCallRequest
	if err := c.Bind(&request); err != nil {
		return request, echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	return request, nil
}

// isLocalAddress is whether an address names somebody on this server.
func (s *Server) isLocalAddress(address string) bool {
	server, err := federation.ServerOf(address)
	return err == nil && strings.EqualFold(server, s.rooms.ServerName())
}

/*
peerAddressFor is where to reach the server an address names: the address a
room's peers record for it; else an allowlist entry for it written as a full
address, scheme and port and all; else its host over HTTPS.
*/
func (s *Server) peerAddressFor(ctx context.Context, host string) string {
	if peers, err := s.rooms.KnownPeers(ctx); err == nil {
		for _, peer := range peers {
			if strings.EqualFold(peer.Server, host) && peer.Address != "" {
				return peer.Address
			}
		}
	}
	for _, entry := range s.federationTrust(ctx).Allowed {
		if listed, err := federation.Host(entry); err == nil && strings.EqualFold(listed, host) && strings.Contains(entry, "://") {
			return strings.TrimRight(entry, "/")
		}
	}
	return "https://" + host
}

// The home's half.

/*
remoteParticipant is the row standing for somebody from another server in a
call here, made or brought up to date. Their identifier is their own server's,
refused if it is already somebody else's.
*/
func (s *Server) remoteParticipant(ctx context.Context, request federatedCallRequest, origin string, callID uuid.UUID) (dbgen.User, error) {
	id, err := uuid.Parse(request.ID)
	if err != nil || id == uuid.Nil {
		return dbgen.User{}, echo.NewHTTPError(http.StatusBadRequest, "a member needs the identifier their server knows them by")
	}
	name := strings.TrimSpace(request.DisplayName)
	if identity.ValidateDisplayName(name) != nil {
		name, _, _ = strings.Cut(request.Member, "@")
	}
	if existing, err := s.queries.GetUserByID(ctx, id); err == nil {
		if existing.RemoteAddress == nil || *existing.RemoteAddress != request.Member {
			return dbgen.User{}, echo.NewHTTPError(http.StatusConflict, "that identifier is somebody else's")
		}
		if err := s.queries.UpdateRemoteShadow(ctx, dbgen.UpdateRemoteShadowParams{
			ID: id, DisplayName: name, GuestCall: &callID, RemoteServer: &origin,
		}); err != nil {
			return dbgen.User{}, err
		}
		existing.DisplayName, existing.GuestCall, existing.RemoteServer = name, &callID, &origin
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, err
	}
	if _, err := s.queries.UserByRemoteAddress(ctx, &request.Member); err == nil {
		return dbgen.User{}, echo.NewHTTPError(http.StatusConflict, "that person is known here by another identifier")
	}
	username := "remote-" + hex.EncodeToString(id[:6])
	return s.queries.CreateRemoteShadow(ctx, dbgen.CreateRemoteShadowParams{
		ID: id, Username: username, UsernameFolded: identity.NormaliseUsername(username), DisplayName: name,
		GuestCall: &callID, RemoteAddress: &request.Member, RemoteServer: &origin,
	})
}

// memberRequest reads a request naming one of the signing server's people.
func (s *Server) memberRequest(c *echo.Context) (federatedCallRequest, string, error) {
	request, err := readCallRequest(c)
	if err != nil {
		return request, "", err
	}
	origin, _ := verifiedOrigin(c)
	if err := s.peerSpeaksFor(origin, request.Member); err != nil {
		return request, "", echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	return request, origin, nil
}

// remoteInCall finds somebody from another server who is in a call here.
func (s *Server) remoteInCall(ctx context.Context, request federatedCallRequest) (dbgen.User, rooms.Call, error) {
	callID, err := uuid.Parse(request.Call)
	if err != nil {
		return dbgen.User{}, rooms.Call{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	shadow, err := s.queries.UserByRemoteAddress(ctx, &request.Member)
	if err != nil {
		return dbgen.User{}, rooms.Call{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	roomID, in := s.rooms.CallRoomOf(ctx, callID, shadow.ID)
	if !in {
		return dbgen.User{}, rooms.Call{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	call, err := s.rooms.OpenCallIn(ctx, roomID)
	if err != nil || call.ID != callID {
		return dbgen.User{}, rooms.Call{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	return shadow, call, nil
}

// sharedRoomFor finds a shared room here that somebody from another server is
// a member of.
func (s *Server) sharedRoomFor(ctx context.Context, slug, member string) (dbgen.Room, error) {
	room, err := s.rooms.FederatedRoom(ctx, slug)
	if err != nil || !s.rooms.HostedHere(room) {
		return dbgen.Room{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	member2, err := s.queries.IsRemoteMember(ctx, dbgen.IsRemoteMemberParams{RoomID: room.ID, Address: member})
	if err != nil || !member2 {
		return dbgen.Room{}, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	return room, nil
}

/*
handleFederatedCallJoin puts somebody from another server in a call here: one
they were rung into, or the call in a room shared with their server, which
they may start. This server's own rules apply — the call is open, the limit,
the room's membership — and nothing of the other server's.
*/
func (s *Server) handleFederatedCallJoin(c *echo.Context) error {
	ctx := c.Request().Context()
	request, origin, err := s.memberRequest(c)
	if err != nil {
		return err
	}
	limit, full := s.callLimit(ctx)

	var joined rooms.Call
	if request.Room != "" {
		room, err := s.sharedRoomFor(ctx, request.Room, request.Member)
		if err != nil {
			return err
		}
		current, err := s.rooms.OpenCallIn(ctx, room.ID)
		callID := current.ID
		if errors.Is(err, rooms.ErrNoCall) {
			callID = uuid.Nil
		} else if err != nil {
			return callError(err)
		}
		shadow, err := s.remoteParticipant(ctx, request, origin, callID)
		if err != nil {
			return err
		}
		call, started, err := s.rooms.JoinCallRemote(ctx, room, shadow.ID, request.Member, request.Audio, request.Video, limit)
		if err != nil {
			return joinError(err, full)
		}
		if callID == uuid.Nil {
			// Started just now: the row standing for them names the call.
			_ = s.queries.UpdateRemoteShadow(ctx, dbgen.UpdateRemoteShadowParams{
				ID: shadow.ID, DisplayName: shadow.DisplayName, GuestCall: &call.ID, RemoteServer: &origin,
			})
		}
		if started != nil {
			s.publishEvent(ctx, *started)
		}
		joined = call
	} else {
		callID, err := uuid.Parse(request.Call)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "there is no call here")
		}
		invited, err := s.queries.IsRemoteInvitedToCall(ctx, dbgen.IsRemoteInvitedToCallParams{CallID: callID, Address: request.Member})
		if err != nil || !invited {
			return echo.NewHTTPError(http.StatusNotFound, "there is no call here")
		}
		record, err := s.rooms.CallRecord(ctx, callID)
		if err != nil || record.EndedAt != nil {
			return echo.NewHTTPError(http.StatusNotFound, "there is no call here")
		}
		shadow, err := s.remoteParticipant(ctx, request, origin, callID)
		if err != nil {
			return err
		}
		if err := s.queries.InviteToCall(ctx, dbgen.InviteToCallParams{CallID: callID, UserID: shadow.ID}); err != nil {
			return err
		}
		if joined, err = s.rooms.JoinInvited(ctx, callID, shadow.ID, request.Audio, request.Video, limit); err != nil {
			return joinError(err, full)
		}
	}
	id, _ := uuid.Parse(request.ID)
	_, _ = s.queries.TouchRemoteParticipant(ctx, dbgen.TouchRemoteParticipantParams{CallID: joined.ID, UserID: id})
	joined = s.forwardIfLarge(ctx, joined)
	view := s.callViewOf(joined.Call, joined.Participants)
	s.broadcastCallState(ctx, view)
	return c.JSON(http.StatusOK, view)
}

// handleFederatedCallView shows somebody from another server the call they
// are in, or the call in a room shared with theirs, if there is one.
func (s *Server) handleFederatedCallView(c *echo.Context) error {
	ctx := c.Request().Context()
	request, _, err := s.memberRequest(c)
	if err != nil {
		return err
	}
	if request.Room == "" {
		_, call, err := s.remoteInCall(ctx, request)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, s.callViewOf(call.Call, call.Participants))
	}
	room, err := s.sharedRoomFor(ctx, request.Room, request.Member)
	if err != nil {
		return err
	}
	call, err := s.rooms.OpenCallIn(ctx, room.ID)
	if err != nil {
		return callError(err)
	}
	return c.JSON(http.StatusOK, s.callViewOf(call.Call, call.Participants))
}

func (s *Server) handleFederatedCallMedia(c *echo.Context) error {
	ctx := c.Request().Context()
	request, _, err := s.memberRequest(c)
	if err != nil {
		return err
	}
	shadow, call, err := s.remoteInCall(ctx, request)
	if err != nil {
		return err
	}
	if err := s.rooms.SetCallMedia(ctx, call.ID, shadow.ID, request.Audio, request.Video, request.Held); err != nil {
		return callError(err)
	}
	return s.respondWithCallState(c, call.RoomID, call)
}

func (s *Server) handleFederatedCallLeave(c *echo.Context) error {
	ctx := context.WithoutCancel(c.Request().Context())
	request, _, err := s.memberRequest(c)
	if err != nil {
		return err
	}
	shadow, call, err := s.remoteInCall(ctx, request)
	if err != nil {
		return err
	}
	ended, err := s.rooms.LeaveCall(ctx, call.ID, shadow.ID)
	if err != nil {
		return callError(err)
	}
	if ended != nil {
		s.publishEvent(ctx, *ended)
		s.dismissRemaining(ctx, call.RoomID, call)
	}
	if _, err := s.announceCall(ctx, call.RoomID, call); err != nil {
		s.log.Warn("could not announce a call somebody left", "error", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleFederatedCallPresent(c *echo.Context) error {
	ctx := c.Request().Context()
	request, _, err := s.memberRequest(c)
	if err != nil {
		return err
	}
	shadow, call, err := s.remoteInCall(ctx, request)
	if err != nil {
		return err
	}
	if _, err := s.queries.TouchRemoteParticipant(ctx, dbgen.TouchRemoteParticipantParams{CallID: call.ID, UserID: shadow.ID}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// handleFederatedCallSignal passes on negotiation from somebody from another
// server, authorised as any participant's is.
func (s *Server) handleFederatedCallSignal(c *echo.Context) error {
	ctx := c.Request().Context()
	request, _, err := s.memberRequest(c)
	if err != nil {
		return err
	}
	shadow, call, err := s.remoteInCall(ctx, request)
	if err != nil {
		return err
	}
	to, err := uuid.Parse(request.To)
	if err != nil || len(request.Signal) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a signal names whom it is for and carries something")
	}
	if err := s.Signal(ctx, shadow.ID, call.ID, to, request.Signal); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "that signal cannot be passed on")
	}
	return c.NoContent(http.StatusAccepted)
}

// ringRemote rings somebody on another server into a call here, in the
// background: their server answers the same whether it rang them or not.
func (s *Server) ringRemote(ctx context.Context, caller dbgen.User, call rooms.Call, address string) bool {
	if !s.callsFederated(ctx) || s.isLocalAddress(address) {
		return false
	}
	host, err := federation.ServerOf(address)
	if err != nil {
		return false
	}
	if allowed, _ := s.federationTrust(ctx).Allows(host); !allowed {
		return false
	}
	if err := s.queries.InviteRemoteToCall(ctx, dbgen.InviteRemoteToCallParams{
		CallID: call.ID, Address: address, InvitedBy: &caller.ID,
	}); err != nil {
		return false
	}
	request := federatedCallRequest{Call: call.ID.String(), RoomID: call.RoomID.String(), To: address}
	request.From.Address = s.rooms.UserAddress(caller.Username)
	request.From.DisplayName = identity.NameOf(caller.Username, caller.DisplayName)
	peer := s.peerAddressFor(ctx, host)
	sender := s.callSender(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := sender.Call(ctx, peer, "ring", request, nil); err != nil {
			s.log.Info("could not ring somebody on another server", "server", host, "error", err)
		}
	}()
	return true
}

/*
relayToRemote passes a call's frame to somebody from another server, through
their server, in order: one queue per person, so an offer is never overtaken
by its own candidates.
*/
func (s *Server) relayToRemote(ctx context.Context, callID, userID uuid.UUID, frame realtime.Frame) {
	if !deliverable[frame.Type] {
		return
	}
	person, err := s.queries.GetUserByID(ctx, userID)
	if err != nil || person.RemoteAddress == nil || person.RemoteServer == nil {
		return
	}
	request := federatedCallRequest{Call: callID.String(), To: *person.RemoteAddress, Type: frame.Type, Payload: frame.Payload}
	// ponytail: one goroutine per person from elsewhere, kept for the life of
	// the process; retire idle queues if a server ever talks to thousands.
	queue, started := s.remoteQueues.LoadOrStore(*person.RemoteAddress, make(chan remoteDelivery, 256))
	if !started {
		go s.deliverRemotely(queue.(chan remoteDelivery))
	}
	select {
	case queue.(chan remoteDelivery) <- remoteDelivery{peer: *person.RemoteServer, request: request}:
	default:
		s.log.Warn("dropped a call frame for somebody on another server: their queue is full", "to", *person.RemoteAddress)
	}
}

type remoteDelivery struct {
	peer    string
	request federatedCallRequest
}

func (s *Server) deliverRemotely(queue chan remoteDelivery) {
	for delivery := range queue {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.callSender(ctx).Call(ctx, delivery.peer, "deliver", delivery.request, nil); err != nil {
			s.log.Info("could not pass a call frame to another server", "to", delivery.request.To, "error", err)
		}
		cancel()
	}
}

// tellPerson passes a call's frame to somebody in it, wherever they are
// connected: on this node, another, or another server.
func (s *Server) tellPerson(ctx context.Context, callID, userID uuid.UUID, frame realtime.Frame) {
	if s.hub == nil {
		return
	}
	here := s.hub.SendToUser(userID, frame)
	if here+s.whisperElsewhere(ctx, userID, frame, here) > 0 {
		return
	}
	s.relayToRemote(ctx, callID, userID, frame)
}

// The half of somebody's own server.

/*
handleFederatedRing rings somebody here into a call another server holds —
if this server takes calls from others (checked before this handler) and the
person accepts calls from people on other servers. The answer is the same
either way, so the caller learns neither setting.
*/
func (s *Server) handleFederatedRing(c *echo.Context) error {
	ctx := c.Request().Context()
	request, err := readCallRequest(c)
	if err != nil {
		return err
	}
	origin, home := verifiedOrigin(c)
	callID, errCall := uuid.Parse(request.Call)
	if errCall != nil || s.peerSpeaksFor(origin, request.From.Address) != nil || !s.isLocalAddress(request.To) {
		return c.NoContent(http.StatusAccepted)
	}
	name, _, _ := strings.Cut(request.To, "@")
	person, err := s.identity.UserByUsername(ctx, name)
	if err != nil || person.GuestCall != nil || person.RemoteAddress != nil {
		return c.NoContent(http.StatusAccepted)
	}
	if s.moderation != nil && !s.moderation.CallPolicyFor(ctx, person.ID).FromOtherServers {
		return c.NoContent(http.StatusAccepted)
	}
	if err := s.queries.RingRemoteCall(ctx, dbgen.RingRemoteCallParams{
		CallID: callID, UserID: person.ID, Home: home, HomeAddress: origin,
	}); err != nil {
		return err
	}
	// Recorded for somebody else's home already: not this one's to ring.
	if rc, err := s.queries.RemoteCallOf(ctx, dbgen.RemoteCallOfParams{CallID: callID, UserID: person.ID}); err != nil || rc.Home != home {
		return c.NoContent(http.StatusAccepted)
	}

	frame := ringFrame{Call: callID.String(), RoomID: request.RoomID, Kind: "conference", Conference: true}
	frame.From.Username, _, _ = strings.Cut(request.From.Address, "@")
	frame.From.DisplayName = request.From.DisplayName
	if frame.From.DisplayName == "" {
		frame.From.DisplayName = frame.From.Username
	}
	frame.From.Address = request.From.Address
	payload, _ := json.Marshal(frame)
	ring := realtime.Frame{Type: realtime.FrameCallRing, Payload: payload}
	if s.hub != nil {
		here := s.hub.SendToUser(person.ID, ring)
		s.whisperElsewhere(ctx, person.ID, ring, here)
	}
	return c.NoContent(http.StatusAccepted)
}

/*
handleFederatedDeliver passes on what a call held elsewhere sends somebody
here: only from the server recorded as that call's home for them, only while
they are rung into it or in it, and only frames a call sends.
*/
func (s *Server) handleFederatedDeliver(c *echo.Context) error {
	ctx := c.Request().Context()
	request, err := readCallRequest(c)
	if err != nil {
		return err
	}
	_, home := verifiedOrigin(c)
	callID, errCall := uuid.Parse(request.Call)
	if errCall != nil || !deliverable[request.Type] || !s.isLocalAddress(request.To) {
		return echo.NewHTTPError(http.StatusNotFound, "nobody here is in that call")
	}
	name, _, _ := strings.Cut(request.To, "@")
	person, err := s.identity.UserByUsername(ctx, name)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "nobody here is in that call")
	}
	rc, err := s.queries.RemoteCallOf(ctx, dbgen.RemoteCallOfParams{CallID: callID, UserID: person.ID})
	if err != nil || rc.Home != home || rc.LeftAt != nil {
		return echo.NewHTTPError(http.StatusNotFound, "nobody here is in that call")
	}

	payload := request.Payload
	frame := realtime.Frame{Type: request.Type, Payload: payload}
	if request.Type == realtime.FrameCallState {
		var state callView
		if json.Unmarshal(payload, &state) != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "an unreadable call state")
		}
		if !slices.ContainsFunc(state.Participants, func(p callParticipantView) bool { return p.UserID == person.ID.String() }) && rc.JoinedAt != nil {
			_ = s.queries.LeftRemoteCall(ctx, dbgen.LeftRemoteCallParams{CallID: callID, UserID: person.ID})
		}
		state = s.localView(rc, state)
		frame.Payload, _ = json.Marshal(state)
		frame.RoomID = state.RoomID
	}
	if s.hub != nil {
		here := s.hub.SendToUser(person.ID, frame)
		s.whisperElsewhere(ctx, person.ID, frame, here)
	}
	return c.NoContent(http.StatusAccepted)
}

// localView names a call in a shared room by this server's copy of the room,
// which is the conversation the person here is in.
func (s *Server) localView(rc dbgen.RemoteCall, view callView) callView {
	if rc.RoomID != nil {
		view.RoomID = rc.RoomID.String()
	}
	return view
}

// remoteCallFor is a call held elsewhere that somebody here is rung into or in.
func (s *Server) remoteCallFor(ctx context.Context, callID, userID uuid.UUID) (dbgen.RemoteCall, bool) {
	rc, err := s.queries.RemoteCallOf(ctx, dbgen.RemoteCallOfParams{CallID: callID, UserID: userID})
	return rc, err == nil
}

// askHome sends one request about somebody here to the home of their call.
func (s *Server) askHome(ctx context.Context, home string, user dbgen.User, request federatedCallRequest, into any) error {
	request.Member = s.rooms.UserAddress(user.Username)
	request.ID = user.ID.String()
	request.DisplayName = identity.NameOf(user.Username, user.DisplayName)
	err := s.callSender(ctx).Call(ctx, home, request.action, request, into)
	var refusal *federation.CallRefusedError
	if errors.As(err, &refusal) {
		status := refusal.Status
		if status != http.StatusConflict && status != http.StatusNotFound {
			status = http.StatusBadGateway
		}
		return echo.NewHTTPError(status, refusal.Reason)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the server holding the call cannot be reached")
	}
	return nil
}

// sharedRoomHome is where the call of a room another server holds is held,
// and the room's slug there.
func (s *Server) sharedRoomHome(ctx context.Context, room dbgen.Room) (string, string, string, error) {
	peers, err := s.rooms.Peers(ctx, room.ID)
	if err != nil || len(peers) == 0 || room.Slug == nil {
		return "", "", "", echo.NewHTTPError(http.StatusConflict, "calls are not available in this conversation")
	}
	address := peers[0].Address
	if address == "" {
		address = "https://" + peers[0].Peer
	}
	return address, peers[0].Peer, *room.Slug, nil
}

/*
remoteRoomCall answers a person here about the call in a room another server
holds, by asking it: what is there, joining, saying what they send, leaving.
Their permission to call in the conversation is this server's to check, and
is; the room's home applies its own rules on top.
*/
func (s *Server) remoteRoomCall(c *echo.Context, room dbgen.Room, action string) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	if !s.callsFederated(ctx) {
		return echo.NewHTTPError(http.StatusConflict, "calls are not available in this conversation")
	}
	address, home, slug, err := s.sharedRoomHome(ctx, room)
	if err != nil {
		return err
	}
	var media callMediaRequest
	if action == "join" || action == "media" {
		if err := c.Bind(&media); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
		}
	}

	var view callView
	if err := s.askHome(ctx, address, user, federatedCallRequest{action: "view", Room: slug}, &view); err != nil && action != "join" {
		return err
	} else if action == "join" {
		needed := authz.PermCallJoin
		if err != nil {
			needed = authz.PermCallStart
		}
		if decision := currentPermissions(c).AllowsChannel(needed, room.ID); !decision.Allowed {
			return echo.NewHTTPError(http.StatusForbidden, decision.Error())
		}
	}
	switch action {
	case "view":
	case "join":
		if err := s.askHome(ctx, address, user, federatedCallRequest{action: "join", Room: slug, Audio: media.Audio, Video: media.Video}, &view); err != nil {
			return err
		}
		callID, _ := uuid.Parse(view.ID)
		if err := s.queries.RingRemoteCall(ctx, dbgen.RingRemoteCallParams{
			CallID: callID, UserID: user.ID, Home: home, HomeAddress: address, RoomID: &room.ID,
		}); err != nil {
			return err
		}
		_ = s.queries.JoinedRemoteCall(ctx, dbgen.JoinedRemoteCallParams{CallID: callID, UserID: user.ID})
		s.endRinging(ctx, user.ID, callID)
	default:
		callID, _ := uuid.Parse(view.ID)
		rc, ok := s.remoteCallFor(ctx, callID, user.ID)
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "there is no call here")
		}
		return s.remoteCallAction(c, rc, action, media)
	}
	rc := dbgen.RemoteCall{RoomID: &room.ID}
	return c.JSON(http.StatusOK, s.localView(rc, view))
}

/*
remoteByID answers a person here about a call held elsewhere that they were
rung into, by asking its home. Reports whether the call was one.
*/
func (s *Server) remoteByID(c *echo.Context, action string) (bool, error) {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	callID, err := uuid.Parse(c.Param("callId"))
	if err != nil {
		return false, nil
	}
	rc, ok := s.remoteCallFor(ctx, callID, user.ID)
	if !ok {
		return false, nil
	}
	if !s.callsFederated(ctx) {
		return true, echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	if action == "join" {
		if decision := currentPermissions(c).AllowsServer(authz.PermCallJoin); !decision.Allowed {
			return true, echo.NewHTTPError(http.StatusForbidden, decision.Error())
		}
	}
	var media callMediaRequest
	if action == "join" || action == "media" {
		if err := c.Bind(&media); err != nil {
			return true, echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
		}
	}
	return true, s.remoteCallAction(c, rc, action, media)
}

// hostedElsewhere is a room another server holds, whose call is held there too.
func (s *Server) hostedElsewhere(c *echo.Context) (dbgen.Room, bool) {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return dbgen.Room{}, false
	}
	room, err := s.rooms.ByID(c.Request().Context(), roomID)
	if err != nil || s.rooms.HostedHere(room) {
		return dbgen.Room{}, false
	}
	return room, true
}

func (s *Server) remoteCallAction(c *echo.Context, rc dbgen.RemoteCall, action string, media callMediaRequest) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	request := federatedCallRequest{action: action, Call: rc.CallID.String(), Audio: media.Audio, Video: media.Video, Held: media.Held}
	switch action {
	case "leave":
		_ = s.queries.LeftRemoteCall(ctx, dbgen.LeftRemoteCallParams{CallID: rc.CallID, UserID: user.ID})
		_ = s.askHome(context.WithoutCancel(ctx), rc.HomeAddress, user, request, nil)
		return c.NoContent(http.StatusNoContent)
	case "view":
		// A call one was rung into is described by the ring until they join.
		if rc.JoinedAt == nil || rc.LeftAt != nil {
			return echo.NewHTTPError(http.StatusNotFound, "there is no call here")
		}
	}
	var view callView
	if err := s.askHome(ctx, rc.HomeAddress, user, request, &view); err != nil {
		return err
	}
	if action == "join" {
		_ = s.queries.JoinedRemoteCall(ctx, dbgen.JoinedRemoteCallParams{CallID: rc.CallID, UserID: user.ID})
		s.endRinging(ctx, user.ID, rc.CallID)
	}
	return c.JSON(http.StatusOK, s.localView(rc, view))
}

// signalRemote passes a person's negotiation in a call held elsewhere to its
// home, in the order it was sent: one at a time, from their connection.
func (s *Server) signalRemote(ctx context.Context, from uuid.UUID, rc dbgen.RemoteCall, to uuid.UUID, signal json.RawMessage) error {
	if rc.JoinedAt == nil || rc.LeftAt != nil || !s.callsFederated(ctx) {
		return rooms.ErrNoCall
	}
	user, err := s.identity.UserByID(ctx, from)
	if err != nil {
		return err
	}
	return s.askHome(ctx, rc.HomeAddress, user, federatedCallRequest{action: "signal", Call: rc.CallID.String(), To: to.String(), Signal: signal}, nil)
}

/*
vouchForRemoteCallers tells each call held elsewhere that its people here are
still connected, or that they have gone. Part of the sweep.
*/
func (s *Server) vouchForRemoteCallers(ctx context.Context) {
	if !s.callsFederated(ctx) {
		return
	}
	present, err := s.queries.PresentInRemoteCalls(ctx)
	if err != nil {
		s.log.Warn("could not read who is in calls elsewhere", "error", err)
		return
	}
	var wait sync.WaitGroup
	for _, rc := range present {
		user, err := s.identity.UserByID(ctx, rc.UserID)
		if err != nil {
			continue
		}
		action := "present"
		if s.presence != nil {
			if connections, err := s.presence.Connections(ctx, rc.UserID); err == nil && connections == 0 {
				action = "leave"
				_ = s.queries.LeftRemoteCall(ctx, dbgen.LeftRemoteCallParams{CallID: rc.CallID, UserID: rc.UserID})
			}
		}
		wait.Go(func() {
			if err := s.askHome(ctx, rc.HomeAddress, user, federatedCallRequest{action: action, Call: rc.CallID.String()}, nil); err != nil {
				s.log.Info("could not tell another server about somebody in its call", "error", err)
			}
		})
	}
	wait.Wait()
}
