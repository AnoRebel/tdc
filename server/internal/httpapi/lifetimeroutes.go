package httpapi

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/moderation"
)

/*
Giving an individual message a lifetime.

Independent of whether the conversation itself is disposable: somebody can put
an hour on one message in a channel that keeps everything, which is the case
this exists for.
*/

type setLifetimeRequest struct {
	// Kind is "after_sending", "once_seen" or "after_seeing".
	Kind string `json:"kind"`
	// Seconds is the duration, for the kinds that use one.
	Seconds int32 `json:"seconds,omitempty"`
}

func (s *Server) handleSetMessageLifetime(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request setLifetimeRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	event, err := s.rooms.EventInRoom(ctx, roomID, c.Param("eventId"))
	if err != nil {
		return roomError(err)
	}

	/*
		Its sender, or a moderator.

		Anybody else could otherwise put a lifetime on somebody's message and
		have it disappear, which is deletion by another name — and the rule
		against extending would not help, since there was nothing to extend.
	*/
	mine := event.SenderID != nil && *event.SenderID == user.ID
	moderator := currentPermissions(c).AllowsServer(authz.PermModerationReview).Allowed
	if !mine && !moderator {
		return echo.NewHTTPError(http.StatusForbidden,
			"only whoever sent this message, or a moderator, may set when it expires")
	}

	lifetime, err := s.moderation.SetLifetime(ctx, event.ID, roomID, user.ID,
		moderation.LifetimeKind(request.Kind), request.Seconds,
		time.UnixMilli(event.CreatedAt))

	switch {
	case errors.Is(err, moderation.ErrUnknownLifetime),
		errors.Is(err, moderation.ErrLifetimeNeedsDuration):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, moderation.ErrLifetimeExtended):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case err != nil:
		return err
	}

	// Recorded, because shortening somebody's message to nothing is a
	// moderator action when a moderator does it.
	if moderator && !mine {
		s.recordModeration(c, user, audit.ActionMessageLifetimeSet,
			"event", event.ID, map[string]any{
				"kind": request.Kind, "seconds": request.Seconds,
			})
	}

	return c.JSON(http.StatusOK, lifetimeResponseOf(lifetime))
}

func lifetimeResponseOf(lifetime moderation.Lifetime) lifetimeResponse {
	response := lifetimeResponse{
		Kind:    string(lifetime.Kind),
		Seconds: lifetime.Seconds,
	}
	if !lifetime.ExpiresAt.IsZero() {
		when := lifetime.ExpiresAt
		response.ExpiresAt = &when
	}
	return response
}

// handleLifetimeKinds lists the ways a message can be made to expire, so a
// client does not hard-code a list that can drift from what the server accepts.
func (s *Server) handleLifetimeKinds(c *echo.Context) error {
	kinds := moderation.LifetimeKinds()

	response := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		response = append(response, string(kind))
	}
	return c.JSON(http.StatusOK, response)
}
