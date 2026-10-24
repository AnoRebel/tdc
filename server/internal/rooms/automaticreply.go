package rooms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// ViaAutomaticReply is the "via" kind of a reply the server posted on
// somebody's behalf while they were away.
const ViaAutomaticReply = "automatic"

/*
PostAutomaticReply posts somebody's automatic reply in a conversation, as them
and marked as not typed by them.

Sent as the person, because it is their words and their conversation; marked
with a "via" in the content, exactly as a program's message is, so every client,
export and bot can tell it was not the person typing just now. The mark is set
here and nowhere a caller can reach.

threadRoot puts it in a thread rather than the conversation itself, which is
where an answer to a mention goes.
*/
func (s *Service) PostAutomaticReply(
	ctx context.Context,
	room dbgen.Room,
	person dbgen.User,
	body string,
	threadRoot *string,
) (dbgen.Event, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return dbgen.Event{}, ErrEmptyMessage
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	payload := map[string]any{
		"body": trimmed,
		"via":  Via{Kind: ViaAutomaticReply, ID: person.ID.String(), Name: person.DisplayName},
	}
	if threadRoot != nil {
		payload["threadRoot"] = *threadRoot
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode automatic reply: %w", err)
	}

	if threadRoot != nil {
		return s.appendIn(ctx, room, person, events.TypeMessageSent, content, "", threadRoot)
	}
	return s.append(ctx, room, person, events.TypeMessageSent, content, "")
}
