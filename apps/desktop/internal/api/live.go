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
	// Withdraw takes down the banner about a target, if one is showing.
	Withdraw(target Target) error
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

	/*
		The banner each ringing call raised, by the call's identifier, so the
		frame saying it stopped ringing — which names only the call — can
		take the right one down.
	*/
	ringsMu sync.Mutex
	rings   map[string]Target
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
			/*
				A read error is the connection ending, deliberately or not. The
				webview's machinery decides whether to reconnect; this only tells
				it the socket is gone — and only if it is still the socket. One
				replaced by a reconnection ends too, after the new one has
				opened, and saying so would have the webview reconnect again,
				and again.
			*/
			b.mu.Lock()
			current, cancel := b.conn == conn, b.cancel
			if current {
				b.conn, b.cancel = nil, nil
			}
			b.mu.Unlock()
			if current {
				cancel()
				b.emit(LiveCloseEvent)
			}
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
		ID     string `json:"id"`
		Type   string `json:"type"`
		Sender string `json:"sender"`
		// SenderName is what to call the sender, absent for somebody on
		// another server.
		SenderName string `json:"senderName"`
		Content    struct {
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
	if frame.Type == "call.ring_ended" {
		b.withdrawRing(data)
		return
	}
	if frame.Type == "event.reminder" {
		b.announceReminder(data)
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

	// Who wrote it is the title: the room's name is not in the frame, and a
	// person's name says more about whether to look than a channel identifier
	// would. Their display name, or for somebody on another server, whose name
	// this server does not know, their address.
	//
	// The body is Markdown source; a banner shows its words, not its markers.
	title := frame.Payload.SenderName
	if title == "" {
		title = frame.Payload.Sender
	}
	_ = b.notifier.Show(title, b.service.bannerBody(plainText(frame.Payload.Content.Body)), Target{
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
			Call     string `json:"call"`
			RoomName string `json:"roomName"`
			Kind     string `json:"kind"`
			From     struct {
				Username    string `json:"username"`
				DisplayName string `json:"displayName"`
				Address     string `json:"address"`
			} `json:"from"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &ring); err != nil || ring.Payload.From.Username == "" {
		return
	}

	where := b.service.banner(func(w BannerWords) string { return w.DirectCall }, nil)
	switch {
	case ring.Payload.Kind == "conference":
		// Somebody merging calls; the conversation is not theirs to see.
		where = b.service.banner(func(w BannerWords) string { return w.Conference }, nil)
	case ring.Payload.Kind != "direct" && ring.Payload.RoomName != "":
		where = b.service.banner(func(w BannerWords) string { return w.InChannel },
			map[string]string{"room": ring.Payload.RoomName})
	}

	b.service.mu.RLock()
	server := b.service.current
	b.service.mu.RUnlock()

	// Called by their display name, or their username without one.
	from := ring.Payload.From.DisplayName
	if strings.TrimSpace(from) == "" {
		from = ring.Payload.From.Username
	}
	target := Target{Server: server, Room: ring.RoomID}
	if ring.Payload.Call != "" {
		b.ringsMu.Lock()
		if b.rings == nil {
			b.rings = map[string]Target{}
		}
		b.rings[ring.Payload.Call] = target
		b.ringsMu.Unlock()
	}
	_ = b.notifier.Show(b.service.banner(func(w BannerWords) string { return w.Calling },
		map[string]string{"name": from}), where, target)
}

/*
withdrawRing takes down a ringing call's banner once it has stopped ringing:
answered on another device, declined, or given up on by the caller. The web
client closes its own the same way; without this the banner went on saying
somebody was calling after the call had ended.
*/
func (b *LiveBridge) withdrawRing(data []byte) {
	var ended struct {
		Payload struct {
			Call string `json:"call"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &ended); err != nil || ended.Payload.Call == "" {
		return
	}
	b.ringsMu.Lock()
	target, ok := b.rings[ended.Payload.Call]
	delete(b.rings, ended.Payload.Call)
	b.ringsMu.Unlock()
	if ok {
		_ = b.notifier.Withdraw(target)
	}
}

/*
announceReminder raises a banner for an event somebody is going to that is
about to start. The server sends the frame only to people taking part who have
not muted the event; pressing the banner opens the event's conversation, which
the shared interface turns into the event's page.
*/
func (b *LiveBridge) announceReminder(data []byte) {
	var reminder struct {
		RoomID  string `json:"roomId"`
		Payload struct {
			Title   string `json:"title"`
			Minutes int    `json:"minutes"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &reminder); err != nil || reminder.Payload.Title == "" {
		return
	}
	body := b.service.startsIn(reminder.Payload.Minutes)

	b.service.mu.RLock()
	server := b.service.current
	b.service.mu.RUnlock()

	_ = b.notifier.Show(reminder.Payload.Title, body, Target{Server: server, Room: reminder.RoomID})
}
