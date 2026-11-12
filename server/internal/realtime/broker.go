package realtime

import (
	"context"
	"time"
	"uuid"
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

/*
Receivers is what a node does with what other nodes send it.

Announcement is required. Private is optional: a node given none ignores
private frames, which is the right answer for anything that holds no
connections.
*/
type Receivers struct {
	// Announcement handles an event another node stored.
	Announcement func(Announcement)
	// Private delivers a frame meant for one person to their connections on
	// this node, and reports how many received it.
	Private func(userID uuid.UUID, frame Frame) int
	// Holds says whether this node has any connection for a person. Asked of
	// every piece before anything is decoded or kept, so a node pays almost
	// nothing for the private answers of people connected elsewhere. Nil is
	// taken as yes.
	Holds func(userID uuid.UUID) bool
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
	Listen(ctx context.Context, receive Receivers) error

	/*
		Whisper delivers a frame meant for one person to their connections on
		the other nodes, and reports how many received it.

		Never written anywhere: it travels in the notification itself, which
		Postgres holds in memory until every listener has read it, and is in
		no table, no log and no backup. expect is how many connections the
		person is believed to hold elsewhere; the answer comes back as soon as
		that many have acknowledged it, and otherwise after wait, with however
		many had.
	*/
	Whisper(ctx context.Context, userID uuid.UUID, frame Frame, expect int, wait time.Duration) (int, error)

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
func (Alone) Listen(ctx context.Context, _ Receivers) error {
	<-ctx.Done()
	return ctx.Err()
}

// Whisper reaches nobody, because there is no other node to reach.
func (Alone) Whisper(context.Context, uuid.UUID, Frame, int, time.Duration) (int, error) {
	return 0, nil
}

// Close does nothing.
func (Alone) Close() error { return nil }

// NodeID identifies this process among the nodes sharing a database.
//
// Fresh each start, because what it is for is telling this node's own
// announcements apart from everybody else's — and a node that restarted is, for
// that purpose, a different node.
func NodeID() string { return uuid.New().String() }
