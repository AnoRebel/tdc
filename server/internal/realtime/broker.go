package realtime

import (
	"context"

	"github.com/google/uuid"
)

/*
Getting an event to connections this process is not holding.

One node needs nothing: a message is stored, and the connections that should see
it are in the same memory as the code that stored it. Two nodes is a different
problem — somebody connected to the second node hears nothing about a message
sent through the first, and the only sign is that the conversation appears to
have stopped until they reload.

So a broker, which one node tells about an event and every node hears. Behind an
interface because the single-node deployment must not need one, and because
Postgres is the implementation that costs nothing to run rather than the only
one imaginable.
*/

/*
Announcement is what one node tells the others.

The event's identity, not the event. Postgres's NOTIFY carries at most eight
thousand bytes, which a message with a long body and several attachments would
exceed — and a fanout that worked until somebody wrote a long enough message is
worse than one that never worked. Each node reads the event itself, which it
can, because the event is in the database they share.
*/
type Announcement struct {
	EventID string `json:"e"`
	RoomID  string `json:"r"`
	/*
		Origin is the node that published it.

		Carried so a node can ignore its own announcement: it has already
		delivered to its own connections, and doing it twice would show
		everybody on that node the same message.
	*/
	Origin string `json:"o"`
}

// Broker carries announcements between nodes.
type Broker interface {
	/*
		Announce tells every node about an event, including this one.

		Errors are the caller's to log rather than to act on: the event is
		already stored, so a client that misses the live frame receives it on
		reconnection. A send must not fail because a fanout did.
	*/
	Announce(ctx context.Context, announcement Announcement) error

	/*
		Listen delivers announcements from other nodes until the context ends.

		Called once at startup. The handler is invoked for each announcement
		that did not originate here.
	*/
	Listen(ctx context.Context, handle func(Announcement)) error

	// Close releases whatever the broker holds.
	Close() error
}

/*
Alone is the broker for a server running by itself.

Not a null object that silently drops things — it delivers to this node, which
is every node there is. A single-node deployment is the ordinary one, and it
should not need a broker configured, a connection held open, or a round trip
through the database to show somebody their own message.
*/
type Alone struct{}

// Announce does nothing: the publisher has already delivered locally.
func (Alone) Announce(context.Context, Announcement) error { return nil }

// Listen blocks until the context ends, because nothing else will announce.
func (Alone) Listen(ctx context.Context, _ func(Announcement)) error {
	<-ctx.Done()
	return ctx.Err()
}

// Close does nothing.
func (Alone) Close() error { return nil }

// NodeID identifies this process among the nodes sharing a database.
//
// Fresh each start, because what it is for is telling this node's own
// announcements apart from everybody else's — and a node that restarted is, for
// that purpose, a different node.
func NodeID() string { return uuid.NewString() }
