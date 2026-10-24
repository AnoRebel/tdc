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
	// SenderName and ActorName are what to call those two people: a display
	// name, or a username without one. Absent for somebody on another server,
	// whom a client calls by their address.
	SenderName string `json:"senderName,omitempty"`
	ActorName  string `json:"actorName,omitempty"`
	// Detail is what the reason needs — the emoji, for a reaction.
	Detail string `json:"detail,omitempty"`
	// Count is how many unread notifications this entry stands for, when the
	// list is grouped. One means it stands only for itself.
	Count int64 `json:"count,omitempty"`
	// Actors is how many different people are behind that count, which
	// separates "five from Amina" from "five people replied".
	Actors int64 `json:"actors,omitempty"`
}

/*
handleListNotifications returns what somebody has not yet seen.

Grouped by conversation unless asked otherwise: ten messages in a busy channel
is one thing worth knowing about, and listing each separately buries every
other conversation under the noisiest one — which is what makes people stop
reading the list at all.

The ungrouped form stays available, because a client showing a conversation's
own history wants every entry rather than a summary of them.
*/
func (s *Server) handleListNotifications(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	limit := 0
	if raw := c.QueryParam("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}

	var notifications []notify.Notification
	var err error

	if c.QueryParam("grouped") == "false" {
		notifications, err = s.notify.Unread(ctx, user.ID, int32(limit))
	} else {
		notifications, err = s.notify.Grouped(ctx, user.ID)
	}
	if err != nil {
		return err
	}

	response := make([]notificationResponse, 0, len(notifications))
	names := newNameBook(s.rooms)
	for _, n := range notifications {
		response = append(response, notificationResponse{
			ID:         n.ID.String(),
			RoomID:     n.RoomID.String(),
			EventID:    n.EventID,
			Reason:     string(n.Reason),
			Sender:     n.Sender,
			Preview:    n.Preview,
			Actor:      n.Actor,
			Detail:     n.Detail,
			SenderName: names.at(ctx, n.Sender),
			ActorName:  names.at(ctx, n.Actor),
			Count:      n.Count,
			Actors:     n.Actors,
		})
	}

	return c.JSON(http.StatusOK, response)
}

/*
handleDismissRoomNotifications withdraws everything unread in a conversation.

What dismissing a grouped entry means. The summary stands for several
notifications, so clearing it has to clear what it stood for — otherwise the
group reappears the moment the list is redrawn.
*/
func (s *Server) handleDismissRoomNotifications(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// Scoped to this person, so an identifier belonging to a conversation they
	// are not in dismisses nothing rather than somebody else's notifications.
	if err := s.notify.DismissRoom(c.Request().Context(), user.ID, roomID); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
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

// levelResponse is one notification setting.
type levelResponse struct {
	// Level is "all", "mentions" or "none".
	Level string `json:"level"`
	Muted bool   `json:"muted"`
}

// preferencesResponse is somebody's default and each conversation set apart.
type preferencesResponse struct {
	Default levelResponse `json:"default"`
	// Rooms holds only the conversations with a setting of their own; every
	// other one follows Default.
	Rooms map[string]levelResponse `json:"rooms"`
}

func (s *Server) handleGetNotificationPreferences(c *echo.Context) error {
	user, _ := currentUser(c)

	global, rooms, err := s.notify.Preferences(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := preferencesResponse{
		Default: levelResponse{Level: string(global.Level), Muted: global.Muted},
		Rooms:   make(map[string]levelResponse, len(rooms)),
	}
	for roomID, preference := range rooms {
		response.Rooms[roomID.String()] = levelResponse{
			Level: string(preference.Level),
			Muted: preference.Muted,
		}
	}
	return c.JSON(http.StatusOK, response)
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
