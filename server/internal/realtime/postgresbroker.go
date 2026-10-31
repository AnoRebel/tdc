package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

/*
Carrying announcements over the database every node already shares.

Postgres's LISTEN and NOTIFY, rather than a message broker to install. The
deployment this is for is a small operator with two nodes behind a load
balancer; asking them to run and maintain a queue as well would be asking more
than the problem is worth, and they are already running Postgres or there would
be no server.

What it is not is durable. An announcement reaches whoever is listening at that
moment and nobody else — a node that is starting up, or whose connection has
just dropped, misses it. That is acceptable here and nowhere else: the event is
in the log either way, so a client that misses the live frame receives it on
reconnection, which is the same path a client with a bad network already takes.
*/

// notifyChannel is what every node listens on. One channel for every room,
// because the announcement names the room and a channel per room would be
// thousands of LISTENs on every node.
const notifyChannel = "tdc_events"

/*
reconnectDelay is how long to wait before listening again after the connection
drops.

A second, not a backoff. A dropped listener means live delivery is silently
degraded for as long as it lasts, so the priority is being back quickly; and the
thing it reconnects to is a database this process needs anyway, so a tight loop
against a server that is down is not the risk it would be against somebody
else's API.
*/
const reconnectDelay = time.Second

/*
privateChannel carries frames meant for one person, and the acknowledgements
that they arrived.

A channel of its own rather than a kind of announcement, because what it
carries is the content itself — an announcement names an event every node can
read from the database, and a private frame is deliberately in no table at all.
*/
const privateChannel = "tdc_private"

/*
privatePieceSize is how much of a private frame one notification carries.

NOTIFY takes at most eight thousand bytes, and a private answer may be that long
before it is wrapped in a frame. So a frame is cut into pieces, each base64 so
its size is known in advance — 5000 bytes become 6668 — and sent in one
transaction, which Postgres delivers to each listener together and in order.
*/
const privatePieceSize = 5000

// maxPrivateFrame bounds what is sent in pieces at all: comfortably more than
// the longest answer a program may give, and far less than would crowd the
// notification queue every node shares.
const maxPrivateFrame = 64 << 10

// maxPieces and maxAssemblies bound what a node will hold for frames still
// arriving. A piece says how many pieces its frame has; believing any number
// would let one notification make a node allocate without limit.
const (
	maxPieces     = maxPrivateFrame/privatePieceSize + 1
	maxAssemblies = 256
)

// assemblyLimit is how long a frame's first piece waits for the rest before it
// is dropped. Pieces arrive together, so this only collects what a listener
// that reconnected mid-delivery will never complete.
const assemblyLimit = 30 * time.Second

// privatePiece is one notification on the private channel: a piece of a frame,
// or a node's acknowledgement that it delivered one.
type privatePiece struct {
	ID     string `json:"i"`
	Origin string `json:"o"`
	// User is who the frame is for, on every piece, so a node holding no
	// connection for them can drop the piece before decoding anything.
	User uuid.UUID `json:"u,omitempty"`
	Part int       `json:"n,omitempty"`
	Of   int       `json:"of,omitempty"`
	Data string    `json:"d,omitempty"`
	// Ack marks an acknowledgement, and Delivered how many connections on
	// the acknowledging node received the frame.
	Ack       bool `json:"a,omitempty"`
	Delivered int  `json:"c,omitempty"`
}

// privateDelivery is what the pieces of a frame make up.
type privateDelivery struct {
	User  uuid.UUID `json:"u"`
	Frame Frame     `json:"f"`
}

// assembly is a frame whose pieces are still arriving.
type assembly struct {
	pieces  [][]byte
	got     int
	started time.Time
}

// PostgresBroker announces over LISTEN/NOTIFY.
type PostgresBroker struct {
	pool *pgxpool.Pool
	// node is this process, so its own announcements can be ignored.
	node string
	log  *slog.Logger

	mu sync.Mutex
	// waiting holds, for each frame this node is whispering, where its
	// acknowledgements go.
	waiting map[string]chan int
	// partial holds frames from other nodes still being put together.
	partial map[string]*assembly
}

