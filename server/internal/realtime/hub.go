// Package realtime delivers events to connected clients as they happen.
//
// The hub is server-authoritative: a connection receives an event only if the
// server decides it should, checked against membership at delivery time rather
// than trusting what the client asked to subscribe to.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Frame is one message sent to a client.
type Frame struct {
	// Type distinguishes an event from a control message.
	Type string `json:"type"`
	// Room the frame concerns, in room@server form, when it concerns one.
	Room string `json:"room,omitempty"`
	// RoomID is the local identifier, which clients use for subscription.
	RoomID string `json:"roomId,omitempty"`
	// Sequence is the event's position in its room, so a client can detect a
	// gap and ask for what it missed.
	Sequence int64 `json:"sequence,omitempty"`
	// Payload is the frame's body.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Frame types.
const (
	// FrameEvent carries a room event.
	FrameEvent = "event"
	// FrameReady is sent once a connection is established, so a client knows
	// it may begin.
	FrameReady = "ready"
	// FrameSubscribed acknowledges a subscription change.
	FrameSubscribed = "subscribed"
	// FrameResync tells a client it has fallen too far behind to replay and
	// must fetch the timeline afresh. Sent rather than silently omitting
	// events, which would leave the client with a gap it cannot see.
	FrameResync = "resync"
	// FrameError reports a problem with something the client sent.
	FrameError = "error"
	// FramePong answers a client's keepalive.
	FramePong = "pong"
	// FrameCallSignal carries another participant's call negotiation.
	FrameCallSignal = "call.signal"
	// FrameCallState says who is in a call and what each is sending.
	FrameCallState = "call.state"
	// FrameCallRing tells somebody they are being called.
	FrameCallRing = "call.ring"
	// FrameCallRingEnded tells somebody's devices to stop ringing: they
	// answered on one, declined, or nobody is calling any more.
	FrameCallRingEnded = "call.ring_ended"
	// FrameCallDeclined tells the people in a call that somebody they rang
	// declined.
	FrameCallDeclined = "call.declined"
)

// Connection is one client's live connection.
type Connection struct {
	ID     uuid.UUID
	UserID uuid.UUID

	// send carries frames to the writer goroutine. Buffered so a slow reader
	// does not block the sender; a full buffer means the client cannot keep up
	// and is disconnected rather than allowed to stall everyone else.
	send chan Frame

	mu sync.RWMutex
	// subscribed rooms, by identifier. A client receives events only for
	// rooms it has subscribed to, so opening one channel does not stream every
	// room the person belongs to.
	subscribed map[uuid.UUID]bool

	/*
		viewing is the one room this client currently has on screen.

		Distinct from subscribed, which is every room it wants events for: a
		client subscribes to several and looks at one. Notifying somebody about
		a message they are watching arrive is the interruption that teaches
		people to switch notifications off, so this is what suppression needs.

		Nil when the window is not focused, so a client left open in a
		background tab notifies normally.
	*/
	viewing *uuid.UUID

	closeOnce sync.Once
	closed    chan struct{}
}

// sendBuffer is how many frames may queue for one client before it is
// considered unable to keep up.
const sendBuffer = 64

// NewConnection builds a connection for a user.
func NewConnection(userID uuid.UUID) *Connection {
	return &Connection{
		ID:         uuid.New(),
		UserID:     userID,
		send:       make(chan Frame, sendBuffer),
		subscribed: make(map[uuid.UUID]bool),
		closed:     make(chan struct{}),
	}
}

// Frames returns the channel the writer reads from.
func (c *Connection) Frames() <-chan Frame { return c.send }

// Closed reports when the connection has been closed.
func (c *Connection) Closed() <-chan struct{} { return c.closed }

// Close ends the connection. Safe to call more than once.
func (c *Connection) Close() {
	c.closeOnce.Do(func() { close(c.closed) })
}

// Subscribe adds rooms to what this connection receives.
func (c *Connection) Subscribe(roomIDs ...uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range roomIDs {
		c.subscribed[id] = true
	}
}

// Unsubscribe removes rooms.
func (c *Connection) Unsubscribe(roomIDs ...uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range roomIDs {
		delete(c.subscribed, id)
	}
}

/*
Viewing records which room this client has on screen, if any.

A nil room means it is looking at nothing — the window is in the background, or
the person is on a settings page. Either way, notifications resume.
*/
func (c *Connection) Viewing(roomID *uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.viewing = roomID
}

// IsViewing reports whether this connection has a room on screen.
func (c *Connection) IsViewing(roomID uuid.UUID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.viewing != nil && *c.viewing == roomID
}

// SubscribedTo reports whether this connection wants a room's events.
func (c *Connection) SubscribedTo(roomID uuid.UUID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subscribed[roomID]
}

// Subscriptions returns the rooms this connection is receiving.
func (c *Connection) Subscriptions() []uuid.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]uuid.UUID, 0, len(c.subscribed))
	for id := range c.subscribed {
		ids = append(ids, id)
	}
	return ids
}

