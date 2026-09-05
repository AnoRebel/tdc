package httpapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/notify"
)

// notificationResponse is one thing somebody has been told about.
type notificationResponse struct {
	ID     string `json:"id"`
	RoomID string `json:"roomId"`
	// EventID lets a client jump to the message rather than only naming it.
	EventID string `json:"eventId"`
	// Reason is why this was raised — "mention", "direct", "thread",
	// "reaction", "reply", "join", "forward" or "pin" — so a client can phrase
	// and group them rather than showing one undifferentiated list.
	Reason  string `json:"reason"`
	Sender  string `json:"sender"`
	Preview string `json:"preview"`
	// Actor is who did it. The same as Sender for a message, and different for
	// everything else: a reaction's event is the message reacted to, whose
	// sender is usually the person being notified.
	Actor string `json:"actor"`
	// Detail is what the reason needs — the emoji, for a reaction.
	Detail string `json:"detail,omitempty"`
}

func (s *Server) handleListNotifications(c *echo.Context) error {
	user, _ := currentUser(c)

	limit := 0
	if raw := c.QueryParam("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}

	notifications, err := s.notify.Unread(c.Request().Context(), user.ID, int32(limit))
	if err != nil {
		return err
	}

	response := make([]notificationResponse, 0, len(notifications))
	for _, n := range notifications {
		response = append(response, notificationResponse{
			ID:      n.ID.String(),
			RoomID:  n.RoomID.String(),
			EventID: n.EventID,
			Reason:  string(n.Reason),
			Sender:  n.Sender,
			Preview: n.Preview,
			Actor:   n.Actor,
			Detail:  n.Detail,
		})
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleNotificationCount(c *echo.Context) error {
	user, _ := currentUser(c)

	count, err := s.notify.UnreadCount(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]int64{"unread": count})
}

func (s *Server) handleMarkNotificationRead(c *echo.Context) error {
	user, _ := currentUser(c)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such notification")
	}

	// Scoped to this person, so an identifier belonging to somebody else does
	// nothing rather than dismissing their notification.
	if err := s.notify.MarkRead(c.Request().Context(), user.ID, id); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

type preferenceRequest struct {
	// Level is "all", "mentions" or "none".
	Level string `json:"level"`
	Muted bool   `json:"muted"`
	// Room narrows this to one conversation. Absent sets the global default.
	Room string `json:"room,omitempty"`
}

func (s *Server) handleSetNotificationPreference(c *echo.Context) error {
	user, _ := currentUser(c)

	var request preferenceRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	level := notify.Level(request.Level)
	switch level {
	case notify.LevelAll, notify.LevelMentions, notify.LevelNone:
	default:
		return echo.NewHTTPError(http.StatusBadRequest,
			`level must be "all", "mentions" or "none"`)
	}

	var room *uuid.UUID
	if request.Room != "" {
		roomID, err := uuid.Parse(request.Room)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "that is not a room")
		}
		// Only for a room they are in: setting a preference on a channel you
		// cannot see would disclose that it exists.
		if _, err := s.rooms.RequireMembership(c.Request().Context(), roomID, user.ID); err != nil {
			return roomError(err)
		}
		room = &roomID
	}

	err := s.notify.SetPreference(c.Request().Context(), user.ID, room,
		notify.Preference{Level: level, Muted: request.Muted})
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
