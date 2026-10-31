package httpapi

import (
	"context"
	"net/http"
	"strings"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/moderation"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Applying the operator's content filter to a message being sent.

The filter itself lives in the moderation package and knows nothing about
requests. This is where its verdict becomes an HTTP outcome, and where a
flagged message reaches the moderation queue.
*/

// filterOutcome is what the filter decided about a message.
type filterOutcome struct {
	// Body is what should actually be stored — the original, or the masked
	// version when the action is masking.
	Body string
	// Flagged is set when a moderator should see this after it is delivered.
	Flagged bool
	// Matched are the words that hit. Shown to moderators, never to the
	// author.
	Matched []string
}

/*
applyContentFilter checks a message and says what to do with it.

Returns an error only for blocking, which is the one outcome that stops the
message. Flagging and masking both deliver, so they come back in the outcome
rather than as failures.

The filter is rebuilt from the setting on each call rather than held. That is a
regexp compilation per message, which is real work — but the list is small, the
alternative is a cache that has to be invalidated from a settings write in
another process, and a filter that silently keeps applying a list an operator
deleted is worse than one that costs a millisecond.
*/
func (s *Server) applyContentFilter(ctx context.Context, body string) (filterOutcome, error) {
	outcome := filterOutcome{Body: body}

	if s.settings == nil || strings.TrimSpace(body) == "" {
		return outcome, nil
	}

	words := splitList(s.settings.Get(ctx, settings.FilterWords))
	if len(words) == 0 {
		return outcome, nil
	}

	match, matched := moderation.NewFilter(words).Check(body)
	if !matched {
		return outcome, nil
	}

	action := moderation.Action(s.settings.Get(ctx, settings.FilterAction))
	if !moderation.KnownAction(action) {
		// A value the server does not recognise falls back to the gentlest
		// action rather than the harshest: a typo in a setting should not start
		// refusing everybody's messages.
		action = moderation.ActionFlag
	}

	switch action {
	case moderation.ActionBlock:
		/*
			Deliberately vague. Naming the word that matched would turn the
			endpoint into an oracle: send a message per candidate word and read
			the refusals back to recover the operator's whole list, which is
			exactly what somebody working around a filter wants.
		*/
		return outcome, echo.NewHTTPError(http.StatusUnprocessableEntity,
			"this message was refused by this server's content policy")

	case moderation.ActionMask:
		outcome.Body = match.Masked
		// Masked messages are flagged too. Somebody should see what the filter
		// decided to alter, and masking silently would mean nobody ever
		// discovers the list is wrong.
		outcome.Flagged = true
		outcome.Matched = match.Matched

	case moderation.ActionFlag:
		outcome.Flagged = true
		outcome.Matched = match.Matched
	}

	return outcome, nil
}

// splitList reads a comma-separated setting.
func splitList(value string) []string {
	parts := strings.Split(value, ",")

	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

/*
flagForReview puts a filtered message in the moderation queue.

Filed as a report with no reporter, which is what the reporter-non-disclosure
rule requires anyway: there is nobody to disclose. The queue a moderator reads
is the same one people's own reports arrive in, deliberately — a second queue
is a queue somebody forgets to read.

Run detached from the request. The message has already been delivered by this
point, and a moderation queue that failed to accept an entry must not turn a
successful send into an error the author sees.
*/
func (s *Server) flagForReview(
	c *echo.Context,
	roomID uuid.UUID,
	eventID string,
	author string,
	matched []string,
	body string,
) {
	detail := "Matched this server's content filter: " + strings.Join(matched, ", ")

	s.inBackground(c.Request().Context(), "flag filtered message", func(ctx context.Context) {
		if _, err := s.moderation.FileAutomatically(ctx, moderation.Filing{
			RoomID:       &roomID,
			EventID:      eventID,
			Subject:      author,
			Reason:       moderation.ReasonOther,
			Detail:       detail,
			ReportedBody: body,
		}); err != nil {
			s.log.Error("cannot flag a filtered message for review",
				"room", roomID, "event", eventID, "error", err)
		}
	})
}
