package httpapi

import (
	"bytes"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/calendar"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Events out of the server, as iCalendar.

One event as a file; a person's own subscription address, which any calendar
application can follow; and, if the operator chooses, the server's public
events as a feed. The subscription address carries its secret, so it needs no
session — and is decided, at every fetch, for its owner as they are now.
*/
func init() {
	registerRoutes(func(s *Server) {
		if !s.ensureCalendar() {
			return
		}
		feature := s.requireFeature(settings.FeatureEvents)
		s.echo.GET("/api/events/:eventId/ics", s.handleEventICS, requireAuthentication, feature)

		me := s.echo.Group("/api/me/calendar-feed", requireAuthentication, feature)
		me.GET("", s.handleGetCalendarFeed)
		me.POST("", s.handleMakeCalendarFeed)
		me.DELETE("", s.handleRevokeCalendarFeed)

		s.echo.GET("/api/calendar/feeds/:secret/events.ics", s.handleCalendarFeed, feature)
		s.echo.GET("/api/calendar/public.ics", s.handlePublicCalendar, feature)
	})

	documentRoutes(map[string]entry{
		"GET /api/events/:eventId/ics":               {Summary: "An event as an iCalendar file", Authentication: "session or token"},
		"GET /api/me/calendar-feed":                  {Summary: "Whether you have a calendar subscription address, and when it was last used", Authentication: "session or token"},
		"POST /api/me/calendar-feed":                 {Summary: "Make or replace your calendar subscription address; its secret is shown once", Authentication: "session"},
		"DELETE /api/me/calendar-feed":               {Summary: "Revoke your calendar subscription address", Authentication: "session or token"},
		"GET /api/calendar/feeds/:secret/events.ics": {Summary: "A person's events as an iCalendar feed", Authentication: "the secret in the address"},
		"GET /api/calendar/public.ics":               {Summary: "The server's public events as an iCalendar feed, where the operator publishes it", Authentication: "none"},
	})
}

// eventLink is where an event's page is, for a calendar entry to point back.
func (s *Server) eventLink(id uuid.UUID) string {
	return s.appLink("/events/" + id.String())
}

func (s *Server) writeICS(c *echo.Context, name, filename string, events []calendar.Listed) error {
	var body bytes.Buffer
	if err := (calendar.Export{Name: name, Link: s.eventLink, Events: events, Now: time.Now()}).Write(&body); err != nil {
		return err
	}
	header := c.Response().Header()
	header.Set("Content-Type", "text/calendar; charset=utf-8")
	header.Set("Content-Disposition", `inline; filename="`+filename+`"`)
	// Never shared by a cache: what a feed holds depends on who it is for.
	header.Set("Cache-Control", "private, no-store")
	return c.Blob(http.StatusOK, "text/calendar; charset=utf-8", body.Bytes())
}

func (s *Server) handleEventICS(c *echo.Context) error {
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	event, err := s.calendar.Get(c.Request().Context(), viewer, id)
	if err != nil {
		return eventError(err)
	}
	return s.writeICS(c, event.Title, "event.ics", []calendar.Listed{event})
}

type calendarFeedView struct {
	Exists     bool    `json:"exists"`
	CreatedAt  *string `json:"createdAt,omitempty"`
	LastUsedAt *string `json:"lastUsedAt,omitempty"`
	// Path is the address, relative to this server, only in the answer to
	// making it: the secret is not kept and cannot be shown again.
	Path string `json:"path,omitempty"`
}

func (s *Server) handleGetCalendarFeed(c *echo.Context) error {
	user, _ := currentUser(c)
	feed, ok := s.calendar.Feed(c.Request().Context(), user.ID)
	if !ok {
		return c.JSON(http.StatusOK, calendarFeedView{})
	}
	view := calendarFeedView{Exists: true}
	created := rfc3339(feed.CreatedAt)
	view.CreatedAt = &created
	if feed.LastUsedAt != nil {
		used := rfc3339(*feed.LastUsedAt)
		view.LastUsedAt = &used
	}
	return c.JSON(http.StatusOK, view)
}

func (s *Server) handleMakeCalendarFeed(c *echo.Context) error {
	user, _ := currentUser(c)
	if _, ok := currentAPIToken(c); ok {
		// A program does not need somebody's calendar address made for it,
		// and one made this way would be a credential nobody saw issued.
		return echo.NewHTTPError(http.StatusForbidden, "a calendar address is made by its owner, signed in")
	}
	secret, err := s.calendar.MakeFeed(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	s.recordFeedAudit(c, audit.ActionCalendarFeedCreated)
	created := rfc3339(time.Now())
	return c.JSON(http.StatusCreated, calendarFeedView{
		Exists: true, CreatedAt: &created, Path: "/api/calendar/feeds/" + secret + "/events.ics",
	})
}

func (s *Server) handleRevokeCalendarFeed(c *echo.Context) error {
	user, _ := currentUser(c)
	removed, err := s.calendar.RevokeFeed(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	if removed {
		s.recordFeedAudit(c, audit.ActionCalendarFeedRevoked)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) recordFeedAudit(c *echo.Context, action audit.Action) {
	if s.audit == nil {
		return
	}
	user, _ := currentUser(c)
	if err := s.audit.Record(c.Request().Context(), audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: action,
		TargetKind: "user", TargetID: user.ID.String(),
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}); err != nil {
		s.log.Warn("could not record a calendar address in the audit log", "error", err)
	}
}

/*
handleCalendarFeed serves a person's feed to whoever holds its address — their
calendar application. An address that is unknown, revoked, or belongs to a
suspended account is refused alike.
*/
func (s *Server) handleCalendarFeed(c *echo.Context) error {
	ctx := c.Request().Context()
	missing := echo.NewHTTPError(http.StatusNotFound, "no such calendar")
	owner, err := s.calendar.FeedOwner(ctx, c.Param("secret"))
	if err != nil {
		return missing
	}
	user, err := s.queries.GetUserByID(ctx, owner)
	if err != nil || identity.IsSuspended(user) {
		return missing
	}
	permissions, err := s.authz.PermissionsFor(ctx, owner)
	if err != nil {
		return err
	}
	viewer, err := s.calendar.LoadViewer(ctx, owner, permissions)
	if err != nil {
		return err
	}
	events, err := s.calendar.FeedEvents(ctx, viewer)
	if err != nil {
		return err
	}
	return s.writeICS(c, s.serverDisplayName(ctx)+" — "+user.Username, "events.ics", events)
}

func (s *Server) handlePublicCalendar(c *echo.Context) error {
	ctx := c.Request().Context()
	if s.settings.Get(ctx, settings.EventsPublicFeed) != "true" {
		return echo.NewHTTPError(http.StatusNotFound, "this server does not publish its events")
	}
	events, err := s.calendar.PublicEvents(ctx)
	if err != nil {
		return err
	}
	return s.writeICS(c, s.serverDisplayName(ctx), "public.ics", events)
}
