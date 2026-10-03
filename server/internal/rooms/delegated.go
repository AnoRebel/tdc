package rooms

import (
	"context"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
AccessDecider answers, for a room of one kind, whether somebody may see it.

For rooms that belong to something else. An issue's discussion is a room, and
who may read it is not a fact about the room at all: it is whoever may see the
issue, which the project decides. Copying the project's membership into every
discussion would be a second list to keep in step with the first, and the day
they disagreed somebody removed from a private project would still be reading
its issues' comments.

So the room asks. A room whose kind has a decider is, to anybody the decider
admits, what a public channel is to everybody on the server: readable, and
joinable, with posting still needing membership. To anybody it does not admit,
the room does not exist.
*/
type AccessDecider func(ctx context.Context, room dbgen.Room, userID uuid.UUID) (bool, error)

/*
DelegateAccess hands the question of who may see rooms of a kind to decide.

Called once while the server is being built, before it serves anything; the
table is read without a lock on every request after that.
*/
func (s *Service) DelegateAccess(kind string, decide AccessDecider) {
	if s.deciders == nil {
		s.deciders = make(map[string]AccessDecider)
	}
	s.deciders[kind] = decide
}

/*
ServerName is this server's own name, as it signs events and names rooms.

For a package that creates rooms of its own inside a transaction of its own —
projects do, for their streams and their issues' discussions — and must give
them the same home server a room created here would have.
*/
func (s *Service) ServerName() string { return s.serverName }

// delegated reports whether a room's visibility is decided elsewhere, and if
// so what was decided.
func (s *Service) delegated(ctx context.Context, room dbgen.Room, userID uuid.UUID) (handled, visible bool, err error) {
	decide, ok := s.deciders[room.Kind]
	if !ok {
		return false, false, nil
	}
	visible, err = decide(ctx, room, userID)
	return true, visible, err
}
