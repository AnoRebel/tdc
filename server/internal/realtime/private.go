package realtime

import (
	"log/slog"
	"uuid"
)

/*
FramePrivate carries something meant for one person only — a program's private
answer to a button they pressed.

It never enters a room's log, which every member can read, and it is not
filtered out of a room's fanout per viewer either: it is sent to one person's
connections by who they are, so there is no path by which anybody else's
connection could be handed it.
*/
const FramePrivate = "private"

/*
SendToUser delivers a frame to every connection one person holds, and reports
how many received it.

Every connection rather than the one they pressed from: somebody who pressed on
their phone and picked up their laptop should find the answer there too.

Only this node's connections. The other nodes are reached through the
broker's Whisper, which carries the frame in the notification itself rather
than writing it anywhere.
*/
func (h *Hub) SendToUser(userID uuid.UUID, frame Frame) int {
	h.mu.RLock()
	targets := make([]*Connection, 0, len(h.byUser[userID]))
	for _, conn := range h.byUser[userID] {
		targets = append(targets, conn)
	}
	h.mu.RUnlock()

	delivered := 0
	for _, conn := range targets {
		if !conn.deliver(frame) {
			h.log.Warn("dropping a connection that cannot keep up",
				slog.String("connectionId", conn.ID.String()),
				slog.String("userId", conn.UserID.String()),
			)
			h.Remove(conn)
			continue
		}
		delivered++
	}
	return delivered
}

// Holds reports whether this node has any connection for a person, which is
// what lets a node ignore another's private frame without reading it.
func (h *Hub) Holds(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byUser[userID]) > 0
}
