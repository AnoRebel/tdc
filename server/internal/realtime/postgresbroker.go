package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

// PostgresBroker announces over LISTEN/NOTIFY.
type PostgresBroker struct {
	pool *pgxpool.Pool
	// node is this process, so its own announcements can be ignored.
	node string
	log  *slog.Logger
}

// NewPostgresBroker builds a broker over an existing pool.
func NewPostgresBroker(pool *pgxpool.Pool, node string, log *slog.Logger) *PostgresBroker {
	return &PostgresBroker{pool: pool, node: node, log: log}
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
func (b *PostgresBroker) Listen(ctx context.Context, handle func(Announcement)) error {
	for {
		if err := b.listenOnce(ctx, handle); err != nil {
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
func (b *PostgresBroker) listenOnce(ctx context.Context, handle func(Announcement)) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire a connection to listen on: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return fmt.Errorf("listen: %w", err)
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

		handle(announcement)
	}
}

// Close does nothing: the connection is released when Listen returns, and the
// pool belongs to the caller.
func (b *PostgresBroker) Close() error { return nil }