// NewPostgresBroker builds a broker over an existing pool.
func NewPostgresBroker(pool *pgxpool.Pool, node string, log *slog.Logger) *PostgresBroker {
	return &PostgresBroker{
		pool:    pool,
		node:    node,
		log:     log,
		waiting: make(map[string]chan int),
		partial: make(map[string]*assembly),
	}
}

// Whisper sends a frame for one person to every other node, and counts the
// connections that acknowledge it until expect is reached or wait runs out.
func (b *PostgresBroker) Whisper(
	ctx context.Context,
	userID uuid.UUID,
	frame Frame,
	expect int,
	wait time.Duration,
) (int, error) {
	if expect <= 0 {
		return 0, nil
	}

	encoded, err := json.Marshal(privateDelivery{User: userID, Frame: frame})
	if err != nil {
		return 0, fmt.Errorf("encode the private frame: %w", err)
	}
	if len(encoded) > maxPrivateFrame {
		return 0, fmt.Errorf("the private frame is %d bytes, more than is shared between nodes", len(encoded))
	}

	id := uuid.New().String()
	acknowledged := make(chan int, 64)
	b.mu.Lock()
	b.waiting[id] = acknowledged
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
	}()

	var pieces []string
	for start := 0; start < len(encoded); start += privatePieceSize {
		end := min(start+privatePieceSize, len(encoded))
		pieces = append(pieces, base64.StdEncoding.EncodeToString(encoded[start:end]))
	}

	// One transaction, so each listener receives every piece, together and in
	// order, or none of them.
	err = pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		for part, data := range pieces {
			payload, err := json.Marshal(privatePiece{
				ID: id, Origin: b.node, User: userID, Part: part, Of: len(pieces), Data: data,
			})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", privateChannel, string(payload)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("send the private frame: %w", err)
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	delivered := 0
	for delivered < expect {
		select {
		case count := <-acknowledged:
			delivered += count
		case <-timer.C:
			return delivered, nil
		case <-ctx.Done():
			return delivered, nil
		}
	}
	return delivered, nil
}

// receivePrivate handles one notification on the private channel.
func (b *PostgresBroker) receivePrivate(payload string, receive Receivers) {
	var piece privatePiece
	if err := json.Unmarshal([]byte(payload), &piece); err != nil {
		b.log.Warn("a private notification could not be read", slog.String("error", err.Error()))
		return
	}
	// This node's own pieces, and nothing it acknowledged itself.
	if piece.Origin == b.node {
		return
	}

	if piece.Ack {
		b.mu.Lock()
		acknowledged, waiting := b.waiting[piece.ID]
		b.mu.Unlock()
		if waiting {
			select {
			case acknowledged <- piece.Delivered:
			default:
			}
		}
		return
	}

	if receive.Private == nil || piece.Of <= 0 || piece.Of > maxPieces ||
		piece.Part < 0 || piece.Part >= piece.Of {
		return
	}
	if receive.Holds != nil && !receive.Holds(piece.User) {
		return
	}
	data, err := base64.StdEncoding.DecodeString(piece.Data)
	if err != nil {
		b.log.Warn("a piece of a private frame could not be read", slog.String("error", err.Error()))
		return
	}

	encoded, complete := b.assemble(piece, data)
	if !complete {
		return
	}

	var delivery privateDelivery
	if err := json.Unmarshal(encoded, &delivery); err != nil {
		b.log.Warn("a private frame could not be read", slog.String("error", err.Error()))
		return
	}
	// The frame goes to the person its pieces were addressed to, or nowhere.
	if delivery.User != piece.User {
		return
	}

	delivered := receive.Private(delivery.User, delivery.Frame)
	if delivered == 0 {
		// Nobody here: saying so would only be noise the sender ignores.
		return
	}

	ack, err := json.Marshal(privatePiece{ID: piece.ID, Origin: b.node, Ack: true, Delivered: delivered})
	if err != nil {
		return
	}
	// Off the listening loop, so a slow write cannot hold up what is behind it.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := b.pool.Exec(ctx, "SELECT pg_notify($1, $2)", privateChannel, string(ack)); err != nil {
			b.log.Warn("could not acknowledge a private frame", slog.String("error", err.Error()))
		}
	}()
}

