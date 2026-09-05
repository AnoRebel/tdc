package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// reactionResponse is one emoji on one message.
type reactionResponse struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	// Users are the addresses of everybody who reacted, so a client can show
	// who without a second request.
	Users []string `json:"users"`
	// Mine lets the button show that it is already pressed.
	Mine bool `json:"mine"`
}

type reactionRequest struct {
	Emoji string `json:"emoji"`
}

func (s *Server) handleReact(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request reactionRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	err = s.rooms.React(c.Request().Context(), roomID, user.ID, eventIDParam(c), request.Emoji)
	if errors.Is(err, rooms.ErrNotAnEmoji) {
		return echo.NewHTTPError(http.StatusBadRequest, "a reaction must be a single emoji")
	}
	if err != nil {
		return roomError(err)
	}

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleUnreact(c *echo.Context) error {
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// The emoji is in the query rather than the body: this is a DELETE, and a
	// body on one is widely ignored by proxies and client libraries.
	emoji := c.QueryParam("emoji")
	if emoji == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "which reaction to remove")
	}

	if err := s.rooms.Unreact(
		c.Request().Context(), roomID, user.ID, eventIDParam(c), emoji,
	); err != nil {
		return roomError(err)
	}

	return c.NoContent(http.StatusNoContent)
}

/*
handleRoomReactions returns every reaction in a room's timeline at once.

Per room rather than per message, for the same reason thread counts are: a
timeline draws every message it has, and a request each would be one request
per message.
*/
func (s *Server) handleRoomReactions(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if _, err := s.rooms.Visible(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	events, err := s.rooms.Timeline(ctx, roomID, user.ID, 0)
	if err != nil {
		return roomError(err)
	}

	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}

	byEvent, err := s.rooms.ReactionsFor(ctx, ids, user.ID)
	if err != nil {
		return err
	}

	response := make(map[string][]reactionResponse, len(byEvent))
	for eventID, reactions := range byEvent {
		rendered := make([]reactionResponse, 0, len(reactions))
		for _, reaction := range reactions {
			rendered = append(rendered, reactionResponse{
				Emoji: reaction.Emoji,
				Count: len(reaction.Users),
				Users: reaction.Users,
				Mine:  reaction.Mine,
			})
		}
		response[eventID] = rendered
	}

	return c.JSON(http.StatusOK, response)
}
