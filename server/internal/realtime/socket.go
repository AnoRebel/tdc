package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/coder/websocket"
)

// clientMessage is what a client sends.
type clientMessage struct {
	Action string `json:"action"`
	// Rooms to subscribe to or unsubscribe from.
	Rooms []string `json:"rooms,omitempty"`
	// Since is the last sequence the client holds for each room, so the server
	// can replay exactly what was missed.
	Since map[string]int64 `json:"since,omitempty"`
	// Room is the one this client currently has on screen, for the viewing
	// action. Empty means it is looking at none.
	Room string `json:"room,omitempty"`
	// Call, To and Signal carry one piece of a call's negotiation — an offer,
	// an answer or a network candidate — for one other participant.
	Call   string          `json:"call,omitempty"`
	To     string          `json:"to,omitempty"`
	Signal json.RawMessage `json:"signal,omitempty"`
	// Transfer names the direct transfer a signal belongs to; its other party
	// is the recipient, so none is named.
	Transfer string `json:"transfer,omitempty"`
}

// Client actions.
const (
	actionSubscribe   = "subscribe"
	actionUnsubscribe = "unsubscribe"
	actionPing        = "ping"
	// actionViewing tells the server which room this client has on screen, so
	// notifications for it can be suppressed while somebody is looking at it.
	actionViewing = "viewing"
	// actionSignal relays call negotiation to another participant.
	actionSignal = "signal"
	// actionTransfer relays a direct transfer's negotiation to its other party.
	actionTransfer = "transfer"
)

/*
Signaller authorises and delivers a call's negotiation between two of its
participants. Implemented where calls are known; a server without calls passes
none, and signal messages are refused.
*/
type Signaller interface {
	Signal(ctx context.Context, from, callID, to uuid.UUID, signal json.RawMessage) error
	// SignalTransfer passes a direct transfer's negotiation to the other of
	// its two people, if the transfer allows it now.
	SignalTransfer(ctx context.Context, from, transferID uuid.UUID, signal json.RawMessage) error
}

// maxClientMessageBytes bounds one client message.
//
// Clients send control messages and a call's session descriptions, which run to
// a few kilobytes; a limit stops a connection being used to make the server
// allocate.
const maxClientMessageBytes = 64 * 1024

// Replayer supplies the events a reconnecting client missed.
type Replayer interface {
	// EventsSince returns the events after a sequence, or reports that the gap
	// is too large to replay.
	EventsSince(ctx context.Context, roomID, userID uuid.UUID, afterSequence int64) ([]Frame, bool, error)
	// CanSubscribe reports whether a user may receive a room's events.
	CanSubscribe(ctx context.Context, roomID, userID uuid.UUID) (bool, error)
}

// Lifecycle is told when a person's connections open and close.
//
// An interface so the transport does not depend on the presence package, and
// so a server that does not track presence can pass nothing.
type Lifecycle interface {
	Connected(ctx context.Context, userID uuid.UUID) error
	Disconnected(ctx context.Context, userID uuid.UUID) error
	Touch(ctx context.Context, userID uuid.UUID) error
}

// Server upgrades HTTP connections and runs them.
type Server struct {
	hub       *Hub
	replayer  Replayer
	lifecycle Lifecycle
	signaller Signaller
	keepalive time.Duration
	log       *slog.Logger
}

// SetSignaller gives the server somewhere to send call negotiation.
func (s *Server) SetSignaller(signaller Signaller) { s.signaller = signaller }

// NewServer builds the WebSocket server.
//
// lifecycle may be nil, in which case connection state is not reported
// anywhere.
func NewServer(
	hub *Hub,
	replayer Replayer,
	lifecycle Lifecycle,
	keepalive time.Duration,
	log *slog.Logger,
) *Server {
	if keepalive <= 0 {
		keepalive = DefaultKeepalive
	}
	return &Server{
		hub:       hub,
		replayer:  replayer,
		lifecycle: lifecycle,
		keepalive: keepalive,
		log:       log,
	}
}