// assemble adds a piece to its frame, returning the frame once every piece is in.
func (b *PostgresBroker) assemble(piece privatePiece, data []byte) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	for id, waiting := range b.partial {
		if now.Sub(waiting.started) > assemblyLimit {
			delete(b.partial, id)
		}
	}

	if piece.Of == 1 {
		return data, true
	}

	waiting, ok := b.partial[piece.ID]
	if !ok {
		if len(b.partial) >= maxAssemblies {
			return nil, false
		}
		waiting = &assembly{pieces: make([][]byte, piece.Of), started: now}
		b.partial[piece.ID] = waiting
	}
	if len(waiting.pieces) != piece.Of || waiting.pieces[piece.Part] != nil {
		return nil, false
	}
	waiting.pieces[piece.Part] = data
	waiting.got++
	if waiting.got < piece.Of {
		return nil, false
	}

	delete(b.partial, piece.ID)
	var whole []byte
	for _, part := range waiting.pieces {
		whole = append(whole, part...)
	}
	return whole, true
}

// Announce tells every listening node about an event.
func (b *PostgresBroker) Announce(ctx context.Context, announcement Announcement) error {
	announcement.Origin = b.node

	encoded, err := json.Marshal(announcement)
	if err != nil {
		return fmt.Errorf("encode the announcement: %w", err)
	}

	/*
		Refused here rather than truncated by Postgres.

		The payload carries three identifiers and so cannot approach the limit;
		checking anyway means that if it ever does — a field added later, an
		identifier that grew — it fails where somebody can read why, rather
		than as a database error during a message send.
	*/
	if len(encoded) >= 8000 {
		return fmt.Errorf("the announcement is %d bytes, which NOTIFY cannot carry", len(encoded))
	}

	if _, err := b.pool.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, string(encoded)); err != nil {
		return fmt.Errorf("announce the event: %w", err)
	}
	return nil
}

/*
Listen delivers announcements from other nodes until the context ends.

A connection of its own, held for the life of the process and never returned to
the pool: a listening connection is one Postgres will not let anybody else use,
and borrowing one from the pool would quietly remove it from circulation.
*/
func (b *PostgresBroker) Listen(ctx context.Context, receive Receivers) error {
	for {
		if err := b.listenOnce(ctx, receive); err != nil {
			if ctx.Err() != nil {
				// Shutting down, which is not a failure.
				return nil
			}
			/*
				Logged and retried.

				A dropped listener degrades live delivery on this node until it
				is back — clients still receive everything on reconnection — so
				stopping would be worse than trying again.
			*/
			b.log.Warn("the event listener dropped; reconnecting",
				slog.String("error", err.Error()))
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectDelay):
		}
	}
}

// listenOnce holds one connection and reads from it until something goes wrong.
func (b *PostgresBroker) listenOnce(ctx context.Context, receive Receivers) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire a connection to listen on: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if _, err := conn.Exec(ctx, "LISTEN "+privateChannel); err != nil {
		return fmt.Errorf("listen for private frames: %w", err)
	}

	b.log.Info("listening for events from other nodes", slog.String("node", b.node))

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("waiting for an event: %w", err)
		}

		if notification.Channel == privateChannel {
			b.receivePrivate(notification.Payload, receive)
			continue
		}

		var announcement Announcement
		if err := json.Unmarshal([]byte(notification.Payload), &announcement); err != nil {
			// A payload this cannot read is not worth stopping for: another
			// node's newer version may say something this one does not know.
			b.log.Warn("an announcement could not be read",
				slog.String("error", err.Error()))
			continue
		}

		/*
			This node's own announcement, ignored.

			NOTIFY reaches every listener including the one that sent it, and
			the sender has already delivered to its own connections — doing it
			again would show everybody on this node the same message twice.
		*/
		if announcement.Origin == b.node {
			continue
		}

		receive.Announcement(announcement)
	}
}

// Close does nothing: the connection is released when Listen returns, and the
// pool belongs to the caller.
func (b *PostgresBroker) Close() error { return nil }
