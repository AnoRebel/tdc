package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/calendar"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Calendars into the server: a file somebody has, and a calendar published
elsewhere that the server follows.
*/
func init() {
	registerRoutes(func(s *Server) {
		if !s.ensureCalendar() {
			return
		}
		feature := s.requireFeature(settings.FeatureEvents)
		imports := s.echo.Group("/api/events/import", requireAuthentication, feature)
		imports.POST("/preview", s.handlePreviewImport)
		imports.POST("", s.handleImport)

		subscriptions := s.echo.Group("/api/calendar/subscriptions", requireAuthentication, feature)
		subscriptions.GET("", s.handleListSubscriptions)
		subscriptions.POST("", s.handleSubscribe)
		subscriptions.DELETE("/:subscriptionId", s.handleUnsubscribe)
		subscriptions.POST("/:subscriptionId/refresh", s.handleRefreshSubscription)
	})

	documentRoutes(map[string]entry{
		"POST /api/events/import/preview":                          {Summary: "Say what importing a calendar file would create, update or skip", Authentication: "session or token", Permission: "event.create"},
		"POST /api/events/import":                                  {Summary: "Import events from a calendar file", Authentication: "session or token", Permission: "event.create"},
		"GET /api/calendar/subscriptions":                          {Summary: "Calendars this server follows that you may manage", Authentication: "session or token", Permission: "event.calendars.manage"},
		"POST /api/calendar/subscriptions":                         {Summary: "Follow a calendar published elsewhere", Authentication: "session or token", Permission: "event.calendars.manage"},
		"DELETE /api/calendar/subscriptions/:subscriptionId":       {Summary: "Stop following a calendar, removing its events", Authentication: "session or token", Permission: "event.calendars.manage"},
		"POST /api/calendar/subscriptions/:subscriptionId/refresh": {Summary: "Fetch a followed calendar now", Authentication: "session or token", Permission: "event.calendars.manage"},
	})
}

// calendarFile reads the calendar a request carries: a JSON body's "ics", or
// a file uploaded as "file".
func calendarFile(c *echo.Context, ics string) ([]byte, error) {
	if ics != "" {
		return []byte(ics), nil
	}
	file, err := c.FormFile("file")
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "send the calendar as a file, or as ics")
	}
	if file.Size > calendar.MaxImportBytes {
		return nil, echo.NewHTTPError(http.StatusRequestEntityTooLarge, "a calendar file is at most 1 MB")
	}
	opened, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = opened.Close() }()
	return io.ReadAll(io.LimitReader(opened, calendar.MaxImportBytes+1))
}

type importRequest struct {
	ICS        string   `json:"ics"`
	ChannelID  string   `json:"channelId"`
	Visibility string   `json:"visibility"`
	Kind       string   `json:"kind"`
	UIDs       []string `json:"uids"`
}

func (r *importRequest) fromForm(c *echo.Context) {
	if r.ChannelID == "" {
		r.ChannelID = c.FormValue("channelId")
	}
	if r.Visibility == "" {
		r.Visibility = c.FormValue("visibility")
	}
	if r.Kind == "" {
		r.Kind = c.FormValue("kind")
	}
	if len(r.UIDs) == 0 && c.FormValue("uids") != "" {
		r.UIDs = strings.Split(c.FormValue("uids"), "\n")
	}
}

type importPlanView struct {
	UID        string   `json:"uid"`
	Title      string   `json:"title"`
	StartsAt   string   `json:"startsAt"`
	EndsAt     string   `json:"endsAt"`
	TimeZone   string   `json:"timeZone"`
	AllDay     bool     `json:"allDay"`
	Recurrence string   `json:"recurrence,omitempty"`
	Location   string   `json:"location,omitempty"`
	Cancelled  bool     `json:"cancelled,omitempty"`
	Action     string   `json:"action"`
	Reason     string   `json:"reason,omitempty"`
	Warnings   []string `json:"warnings"`
}

func (s *Server) readImport(c *echo.Context) (importRequest, []calendar.Incoming, error) {
	var request importRequest
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "application/json") {
		if err := c.Bind(&request); err != nil {
			return request, nil, echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
		}
	} else {
		request.fromForm(c)
	}
	data, err := calendarFile(c, request.ICS)
	if err != nil {
		return request, nil, err
	}
	incoming, err := calendar.ParseICS(data)
	if err != nil {
		return request, nil, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return request, incoming, nil
}