// Handle upgrades a request and serves the connection until it closes.
//
// The caller has already authenticated the request: a connection is never
// established for an unauthenticated caller, so there is no anonymous state to
// manage.
func (s *Server) Handle(w http.ResponseWriter, r *http.Request, userID uuid.UUID, allowedOrigins []string) error {
	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Origins are checked against the operator's list rather than
		// reflected: a browser does not apply the same-origin policy to
		// WebSockets, so without this any site could open one on a visitor's
		// behalf and read their messages.
		OriginPatterns: allowedOrigins,
		// Compression is left off: chat frames are small, and the per-message
		// context costs memory per connection, which matters against the
		// server's budget.
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return fmt.Errorf("accept websocket: %w", err)
	}

	socket.SetReadLimit(maxClientMessageBytes)

	conn := NewConnection(userID)
	s.hub.Add(conn)

	// Presence follows the connection: opening one makes the person present,
	// and closing the last one ends it.
	if s.lifecycle != nil {
		if err := s.lifecycle.Connected(r.Context(), userID); err != nil {
			s.log.Error("cannot record connection", slog.String("error", err.Error()))
		}
	}

	defer func() {
		s.hub.Remove(conn)
		if s.lifecycle != nil {
			// A fresh context: the request's is already cancelled by the time
			// this runs, and the disconnection still has to be recorded.
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.lifecycle.Disconnected(closeCtx, userID); err != nil {
				s.log.Error("cannot record disconnection", slog.String("error", err.Error()))
			}
		}
		_ = socket.CloseNow()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// The writer runs separately so that a slow network cannot block the
	// reader, and so keepalives continue while the client is idle.
	go s.writeLoop(ctx, cancel, socket, conn)

	s.hub.SendTo(conn, Frame{Type: FrameReady})

	s.readLoop(ctx, socket, conn)
	return nil
}

// readLoop handles what the client sends until the connection ends.
func (s *Server) readLoop(ctx context.Context, socket *websocket.Conn, conn *Connection) {
	for {
		var message clientMessage
		if err := readJSON(ctx, socket, &message); err != nil {
			// A closed connection is ordinary; anything else is worth a line.
			if !isNormalClosure(err) && ctx.Err() == nil {
				s.log.Debug("websocket read ended",
					slog.String("connectionId", conn.ID.String()),
					slog.String("error", err.Error()),
				)
			}
			conn.Close()
			return
		}

		switch message.Action {
		case actionSubscribe:
			s.handleSubscribe(ctx, conn, message)
		case actionUnsubscribe:
			s.handleUnsubscribe(conn, message)
		case actionPing:
			s.hub.SendTo(conn, Frame{Type: FramePong})
		case actionViewing:
			s.handleViewing(conn, message)
		case actionSignal:
			s.handleSignal(ctx, conn, message)
		case actionTransfer:
			s.handleTransferSignal(ctx, conn, message)
		default:
			s.sendError(conn, "unknown action "+message.Action)
		}
	}
}

// handleSubscribe adds rooms and replays anything the client missed.
//
// Every room is checked against the server's own view of membership: a client
// asking to subscribe to a room it may not see is refused, so subscription is
// not a way to discover a private room.
func (s *Server) handleSubscribe(ctx context.Context, conn *Connection, message clientMessage) {
	for _, raw := range message.Rooms {
		roomID, err := uuid.Parse(raw)
		if err != nil {
			s.sendError(conn, "not a valid room identifier: "+raw)
			continue
		}

		allowed, err := s.replayer.CanSubscribe(ctx, roomID, conn.UserID)
		if err != nil {
			s.log.Error("cannot check subscription",
				slog.String("roomId", roomID.String()),
				slog.String("error", err.Error()),
			)
			s.sendError(conn, "could not subscribe to "+raw)
			continue
		}
		if !allowed {
			// Refused without saying whether the room exists, so subscribing
			// cannot be used to probe for private rooms.
			s.sendError(conn, "cannot subscribe to "+raw)
			continue
		}

		conn.Subscribe(roomID)
		s.hub.SendTo(conn, Frame{Type: FrameSubscribed, RoomID: roomID.String()})

		if since, ok := message.Since[raw]; ok {
			s.replay(ctx, conn, roomID, since)
		}
	}
}

/*
handleViewing records which room this client has on screen.

No authorisation check: saying you are looking at a room grants nothing. The
only effect is to suppress your own notifications for it, so the worst a
dishonest client achieves is silencing itself.

An empty room means looking at none, which is what a client sends when its
window loses focus — a person who has tabbed away should be notified again.
*/
func (s *Server) handleViewing(conn *Connection, message clientMessage) {
	if message.Room == "" {
		conn.Viewing(nil)
		return
	}

	roomID, err := uuid.Parse(message.Room)
	if err != nil {
		s.sendError(conn, "not a valid room identifier: "+message.Room)
		return
	}

	conn.Viewing(&roomID)
}

