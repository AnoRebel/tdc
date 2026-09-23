package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/events"
)

// ErrOriginNotFound refuses an issue made from a message the maker cannot
// see, or one that is not a message at all. The same answer for both, so
// filing an issue cannot be used to learn whether a message exists.
var ErrOriginNotFound = errors.New("no such message")

/*
checkOrigin confirms somebody may refer to a message, and returns the
reference to record.

They must be able to read the room it is in — referring to a message is a way
of pointing other people at it, and nobody should be able to point at what they
cannot see themselves. Only messages qualify: an issue made from a join or a
rename would refer to nothing anybody said.
*/
func (s *Service) checkOrigin(ctx context.Context, actor Actor, origin Origin) (map[string]any, error) {
	room, err := s.rooms.Visible(ctx, origin.RoomID, actor.ID)
	if err != nil {
		return nil, ErrOriginNotFound
	}

	event, err := s.queries.GetEventByID(ctx, origin.EventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOriginNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read origin: %w", err)
	}
	if event.RoomID != room.ID || events.Type(event.Type) != events.TypeMessageSent || event.RedactedAt != nil {
		return nil, ErrOriginNotFound
	}

	return map[string]any{
		"originChannel": room.ID.String(),
		"originEvent":   event.ID,
	}, nil
}