func (s *Server) handlePreviewImport(c *echo.Context) error {
	_, incoming, err := s.readImport(c)
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	plans := s.calendar.PlanImport(c.Request().Context(), viewer, incoming)
	views := make([]importPlanView, 0, len(plans))
	for _, plan := range plans {
		warnings := plan.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		views = append(views, importPlanView{
			UID: plan.UID, Title: plan.Draft.Title, StartsAt: rfc3339(plan.Draft.StartsAt), EndsAt: rfc3339(plan.Draft.EndsAt),
			TimeZone: plan.Draft.TimeZone, AllDay: plan.Draft.AllDay, Recurrence: plan.Draft.Recurrence,
			Location: plan.Draft.Location, Cancelled: plan.Cancelled, Action: plan.Action, Reason: plan.Reason,
			Warnings: warnings,
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"events": views})
}

func (s *Server) handleImport(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	request, incoming, err := s.readImport(c)
	if err != nil {
		return err
	}
	choice := calendar.ImportChoice{Visibility: request.Visibility, Kind: request.Kind, UIDs: request.UIDs}
	if request.ChannelID != "" {
		id, err := uuid.Parse(request.ChannelID)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "channelId is a channel's id")
		}
		choice.ChannelID = &id
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	imported, err := s.calendar.Import(ctx, user, viewer, incoming, choice)
	for _, entry := range imported.Logs {
		s.publishEvent(ctx, entry)
	}
	if err != nil {
		return eventError(err)
	}
	return c.JSON(http.StatusOK, map[string]int{
		"created": imported.Created, "updated": imported.Updated, "skipped": imported.Skipped,
	})
}

type subscriptionView struct {
	ID            string  `json:"id"`
	URL           string  `json:"url"`
	Label         string  `json:"label"`
	ChannelID     string  `json:"channelId,omitempty"`
	Visibility    string  `json:"visibility"`
	Kind          string  `json:"kind"`
	LastFetchedAt *string `json:"lastFetchedAt,omitempty"`
	LastError     string  `json:"lastError,omitempty"`
	Paused        bool    `json:"paused"`
}

func subscriptionViewOf(subscription dbgen.CalendarSubscription) subscriptionView {
	view := subscriptionView{
		ID: subscription.ID.String(), URL: subscription.Url, Label: subscription.Label,
		Visibility: subscription.Visibility, Kind: subscription.Kind, LastError: subscription.LastError,
		Paused: subscription.PausedAt != nil,
	}
	if subscription.ChannelID != nil {
		view.ChannelID = subscription.ChannelID.String()
	}
	if subscription.LastFetchedAt != nil {
		at := rfc3339(*subscription.LastFetchedAt)
		view.LastFetchedAt = &at
	}
	return view
}

func (s *Server) handleListSubscriptions(c *echo.Context) error {
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	all, err := s.calendar.Queries().ListCalendarSubscriptions(c.Request().Context())
	if err != nil {
		return err
	}
	views := []subscriptionView{}
	for _, subscription := range all {
		if viewer.MayManageSubscription(subscription) {
			views = append(views, subscriptionViewOf(subscription))
		}
	}
	return c.JSON(http.StatusOK, views)
}

