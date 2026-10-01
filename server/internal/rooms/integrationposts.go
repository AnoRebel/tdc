package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/actions"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Messages posted by something that is not a person.

Every such message says so in its own content, under "via": what kind of thing
posted it and what it is called. In the content rather than beside it, because
the content is what is signed, stored and replicated — a mark kept anywhere else
is one a client, a federated peer or an export could lose, and then a program's
message would read as somebody's.

Set here, by the server, from how the message arrived. Nothing a caller sends
can put a "via" on a message, and nothing can take one off.
*/

// Via names the program a message came from.
type Via struct {
	// Kind is "webhook" or "bot".
	Kind string `json:"kind"`
	// ID identifies the webhook or application.
	ID string `json:"id"`
	// Name is what readers see in place of a person's name.
	Name string `json:"name"`
}

// ErrOnlyLinks reports a control that needs a program to answer it, on a
// message posted by something that cannot.
var ErrOnlyLinks = errors.New("a message posted through a webhook can only offer links")

/*
PostAs posts a message as a program that holds no account: an incoming webhook.

No membership is asked for — the webhook was bound to this channel when it was
made, and that binding is its whole authority. Everything else a message is
held to applies unchanged: the length, the controls, an archived channel.

Only link buttons may be offered. A press of anything else is delivered to the
message's sender, and a webhook is an address that receives nothing, so a
button that asked a question would be one nobody could ever answer.
*/
func (s *Service) PostAs(
	ctx context.Context,
	roomID uuid.UUID,
	via Via,
	localPart string,
	body string,
	offered []actions.Action,
) (dbgen.Event, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return dbgen.Event{}, ErrEmptyMessage
	}
	if len(trimmed) > MaxMessageLength {
		return dbgen.Event{}, fmt.Errorf("%w: limit is %d characters", ErrMessageTooLong, MaxMessageLength)
	}
	if err := actions.Check(offered); err != nil {
		return dbgen.Event{}, err
	}
	for _, action := range offered {
		if action.Kind != actions.KindButton || action.Capability != actions.CapabilityOpen {
			return dbgen.Event{}, fmt.Errorf("%w: %q asks for an answer", ErrOnlyLinks, action.Name)
		}
	}

	room, err := s.store.GetRoomByID(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read room: %w", err)
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	payload := map[string]any{"body": trimmed, "via": via}
	if len(offered) > 0 {
		payload["actions"] = offered
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode message: %w", err)
	}

	return s.appendAs(ctx, room, nil, s.UserAddress(localPart), events.TypeMessageSent, content, "")
}
