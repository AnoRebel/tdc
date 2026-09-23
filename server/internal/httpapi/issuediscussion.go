package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
Where conversation and work meet: an issue's discussion, and the messages
issues were made from.

A comment on an issue is an ordinary message in the issue's discussion room,
posted, edited, deleted and notified through the room endpoints exactly as a
channel message is. The only things added here are the two a tracker needs and
a channel does not: somebody named in a comment is brought into it, and
somebody who comments keeps hearing about it.
*/
func init() {
	beforeNotifying = append(beforeNotifying, func(s *Server, ctx context.Context, event dbgen.Event, body string, roomID uuid.UUID) {
		if s.projects == nil {
			return
		}
		room, err := s.queries.GetRoomByID(ctx, roomID)
		if err != nil {
			return
		}
		if _, ok := s.projects.IsDiscussion(ctx, room); !ok {
			return
		}

		// Whoever commented follows the discussion from now on.
		if event.SenderID != nil {
			if err := s.projects.Participate(ctx, roomID, *event.SenderID); err != nil {
				s.log.Warn("cannot make a commenter a participant", "roomId", roomID, "error", err)
			}
		}
		// And whoever was named, and may see the issue, is brought in to hear it.
		s.projects.AdmitMentioned(ctx, roomID, notify.MentionsIn(body))
	})

	registerRoutes(func(s *Server) {
		s.echo.GET("/api/rooms/:id/issues", s.handleIssuesFromRoom, requireAuthentication, s.requireProjects)
	})

	documentRoutes(map[string]entry{
		"GET /api/rooms/:id/issues": {Summary: "Issues made from a room's messages", Authentication: "session or token"},
	})
}

// issueFromMessage is one issue made from one of a room's messages.
type issueFromMessage struct {
	// EventID is the message it was made from.
	EventID string `json:"eventId"`
	// Key is the issue's identifier, WEB-12, which is also how to open it.
	Key   string `json:"key"`
	Title string `json:"title"`
	// State is the name of the state it is in, and whether that means done.
	State     string `json:"state"`
	Completed bool   `json:"completed"`
}

/*
handleIssuesFromRoom says which of a room's messages became issues.

Answered per reader. The room must be one they can see, and each issue one
they can see: a message that became an issue in a private project shows
nothing to a room member outside it, because a marker saying "an issue was
made from this" would itself tell them the private project exists.
*/
func (s *Server) handleIssuesFromRoom(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if _, err := s.rooms.Visible(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	rows, err := s.projects.IssuesFromRoom(ctx, roomID, user.ID)
	if err != nil {
		return err
	}
	response := make([]issueFromMessage, 0, len(rows))
	for _, row := range rows {
		response = append(response, issueFromMessage{
			EventID:   row.EventID,
			Key:       projects.Identifier(dbgen.Project{Key: row.ProjectKey}, dbgen.Issue{Number: row.Number}),
			Title:     row.Title,
			State:     row.StateName,
			Completed: row.IsCompletion,
		})
	}
	return c.JSON(http.StatusOK, response)
}
