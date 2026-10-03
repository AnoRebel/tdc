package realtime

import (
	"log/slog"

	"github.com/google/uuid"
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

Only this node's connections. On a server running several nodes, a person
connected to another node does not receive it — a private answer is
deliberately never written anywhere another node could read it from, which is
the cost of that. The count lets the caller say so rather than claim delivery.
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