// replay sends the events a client missed, or tells it to resynchronise.
func (s *Server) replay(ctx context.Context, conn *Connection, roomID uuid.UUID, since int64) {
	missed, replayable, err := s.replayer.EventsSince(ctx, roomID, conn.UserID, since)
	if err != nil {
		s.log.Error("cannot replay missed events",
			slog.String("roomId", roomID.String()),
			slog.String("error", err.Error()),
		)
		return
	}

	if !replayable {
		// The gap is larger than the server will replay. Saying so is
		// essential: silently sending a partial stream would leave the client
		// with a hole it cannot detect.
		s.hub.SendTo(conn, Frame{Type: FrameResync, RoomID: roomID.String()})
		return
	}

	for _, frame := range missed {
		s.hub.SendTo(conn, frame)
	}
}

func (s *Server) handleUnsubscribe(conn *Connection, message clientMessage) {
	for _, raw := range message.Rooms {
		roomID, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		conn.Unsubscribe(roomID)
	}
}

func (s *Server) sendError(conn *Connection, message string) {
	payload, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		return
	}
	s.hub.SendTo(conn, Frame{Type: FrameError, Payload: payload})
}

// writeLoop sends queued frames and keeps the connection alive.
func (s *Server) writeLoop(ctx context.Context, cancel context.CancelFunc, socket *websocket.Conn, conn *Connection) {
	defer cancel()

	ticker := time.NewTicker(s.keepalive)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-conn.Closed():
			_ = socket.Close(websocket.StatusNormalClosure, "")
			return

		case frame := <-conn.Frames():
			// Bounded so one unreachable client cannot hold a goroutine and a
			// buffer indefinitely.
			writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
			err := writeJSON(writeCtx, socket, frame)
			writeCancel()
			if err != nil {
				conn.Close()
				return
			}

		case <-ticker.C:
			// A ping proves the connection is still there. An idle connection
			// costs one small frame per interval and nothing else, which is
			// what keeps a phone on metered data cheap to leave connected.
			pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
			err := socket.Ping(pingCtx)
			if err == nil && s.lifecycle != nil {
				// The same ping refreshes presence, so a live connection is
				// not swept as stale.
				if touchErr := s.lifecycle.Touch(pingCtx, conn.UserID); touchErr != nil {
					s.log.Debug("cannot refresh presence", slog.String("error", touchErr.Error()))
				}
			}
			pingCancel()
			if err != nil {
				conn.Close()
				return
			}
		}
	}
}

func readJSON(ctx context.Context, socket *websocket.Conn, value any) error {
	_, data, err := socket.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func writeJSON(ctx context.Context, socket *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return socket.Write(ctx, websocket.MessageText, data)
}

func isNormalClosure(err error) bool {
	status := websocket.CloseStatus(err)
	return status == websocket.StatusNormalClosure ||
		status == websocket.StatusGoingAway ||
		errors.Is(err, context.Canceled)
}

/*
handleSignal passes one piece of call negotiation on, if the signaller allows
it. A refusal is reported to the sender only, and says nothing about whether the
call or the recipient exists.
*/
func (s *Server) handleSignal(ctx context.Context, conn *Connection, message clientMessage) {
	if s.signaller == nil {
		s.sendError(conn, "calls are not available")
		return
	}
	callID, errCall := uuid.Parse(message.Call)
	to, errTo := uuid.Parse(message.To)
	if errCall != nil || errTo != nil || len(message.Signal) == 0 {
		s.sendError(conn, "a signal needs a call, a recipient and a body")
		return
	}
	if err := s.signaller.Signal(ctx, conn.UserID, callID, to, message.Signal); err != nil {
		s.sendError(conn, "cannot signal that call")
	}
}

/*
handleTransferSignal passes one piece of a direct transfer's negotiation on.
Like a call's, it is authorised on every message, and a refusal says nothing
about whether the transfer exists.
*/
func (s *Server) handleTransferSignal(ctx context.Context, conn *Connection, message clientMessage) {
	if s.signaller == nil {
		s.sendError(conn, "direct transfers are not available")
		return
	}
	transferID, err := uuid.Parse(message.Transfer)
	if err != nil || len(message.Signal) == 0 {
		s.sendError(conn, "a transfer signal needs a transfer and a body")
		return
	}
	if err := s.signaller.SignalTransfer(ctx, conn.UserID, transferID, message.Signal); err != nil {
		s.sendError(conn, "cannot signal that transfer")
	}
}
