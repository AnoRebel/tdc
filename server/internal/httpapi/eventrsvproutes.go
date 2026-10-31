package httpapi

import (
	"net/http"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Answering events, who is going, and invitations.
*/
func init() {
	registerRoutes(func(s *Server) {
		if !s.ensureCalendar() {
			return
		}
		group := s.echo.Group("/api/events/:eventId", requireAuthentication, s.requireFeature(settings.FeatureEvents))
		group.PUT("/rsvp", s.handleAnswerEvent)
		group.GET("/attendees", s.handleEventAttendees)
		group.GET("/invites", s.handleListEventInvites)
		group.POST("/invites", s.handleInviteToEvent)
		group.DELETE("/invites/:inviteId", s.handleUninvite)
	})

	documentRoutes(map[string]entry{
		"PUT /api/events/:eventId/rsvp":                 {Summary: "Answer an event: going, maybe or declined", Authentication: "session or token"},
		"GET /api/events/:eventId/attendees":            {Summary: "Who answered an event, as you may be shown them", Authentication: "session or token"},
		"GET /api/events/:eventId/invites":              {Summary: "Who an event's organisers invited", Authentication: "session or token", Permission: "its creator, a host, or event.manage.any"},
		"POST /api/events/:eventId/invites":             {Summary: "Invite people or channels to an event", Authentication: "session or token", Permission: "its creator, a host, or event.manage.any"},
		"DELETE /api/events/:eventId/invites/:inviteId": {Summary: "Withdraw an invitation", Authentication: "session or token", Permission: "its creator, a host, or event.manage.any"},
	})
}

type answerView struct {
	Status string `json:"status"`
	// Position is a waiting person's place, from one.
	Position int64 `json:"position,omitempty"`
}

func (s *Server) handleAnswerEvent(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	var request struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	answered, err := s.calendar.Answer(ctx, user, viewer, id, request.Status)
	if err != nil {
		return eventError(err)
	}
	if answered.Log != nil {
		s.publishEvent(ctx, *answered.Log)
	}
	if len(answered.Promoted) > 0 {
		if event, err := s.calendar.Queries().GetCalendarEvent(ctx, id); err == nil {
			s.tellPromoted(ctx, event, answered.Promoted)
		}
	}
	return c.JSON(http.StatusOK, answerView{Status: answered.Status, Position: answered.Position})
}

type attendeeView struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Address     string `json:"address"`
	Status      string `json:"status"`
}

/*
handleEventAttendees lists who answered. People who keep their attendance to
themselves are counted, by answer, in "unnamed" rather than named — except to
the event's organisers, who see everybody.
*/
func (s *Server) handleEventAttendees(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	event, err := s.calendar.Get(ctx, viewer, id)
	if err != nil {
		return eventError(err)
	}
	attendance, err := s.calendar.Attendees(ctx, viewer, event.CalendarEvent)
	if err != nil {
		return err
	}
	named := make([]attendeeView, 0, len(attendance.Named))
	for _, person := range attendance.Named {
		named = append(named, attendeeView{
			UserID: person.UserID.String(), Username: person.Username, DisplayName: person.DisplayName,
			Address: s.rooms.UserAddress(person.Username), Status: person.Status,
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"attendees": named, "unnamed": attendance.Unnamed})
}

type inviteView struct {
	ID       string `json:"id"`
	UserID   string `json:"userId,omitempty"`
	Username string `json:"username,omitempty"`
	// DisplayName is the invited person's display name, empty when they have
	// not set one.
	DisplayName string `json:"displayName,omitempty"`
	ChannelID   string `json:"channelId,omitempty"`
	Channel     string `json:"channel,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

func (s *Server) handleListEventInvites(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	rows, err := s.calendar.Invitations(ctx, viewer, id)
	if err != nil {
		return eventError(err)
	}
	views := make([]inviteView, 0, len(rows))
	for _, row := range rows {
		view := inviteView{
			ID: row.ID.String(), Username: row.Username, DisplayName: row.DisplayName,
			Channel: row.ChannelName, CreatedAt: rfc3339(row.CreatedAt),
		}
		if row.UserID != nil {
			view.UserID = row.UserID.String()
		}
		if row.ChannelID != nil {
			view.ChannelID = row.ChannelID.String()
		}
		views = append(views, view)
	}
	return c.JSON(http.StatusOK, views)
}

/*
handleInviteToEvent invites people and channels by id. Each person invited is
told; an invited channel's members are told too, unless they could already see
the event and so have no news in an invitation.
*/
func (s *Server) handleInviteToEvent(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	var request struct {
		Users    []string `json:"users"`
		Channels []string `json:"channels"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	people, err := parseIDs(&request.Users)
	if err != nil {
		return err
	}
	channels, err := parseIDs(&request.Channels)
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	invited, err := s.calendar.Invite(ctx, user, viewer, id, *people, *channels)
	if err != nil {
		return eventError(err)
	}
	event, err := s.calendar.Get(ctx, viewer, id)
	if err != nil {
		return eventError(err)
	}
	s.recordManaged(c, viewer, event.CalendarEvent, audit.ActionEventManaged, map[string]any{"invited": len(invited.Invites)})

	told := append([]uuid.UUID{}, invited.People...)
	if event.Visibility == "invite" {
		for _, channel := range invited.Channels {
			members, err := s.rooms.MemberIDs(ctx, channel)
			if err == nil {
				told = append(told, members...)
			}
		}
	}
	s.tell(ctx, eventNotice{
		event: event.CalendarEvent, reason: notify.ReasonEventInvite, entry: s.anchorOf(ctx, id),
		actorID: user.ID, actorAddress: s.rooms.UserAddress(user.Username),
		pushTitle: identity.NameOf(user.Username, user.DisplayName) + " invited you", pushBody: event.Title + " — " + whenIn(event.StartsAt, event.TimeZone),
	}, uniqueIDs(told))
	return c.JSON(http.StatusOK, map[string]any{"invited": len(invited.Invites)})
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) handleUninvite(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	inviteID, err := uuid.Parse(c.Param("inviteId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such invitation")
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	if err := s.calendar.Uninvite(ctx, viewer, id, inviteID); err != nil {
		return eventError(err)
	}
	return c.NoContent(http.StatusNoContent)
}
