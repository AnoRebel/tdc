package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

/*
The live connection, opened from here rather than from the webview.

The web client opens a WebSocket the browser authenticates with its session
cookie. The desktop client's session is in this package's cookie jar, out of the
webview's reach on purpose, so the webview cannot open one that the server would
accept. This does it instead: it dials with the same per-server client every
request uses, so the handshake carries the session, and forwards each frame to
the webview as an event. Messages the webview sends — subscribe, viewing — come
back the other way.

The shared connection machinery above this — reconnection, backoff, replaying
subscriptions — stays in the webview. This is only the socket it was missing.
*/

// LiveBridge opens the live socket for the webview and pumps frames across.
//
// The emitter is how a frame reaches the webview; kept as an interface so the
// pump can be tested without a running application. In the application it is
// the Wails event processor.
// Notifier is the part of the system notifier this bridge uses.
//
// An interface rather than the concrete type so the pump can be tested without
// a desktop session behind it: showing a banner needs a notification daemon,
// and a test that required one would not run anywhere useful.
type Notifier interface {
	Show(title, body string, target Target) error
}

// Target is where a notification takes somebody when it is pressed.
type Target struct {
	Server  string `json:"server"`
	Room    string `json:"room"`
	Message string `json:"message,omitempty"`
}

type LiveBridge struct {
	service  *Service
	emit     func(name string, data ...any) bool
	notifier Notifier

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
}

// The events a frame, an opening and a closing arrive under in the webview.
// Named here and matched in the desktop's live plugin; a mismatch is a live
// connection that opens and is never heard from, so they are worth pinning.
const (
	LiveFrameEvent = "tdc:live:frame"
	LiveOpenEvent  = "tdc:live:open"
	LiveCloseEvent = "tdc:live:close"
)

/*
NewLiveBridge builds the bridge over a service, an emitter and a notifier.

The notifier may be nil, which is how a build without one behaves rather than a
special case to test for: no banners, everything else unchanged.
*/
func NewLiveBridge(service *Service, emit func(name string, data ...any) bool, notifier Notifier) *LiveBridge {
	return &LiveBridge{service: service, emit: emit, notifier: notifier}
}

// ErrNotConnected is what sending before the socket is open reports.
var ErrNotConnected = errors.New("the live connection is not open")

/*
Connect opens the socket to the current server and starts pumping frames.

Idempotent in the sense that opening a second time closes the first: the webview
reconnects by calling this again, and two sockets to one server would deliver
every frame twice.
*/
func (b *LiveBridge) Connect() error {
	s := b.service
	s.mu.RLock()
	base, client := s.current, s.clients[s.current]
	s.mu.RUnlock()

	if base == "" || client == nil {
		return ErrNoServer
	}

	// Any previous connection goes first, so a reconnection does not leave two.
	b.Close()

	url := strings.Replace(base, "http", "ws", 1) + "/api/live"

	/*
		The connection's own context, not the call's.

		A bound method is handed a context that Wails cancels when the call
		returns, and this call returns as soon as the socket is open. Dialling
		with it left a connection that the library tore down an instant later —
		the handshake succeeded, the first read failed with "context canceled",
		and the webview reconnected in a loop that never delivered a frame.

		This one lives as long as the connection, and Close is what ends it.
	*/
	pumpCtx, cancel := context.WithCancel(context.Background())

	// Bounded only for the handshake: dialling should not hang forever, but
	// what follows must outlive it.
	dialCtx, endDial := context.WithTimeout(pumpCtx, 30*time.Second)
	defer endDial()

	conn, _, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		cancel()
		return err
	}
	// The server sends frames far larger than the default read limit once a
	// timeline replay carries attachments; lift it to the same ceiling media
	// uses rather than have a large frame close the connection.
	conn.SetReadLimit(mediaLimit)

	b.mu.Lock()
	b.conn = conn
	b.cancel = cancel
	b.mu.Unlock()

	b.emit(LiveOpenEvent)
	go b.pump(pumpCtx, conn)
	return nil
}

// pump reads frames until the connection closes, forwarding each to the webview.
func (b *LiveBridge) pump(ctx context.Context, conn *websocket.Conn) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			// A read error is the connection ending, deliberately or not. The
			// webview's machinery decides whether to reconnect; this only tells
			// it the socket is gone.
			b.emit(LiveCloseEvent)
			return
		}
		b.emit(LiveFrameEvent, string(data))
		b.notify(data)
	}
}

