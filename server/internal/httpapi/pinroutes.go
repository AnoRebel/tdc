package httpapi

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/notify"
)

// pinnedResponse is a message a room has marked as important.
type pinnedResponse struct {
	Message  messageResponse `json:"message"`
	PinnedBy string          `json:"pinnedBy"`
	PinnedAt int64           `json:"pinnedAt"`
}

func (s *Server) handlePinMessage(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	ctx := c.Request().Context()
	eventID := eventIDParam(c)

	if err := s.rooms.PinMessage(ctx, roomID, user.ID, eventID); err != nil {
		return roomError(err)
	}

	// Whoever wrote it hears that the channel has singled it out.
	if s.notify != nil {
		if event, err := s.rooms.EventInRoom(ctx, roomID, eventID); err == nil && event.SenderID != nil {
			s.notify.About(ctx, *event.SenderID, user.ID,
				s.rooms.UserAddress(user.Username), roomID, eventID,
				notify.ReasonPin, "")
		}
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleUnpinMessage(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if err := s.rooms.UnpinMessage(
		c.Request().Context(), roomID, user.ID, eventIDParam(c),
	); err != nil {
		return roomError(err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleListPinned(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	pinned, err := s.rooms.PinnedMessages(c.Request().Context(), roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	response := make([]pinnedResponse, 0, len(pinned))
	names := newNameBook(s.rooms)
	for _, item := range pinned {
		message := messageResponseOf(item.Event)
		message.SenderName = names.sender(c.Request().Context(), item.Event)
		response = append(response, pinnedResponse{
			Message:  message,
			PinnedBy: item.PinnedBy,
			PinnedAt: item.PinnedAt,
		})
	}

	return c.JSON(http.StatusOK, response)
}

type pinRoomRequest struct {
	// Position lets somebody arrange their pinned conversations rather than
	// having them ordered by when each was pinned.
	Position int32 `json:"position"`
}

func (s *Server) handlePinRoom(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request pinRoomRequest
	// A body is optional here: pinning without saying where is ordinary.
	_ = c.Bind(&request)

	if err := s.rooms.PinRoom(c.Request().Context(), user.ID, roomID, request.Position); err != nil {
		return roomError(err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleUnpinRoom(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if err := s.rooms.UnpinRoom(c.Request().Context(), user.ID, roomID); err != nil {
		return roomError(err)
	}

	return c.NoContent(http.StatusNoContent)
}

// handleListPinnedRooms returns the conversations somebody keeps at the top.
func (s *Server) handleListPinnedRooms(c *echo.Context) error {
	user, _ := currentUser(c)

	ids, err := s.rooms.PinnedRooms(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make([]string, 0, len(ids))
	for _, id := range ids {
		response = append(response, id.String())
	}

	return c.JSON(http.StatusOK, response)
}