// deliver queues a frame, reporting whether the client is keeping up.
func (c *Connection) deliver(frame Frame) bool {
	select {
	case c.send <- frame:
		return true
	case <-c.closed:
		return false
	default:
		// The buffer is full: this client is not reading fast enough. Dropping
		// it is better than blocking the sender, which would let one slow
		// client stall delivery for everyone in the room.
		return false
	}
}

// Membership answers whether a user may receive a room's events.
//
// An interface rather than a concrete store so the hub does not depend on the
// rooms package, and so tests can drive it directly.
type Membership interface {
	// MemberIDs returns who is currently in a room.
	MemberIDs(ctx context.Context, roomID uuid.UUID) ([]uuid.UUID, error)
}

// Hub tracks connections and fans events out to them.
type Hub struct {
	mu sync.RWMutex
	// connections by identifier.
	connections map[uuid.UUID]*Connection
	// byUser indexes connections per user, so a person's several devices can
	// be found without scanning every connection.
	byUser map[uuid.UUID]map[uuid.UUID]*Connection

	membership Membership
	log        *slog.Logger
}

// NewHub builds a hub.
func NewHub(membership Membership, log *slog.Logger) *Hub {
	return &Hub{
		connections: make(map[uuid.UUID]*Connection),
		byUser:      make(map[uuid.UUID]map[uuid.UUID]*Connection),
		membership:  membership,
		log:         log,
	}
}

// Add registers a connection.
func (h *Hub) Add(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.connections[conn.ID] = conn
	if h.byUser[conn.UserID] == nil {
		h.byUser[conn.UserID] = make(map[uuid.UUID]*Connection)
	}
	h.byUser[conn.UserID][conn.ID] = conn
}

// Remove deregisters a connection and closes it.
func (h *Hub) Remove(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.connections, conn.ID)
	if forUser, ok := h.byUser[conn.UserID]; ok {
		delete(forUser, conn.ID)
		if len(forUser) == 0 {
			delete(h.byUser, conn.UserID)
		}
	}

	conn.Close()
}

/*
IsWatching reports whether somebody has a room on screen on any device.

Any rather than all: somebody reading a channel on their phone does not need it
raised on their laptop too. Every connection is checked because a person may
have several, and one of them looking is enough.
*/
func (h *Hub) IsWatching(userID, roomID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, conn := range h.byUser[userID] {
		if conn.IsViewing(roomID) {
			return true
		}
	}
	return false
}

// Count reports how many connections are open, for diagnostics.
/*
AnyoneWatching says whether any connection here is subscribed to a room.

Asked before reading an event another node announced. Every node hears every
announcement and most concern rooms this node holds no connection to, so
checking first turns a database read per event per node into a read only where
there is somebody to deliver to.
*/
func (h *Hub) AnyoneWatching(roomID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, conn := range h.connections {
		if conn.SubscribedTo(roomID) {
			return true
		}
	}
	return false
}

func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// Publish delivers an event to everyone entitled to receive it.
//
// Membership is read at delivery time rather than trusted from what the client
// subscribed to. That is what makes a removal take effect at once: a
// connection whose user is no longer in the room stops receiving its events
// without having to reconnect.
func (h *Hub) Publish(ctx context.Context, roomID uuid.UUID, frame Frame) {
	members, err := h.membership.MemberIDs(ctx, roomID)
	if err != nil {
		h.log.Error("cannot determine room members for delivery",
			slog.String("roomId", roomID.String()),
			slog.String("error", err.Error()),
		)
		return
	}

	entitled := make(map[uuid.UUID]bool, len(members))
	for _, id := range members {
		entitled[id] = true
	}

	h.mu.RLock()
	targets := make([]*Connection, 0, len(h.connections))
	for userID, forUser := range h.byUser {
		if !entitled[userID] {
			continue
		}
		for _, conn := range forUser {
			if conn.SubscribedTo(roomID) {
				targets = append(targets, conn)
			}
		}
	}
	h.mu.RUnlock()

	for _, conn := range targets {
		if !conn.deliver(frame) {
			h.log.Warn("dropping a connection that cannot keep up",
				slog.String("connectionId", conn.ID.String()),
				slog.String("userId", conn.UserID.String()),
			)
			h.Remove(conn)
		}
	}
}

// SendTo delivers a frame to one connection, for replies to that client.
func (h *Hub) SendTo(conn *Connection, frame Frame) {
	if !conn.deliver(frame) {
		h.Remove(conn)
	}
}

// CloseAll ends every connection, for shutdown.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, conn := range h.connections {
		conn.Close()
	}
	h.connections = make(map[uuid.UUID]*Connection)
	h.byUser = make(map[uuid.UUID]map[uuid.UUID]*Connection)
}

// DefaultKeepalive is how often the server pings an idle connection.
//
// Frequent enough to notice a dead connection and to keep intermediaries from
// closing an idle one, infrequent enough that an idle client costs almost
// nothing — which matters on metered mobile data.
const DefaultKeepalive = 45 * time.Second