/*
Send passes one message from the webview to the server.

The message is the JSON the webview built — a subscribe or a viewing. Handed on
as a text frame unchanged, so the protocol lives in one place, the webview,
rather than being half here.
*/
func (b *LiveBridge) Send(message json.RawMessage) error {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()

	if conn == nil {
		return ErrNotConnected
	}

	// A write deadline of its own, for the same reason the dial has one: the
	// call's context is cancelled the moment this returns, and a write given it
	// would be cancelled mid-frame.
	ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()

	return conn.Write(ctx, websocket.MessageText, message)
}

// Close ends the connection if one is open. Safe to call when none is.
func (b *LiveBridge) Close() {
	b.mu.Lock()
	conn, cancel := b.conn, b.cancel
	b.conn, b.cancel = nil, nil
	b.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		// Normal closure; the webview asked, or is replacing this connection.
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
}

/*
What a frame has to be before it is worth interrupting somebody.

Only the body of a message somebody else sent. Everything else the socket
carries — a reaction, an edit, a receipt, the control frames — is either not
about a person saying something or is already visible to somebody looking at
the conversation.
*/
type liveFrame struct {
	Type    string `json:"type"`
	RoomID  string `json:"roomId"`
	Payload struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Sender  string `json:"sender"`
		Content struct {
			Body string `json:"body"`
		} `json:"content"`
		Redacted bool `json:"redacted"`
	} `json:"payload"`
}

/*
notify raises a banner for a message from somebody else.

Here rather than in the webview, which is the whole reason it works: a window
that has been closed to the tray has no page running, so a notification asked
for by JavaScript would never be asked for at all. Go is still holding the
socket, and this is what it is holding it for.

Whether the conversation is on screen is the server's decision, not this one —
it knows which room each socket is watching and does not send an event the
person is looking at arrive. What is decided here is narrower: that this is a
message, that it has words in it, and that somebody else wrote it.
*/
func (b *LiveBridge) notify(data []byte) {
	if b.notifier == nil {
		return
	}

	var frame liveFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return
	}

	if frame.Type == "call.ring" {
		b.announceRing(data)
		return
	}

	if frame.Type != "event" || frame.Payload.Type != "message.sent" {
		return
	}
	// A removed message has no body to show, and announcing one would be
	// telling somebody about something that is no longer there.
	if frame.Payload.Redacted {
		return
	}
	if strings.TrimSpace(frame.Payload.Content.Body) == "" {
		return
	}
	// A banner for something this person just typed is the interruption that
	// teaches people to switch notifications off.
	if b.service.isMe(frame.Payload.Sender) {
		return
	}

	b.service.mu.RLock()
	server := b.service.current
	b.service.mu.RUnlock()

	// The sender's own address is the title: the room's name is not in the
	// frame, and "someone@server" says more about whether to look than a
	// channel identifier would.
	_ = b.notifier.Show(frame.Payload.Sender, frame.Payload.Content.Body, Target{
		Server:  server,
		Room:    frame.RoomID,
		Message: frame.Payload.ID,
	})
}

/*
announceRing raises a banner for somebody calling.

The call itself rings in the window, which may be hidden behind others or
closed to the tray; the banner is what reaches somebody then, and pressing it
brings them to the conversation, where the call can be answered.
*/
func (b *LiveBridge) announceRing(data []byte) {
	var ring struct {
		RoomID  string `json:"roomId"`
		Payload struct {
			RoomName string `json:"roomName"`
			Kind     string `json:"kind"`
			From     struct {
				Username string `json:"username"`
				Address  string `json:"address"`
			} `json:"from"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &ring); err != nil || ring.Payload.From.Username == "" {
		return
	}

	where := "A direct call"
	if ring.Payload.Kind != "direct" && ring.Payload.RoomName != "" {
		where = "In #" + ring.Payload.RoomName
	}

	b.service.mu.RLock()
	server := b.service.current
	b.service.mu.RUnlock()

	_ = b.notifier.Show(ring.Payload.From.Username+" is calling", where, Target{
		Server: server,
		Room:   ring.RoomID,
	})
}
