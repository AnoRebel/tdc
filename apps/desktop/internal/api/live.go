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
type LiveBridge struct {
	service *Service
	emit    func(name string, data ...any) bool

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

// NewLiveBridge builds the bridge over a service and an emitter.
func NewLiveBridge(service *Service, emit func(name string, data ...any) bool) *LiveBridge {
	return &LiveBridge{service: service, emit: emit}
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