func (s *Server) handleSubscribe(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	var request struct {
		URL        string `json:"url"`
		Label      string `json:"label"`
		ChannelID  string `json:"channelId"`
		Visibility string `json:"visibility"`
		Kind       string `json:"kind"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	wanted := calendar.NewSubscription{URL: request.URL, Label: request.Label, Visibility: request.Visibility, Kind: request.Kind}
	if request.ChannelID != "" {
		id, err := uuid.Parse(request.ChannelID)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "channelId is a channel's id")
		}
		wanted.ChannelID = &id
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	subscription, err := s.calendar.Subscribe(ctx, user, viewer, wanted, s.privateDeliveriesAllowed(ctx))
	if errors.Is(err, calendar.ErrAddress) {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err != nil {
		return eventError(err)
	}
	s.recordSubscriptionAudit(c, audit.ActionCalendarSubscriptionCreated, subscription)
	// The first fetch now, so somebody subscribing sees whether it works.
	synced, refreshErr := s.calendar.Refresh(ctx, s.calendarFetcher(), subscription, s.refreshEvery(ctx))
	for _, entry := range synced.Logs {
		s.publishEvent(ctx, entry)
	}
	stored, _ := s.calendar.Queries().GetCalendarSubscription(ctx, subscription.ID)
	view := subscriptionViewOf(stored)
	if refreshErr != nil {
		view.LastError = refreshErr.Error()
	}
	return c.JSON(http.StatusCreated, view)
}

func (s *Server) managedSubscription(c *echo.Context) (dbgen.CalendarSubscription, error) {
	id, err := uuid.Parse(c.Param("subscriptionId"))
	missing := echo.NewHTTPError(http.StatusNotFound, "no such subscription")
	if err != nil {
		return dbgen.CalendarSubscription{}, missing
	}
	subscription, err := s.calendar.Queries().GetCalendarSubscription(c.Request().Context(), id)
	if err != nil {
		return dbgen.CalendarSubscription{}, missing
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return dbgen.CalendarSubscription{}, err
	}
	if !viewer.MayManageSubscription(subscription) {
		return dbgen.CalendarSubscription{}, missing
	}
	return subscription, nil
}

func (s *Server) handleUnsubscribe(c *echo.Context) error {
	subscription, err := s.managedSubscription(c)
	if err != nil {
		return err
	}
	// Its events go with it, conversations and all.
	if _, err := s.db.Exec(c.Request().Context(),
		`DELETE FROM rooms WHERE id IN (SELECT id FROM calendar_events WHERE subscription_id = $1)`, subscription.ID); err != nil {
		return err
	}
	if _, err := s.calendar.Queries().DeleteCalendarSubscription(c.Request().Context(), subscription.ID); err != nil {
		return err
	}
	s.recordSubscriptionAudit(c, audit.ActionCalendarSubscriptionDeleted, subscription)
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleRefreshSubscription(c *echo.Context) error {
	ctx := c.Request().Context()
	subscription, err := s.managedSubscription(c)
	if err != nil {
		return err
	}
	if subscription.PausedAt != nil {
		if err := s.calendar.Queries().ResumeCalendarSubscription(ctx, subscription.ID); err != nil {
			return err
		}
		subscription.Failures = 0
	}
	synced, refreshErr := s.calendar.Refresh(ctx, s.calendarFetcher(), subscription, s.refreshEvery(ctx))
	for _, entry := range synced.Logs {
		s.publishEvent(ctx, entry)
	}
	stored, _ := s.calendar.Queries().GetCalendarSubscription(ctx, subscription.ID)
	view := subscriptionViewOf(stored)
	if refreshErr != nil {
		view.LastError = refreshErr.Error()
	}
	return c.JSON(http.StatusOK, map[string]any{
		"subscription": view, "created": synced.Created, "updated": synced.Updated, "cancelled": synced.Cancelled,
	})
}

func (s *Server) recordSubscriptionAudit(c *echo.Context, action audit.Action, subscription dbgen.CalendarSubscription) {
	if s.audit == nil {
		return
	}
	user, _ := currentUser(c)
	if err := s.audit.Record(c.Request().Context(), audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username, Action: action,
		TargetKind: "calendar_subscription", TargetID: subscription.ID.String(),
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
		Detail: map[string]any{"url": subscription.Url, "label": subscription.Label},
	}); err != nil {
		s.log.Warn("could not record a calendar subscription in the audit log", "error", err)
	}
}

// calendarFetcher fetches through the private-address guard, which the
// operator's integrations.allow_private_addresses relaxes for webhooks and
// calendars alike.
func (s *Server) calendarFetcher() *calendar.Fetcher {
	return calendar.NewFetcher(func() bool { return s.privateDeliveriesAllowed(context.Background()) })
}

func (s *Server) refreshEvery(ctx context.Context) time.Duration {
	minutes, err := strconv.Atoi(s.settings.Get(ctx, settings.EventsSubscriptionRefreshMinutes))
	if err != nil || minutes < 15 {
		minutes = 60
	}
	return time.Duration(minutes) * time.Minute
}

// RunCalendarSubscriptions refreshes followed calendars as they come due,
// one claimed at a time so no two nodes fetch the same one.
func (s *Server) RunCalendarSubscriptions(ctx context.Context) {
	if s.calendar == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RefreshDueSubscriptions(ctx)
		}
	}
}

// RefreshDueSubscriptions makes one pass, returning how many it fetched.
func (s *Server) RefreshDueSubscriptions(ctx context.Context) int {
	if s.calendar == nil || !s.settings.Enabled(ctx, settings.FeatureEvents) {
		return 0
	}
	fetcher := s.calendarFetcher()
	fetched := 0
	for range 20 {
		// Held for longer than a fetch can take, so a node that dies mid-fetch
		// leaves it to be tried again rather than lost.
		subscription, err := s.calendar.Queries().ClaimDueCalendarSubscription(ctx, 300)
		if err != nil {
			return fetched
		}
		synced, err := s.calendar.Refresh(ctx, fetcher, subscription, s.refreshEvery(ctx))
		if err != nil {
			s.log.Info("a followed calendar could not be refreshed", "subscription", subscription.ID, "error", err)
		}
		for _, entry := range synced.Logs {
			s.publishEvent(ctx, entry)
		}
		fetched++
	}
	return fetched
}
