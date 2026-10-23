package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/calendar"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Events: meetups, talks, workshops and office hours.

Every route answers an event the caller may not see exactly as one that does
not exist. Who may see what is decided by calendar.Viewer.Sees, from facts
loaded once per request; a refusal to act on something the caller can see is a
403 saying why.
*/
func init() {
	registerRoutes(func(s *Server) {
		if !s.ensureCalendar() {
			return
		}

		feature := s.requireFeature(settings.FeatureEvents)
		group := s.echo.Group("/api/events", requireAuthentication, feature)
		group.GET("", s.handleListEvents)
		group.POST("", s.handleCreateEvent)
		group.GET("/:eventId", s.handleGetEvent)
		group.PUT("/:eventId", s.handleUpdateEvent)
		group.DELETE("/:eventId", s.handleDeleteEvent)
		group.POST("/:eventId/cancel", s.handleCancelEvent)
		group.PUT("/:eventId/cover", s.handleSetEventCover)
	})

	documentRoutes(map[string]entry{
		"GET /api/events":                  {Summary: "Occurrences of the events you can see, over a range", Authentication: "session or token"},
		"POST /api/events":                 {Summary: "Schedule an event", Authentication: "session or token", Permission: "event.create"},
		"GET /api/events/:eventId":         {Summary: "An event, with its people, agenda and answers", Authentication: "session or token"},
		"PUT /api/events/:eventId":         {Summary: "Change an event", Authentication: "session or token", Permission: "its creator, a host, or event.manage.any"},
		"DELETE /api/events/:eventId":      {Summary: "Delete an event and its conversation", Authentication: "session or token", Permission: "its creator, or event.manage.any"},
		"POST /api/events/:eventId/cancel": {Summary: "Cancel an event", Authentication: "session or token", Permission: "its creator, a host, or event.manage.any"},
		"PUT /api/events/:eventId/cover":   {Summary: "Set or clear an event's cover image", Authentication: "session or token", Permission: "its creator, a host, or event.manage.any"},
	})
}

/*
ensureCalendar builds the calendar the first time any of its routes are
mounted, whichever file's registration runs first; false on a server with no
database to keep events in.
*/
func (s *Server) ensureCalendar() bool {
	if s.db == nil || s.rooms == nil {
		return false
	}
	if s.calendar == nil {
		s.calendar = calendar.NewService(s.db, s.rooms, s.log)
	}
	return true
}

// viewerOf is who is asking, with what decides what of the calendar they see.
func (s *Server) viewerOf(c *echo.Context) (calendar.Viewer, error) {
	user, _ := currentUser(c)
	return s.calendar.LoadViewer(c.Request().Context(), user.ID, currentPermissions(c))
}

// eventError maps a calendar failure to a response.
func eventError(err error) error {
	switch {
	case errors.Is(err, calendar.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such event")
	case errors.Is(err, calendar.ErrInvalid):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, calendar.ErrNotAllowed):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, calendar.ErrCancelled), errors.Is(err, calendar.ErrEnded):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, rooms.ErrRoomNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such event")
	}
	return err
}

func eventIDOf(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("eventId"))
	if err != nil {
		return uuid.Nil, echo.NewHTTPError(http.StatusNotFound, "no such event")
	}
	return id, nil
}

type eventPersonView struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Address     string `json:"address"`
}

type eventChannelView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type eventCoverView struct {
	URL        string `json:"url"`
	PreviewURL string `json:"previewUrl,omitempty"`
}

/*
eventView is an event as every route shows it.

StartsAt and EndsAt are the first occurrence; a list also says which occurrence
it is listing, in OccurrenceStartsAt and OccurrenceEndsAt. Every instant is RFC
3339 in UTC, and TimeZone is where the organiser meant it — clients show both
when they differ.
*/
type eventView struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	TimeZone     string            `json:"timeZone"`
	AllDay       bool              `json:"allDay"`
	Recurrence   string            `json:"recurrence,omitempty"`
	Exceptions   []string          `json:"exceptions,omitempty"`
	Location     string            `json:"location,omitempty"`
	Online       bool              `json:"online"`
	OnlineURL    string            `json:"onlineUrl,omitempty"`
	Capacity     *int32            `json:"capacity,omitempty"`
	Visibility   string            `json:"visibility"`
	Channel      *eventChannelView `json:"channel,omitempty"`
	Cover        *eventCoverView   `json:"cover,omitempty"`
	CancelledAt  *string           `json:"cancelledAt,omitempty"`
	CancelReason string            `json:"cancelReason,omitempty"`
	// Imported marks an event that comes from a subscribed calendar, and so
	// changes there rather than here.
	Imported bool `json:"imported,omitempty"`
	// MyAnswer is the caller's own answer: going, maybe, declined or
	// waitlisted, or absent.
	MyAnswer string `json:"myAnswer,omitempty"`

	OccurrenceStartsAt string `json:"occurrenceStartsAt,omitempty"`
	OccurrenceEndsAt   string `json:"occurrenceEndsAt,omitempty"`
}

func rfc3339(at time.Time) string { return at.UTC().Format(time.RFC3339) }

func eventViewOf(event calendar.Listed, answer string) eventView {
	view := eventView{
		ID: event.ID.String(), Kind: event.Kind, Title: event.Title, Description: event.Description,
		StartsAt: rfc3339(event.StartsAt), EndsAt: rfc3339(event.EndsAt), TimeZone: event.TimeZone,
		AllDay: event.AllDay, Recurrence: event.Recurrence, Location: event.Location,
		Online: event.Online, OnlineURL: event.OnlineUrl, Capacity: event.Capacity,
		Visibility: event.Visibility, CancelReason: event.CancelReason,
		Imported: event.SubscriptionID != nil, MyAnswer: answer,
	}
	for _, skipped := range event.Exceptions {
		view.Exceptions = append(view.Exceptions, rfc3339(skipped))
	}
	if event.ChannelID != nil {
		view.Channel = &eventChannelView{ID: event.ChannelID.String(), Name: event.ChannelName}
	}
	if event.CoverAttachmentID != nil {
		id := event.CoverAttachmentID.String()
		view.Cover = &eventCoverView{URL: "/api/attachments/" + id, PreviewURL: "/api/attachments/" + id + "/preview"}
	}
	if event.CancelledAt != nil {
		at := rfc3339(*event.CancelledAt)
		view.CancelledAt = &at
	}
	return view
}

// answersOf is the caller's answer to every event they answered.
func (s *Server) answersOf(ctx context.Context, userID uuid.UUID) map[uuid.UUID]string {
	answers := map[uuid.UUID]string{}
	rows, err := s.calendar.Queries().ListMyCalendarAnswers(ctx, userID)
	if err != nil {
		return answers
	}
	for _, row := range rows {
		answers[row.EventID] = row.Status
	}
	return answers
}

/*
handleListEvents lists the occurrences the caller may see over a range —
?from and ?to, RFC 3339, at most 400 days apart; by default the next 90 days —
optionally in one ?channel, at most ?limit (500) of them.
*/
func (s *Server) handleListEvents(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}

	since, until := time.Now(), time.Now().Add(90*24*time.Hour)
	if raw := c.QueryParam("from"); raw != "" {
		if since, err = time.Parse(time.RFC3339, raw); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "from is an RFC 3339 instant")
		}
	}
	if raw := c.QueryParam("to"); raw != "" {
		if until, err = time.Parse(time.RFC3339, raw); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "to is an RFC 3339 instant")
		}
	}
	var channel *uuid.UUID
	if raw := c.QueryParam("channel"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "channel is a channel's id")
		}
		channel = &id
	}
	limit := 500
	if raw := c.QueryParam("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed < limit {
			limit = parsed
		}
	}

	happenings, err := s.calendar.Range(ctx, viewer, since, until, channel, limit)
	if err != nil {
		return eventError(err)
	}
	answers := s.answersOf(ctx, user.ID)
	views := make([]eventView, 0, len(happenings))
	for _, happening := range happenings {
		view := eventViewOf(happening.Event, answers[happening.Event.ID])
		view.OccurrenceStartsAt = rfc3339(happening.Start)
		view.OccurrenceEndsAt = rfc3339(happening.End)
		views = append(views, view)
	}
	return c.JSON(http.StatusOK, views)
}

type sessionView struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Description   string            `json:"description,omitempty"`
	StartsAt      string            `json:"startsAt"`
	EndsAt        string            `json:"endsAt"`
	Speakers      []eventPersonView `json:"speakers"`
	GuestSpeakers string            `json:"guestSpeakers,omitempty"`
}

type eventCountsView struct {
	Going      int64 `json:"going"`
	Maybe      int64 `json:"maybe"`
	Waitlisted int64 `json:"waitlisted"`
}

type eventPermissionsView struct {
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
	// StartCall says the caller may start the event's call: a host, or
	// somebody who manages events there.
	StartCall bool `json:"startCall"`
}

// eventDetailView is one event with everything its page shows.
type eventDetailView struct {
	eventView
	Hosts    []eventPersonView `json:"hosts"`
	Speakers []eventPersonView `json:"speakers"`
	Sessions []sessionView     `json:"sessions"`
	Counts   eventCountsView   `json:"counts"`
	// WaitlistPosition is the caller's place on the waiting list, from one.
	WaitlistPosition int64                `json:"waitlistPosition,omitempty"`
	CreatedBy        *eventPersonView     `json:"createdBy,omitempty"`
	Next             *occurrenceView      `json:"next,omitempty"`
	May              eventPermissionsView `json:"may"`
	// Member says the caller is in the event's conversation — and so can join
	// its call once it has started.
	Member bool `json:"member"`
}

type occurrenceView struct {
	StartsAt string `json:"startsAt"`
	EndsAt   string `json:"endsAt"`
}

func (s *Server) eventDetail(ctx context.Context, viewer calendar.Viewer, event calendar.Listed) (eventDetailView, error) {
	answer, position := s.calendar.MyAnswer(ctx, event.ID, viewer.UserID)
	detail := eventDetailView{
		eventView: eventViewOf(event, answer), WaitlistPosition: position,
		Hosts: []eventPersonView{}, Speakers: []eventPersonView{}, Sessions: []sessionView{},
		May: eventPermissionsView{
			Edit: viewer.MayEdit(event.CalendarEvent), Delete: viewer.MayDelete(event.CalendarEvent),
			StartCall: event.Online && (viewer.Hosting[event.ID] || viewer.Manages(event.CalendarEvent) ||
				(event.CreatedBy != nil && *event.CreatedBy == viewer.UserID)),
		},
	}
	people, err := s.calendar.People(ctx, event.ID)
	if err != nil {
		return eventDetailView{}, err
	}
	names := map[uuid.UUID]eventPersonView{}
	for _, person := range people {
		view := eventPersonView{
			UserID: person.UserID.String(), Username: person.Username, DisplayName: person.DisplayName,
			Address: s.rooms.UserAddress(person.Username),
		}
		names[person.UserID] = view
		if person.Role == calendar.RoleHost {
			detail.Hosts = append(detail.Hosts, view)
		} else {
			detail.Speakers = append(detail.Speakers, view)
		}
	}
	if event.CreatedBy != nil {
		if creator, err := s.queries.GetUserByID(ctx, *event.CreatedBy); err == nil {
			detail.CreatedBy = &eventPersonView{
				UserID: creator.ID.String(), Username: creator.Username, DisplayName: creator.DisplayName,
				Address: s.rooms.UserAddress(creator.Username),
			}
		}
	}
	sessions, err := s.calendar.Sessions(ctx, event.ID)
	if err != nil {
		return eventDetailView{}, err
	}
	for _, session := range sessions {
		view := sessionView{
			ID: session.ID.String(), Title: session.Title, Description: session.Description,
			StartsAt: rfc3339(session.StartsAt), EndsAt: rfc3339(session.EndsAt),
			GuestSpeakers: session.GuestSpeakers, Speakers: []eventPersonView{},
		}
		for _, speaker := range session.Speakers {
			person, ok := names[speaker]
			if !ok {
				user, err := s.queries.GetUserByID(ctx, speaker)
				if err != nil {
					continue
				}
				person = eventPersonView{
					UserID: user.ID.String(), Username: user.Username, DisplayName: user.DisplayName,
					Address: s.rooms.UserAddress(user.Username),
				}
			}
			view.Speakers = append(view.Speakers, person)
		}
		detail.Sessions = append(detail.Sessions, view)
	}
	counts, err := s.calendar.CountsFor(ctx, event.ID)
	if err != nil {
		return eventDetailView{}, err
	}
	detail.Counts = eventCountsView{Going: counts.Going, Maybe: counts.Maybe, Waitlisted: counts.Waitlisted}
	if next, ok := event.Series().Next(time.Now().Add(-event.EndsAt.Sub(event.StartsAt))); ok && event.CancelledAt == nil {
		detail.Next = &occurrenceView{StartsAt: rfc3339(next.Start), EndsAt: rfc3339(next.End)}
	}
	detail.Member, _ = s.queries.IsRoomMember(ctx, dbgen.IsRoomMemberParams{RoomID: event.ID, UserID: viewer.UserID})
	return detail, nil
}

func (s *Server) handleGetEvent(c *echo.Context) error {
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
	detail, err := s.eventDetail(ctx, viewer, event)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, detail)
}

type sessionRequest struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	StartsAt      string   `json:"startsAt"`
	EndsAt        string   `json:"endsAt"`
	Speakers      []string `json:"speakers"`
	GuestSpeakers string   `json:"guestSpeakers"`
}

/*
eventRequest is an event as somebody sends it, for scheduling and for changing.

Changing sends every field, as the form holds them all. Hosts, speakers and
sessions may be left out to leave them as they are; an empty list clears them.
People are named by their account ids.
*/
type eventRequest struct {
	ChannelID   string   `json:"channelId"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	StartsAt    string   `json:"startsAt"`
	EndsAt      string   `json:"endsAt"`
	TimeZone    string   `json:"timeZone"`
	AllDay      bool     `json:"allDay"`
	Recurrence  string   `json:"recurrence"`
	Exceptions  []string `json:"exceptions"`
	Location    string   `json:"location"`
	Online      bool     `json:"online"`
	OnlineURL   string   `json:"onlineUrl"`
	Capacity    *int32   `json:"capacity"`
	Visibility  string   `json:"visibility"`

	Hosts    *[]string         `json:"hosts"`
	Speakers *[]string         `json:"speakers"`
	Sessions *[]sessionRequest `json:"sessions"`
}

func parseInstant(raw, name string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, echo.NewHTTPError(http.StatusBadRequest, name+" is an RFC 3339 instant")
	}
	return at, nil
}

func parseIDs(raw *[]string) (*[]uuid.UUID, error) {
	if raw == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(*raw))
	for _, each := range *raw {
		id, err := uuid.Parse(each)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "people are named by their ids")
		}
		ids = append(ids, id)
	}
	return &ids, nil
}

func (request eventRequest) draft() (calendar.Draft, error) {
	draft := calendar.Draft{
		Kind: request.Kind, Title: request.Title, Description: request.Description,
		TimeZone: request.TimeZone, AllDay: request.AllDay, Recurrence: request.Recurrence,
		Location: request.Location, Online: request.Online, OnlineURL: request.OnlineURL,
		Capacity: request.Capacity, Visibility: request.Visibility,
	}
	var err error
	if request.ChannelID != "" {
		id, err := uuid.Parse(request.ChannelID)
		if err != nil {
			return draft, echo.NewHTTPError(http.StatusBadRequest, "channelId is a channel's id")
		}
		draft.ChannelID = &id
	}
	if draft.StartsAt, err = parseInstant(request.StartsAt, "startsAt"); err != nil {
		return draft, err
	}
	if draft.EndsAt, err = parseInstant(request.EndsAt, "endsAt"); err != nil {
		return draft, err
	}
	for _, raw := range request.Exceptions {
		skipped, err := parseInstant(raw, "an exception")
		if err != nil {
			return draft, err
		}
		draft.Exceptions = append(draft.Exceptions, skipped)
	}
	if draft.Hosts, err = parseIDs(request.Hosts); err != nil {
		return draft, err
	}
	if draft.Speakers, err = parseIDs(request.Speakers); err != nil {
		return draft, err
	}
	if request.Sessions != nil {
		sessions := make([]calendar.Session, 0, len(*request.Sessions))
		for _, raw := range *request.Sessions {
			session := calendar.Session{Title: raw.Title, Description: raw.Description, GuestSpeakers: raw.GuestSpeakers}
			if session.StartsAt, err = parseInstant(raw.StartsAt, "a session's startsAt"); err != nil {
				return draft, err
			}
			if session.EndsAt, err = parseInstant(raw.EndsAt, "a session's endsAt"); err != nil {
				return draft, err
			}
			speakers, err := parseIDs(&raw.Speakers)
			if err != nil {
				return draft, err
			}
			session.Speakers = *speakers
			sessions = append(sessions, session)
		}
		draft.Sessions = &sessions
	}
	return draft, nil
}

func (s *Server) handleCreateEvent(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	var request eventRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	draft, err := request.draft()
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	created, err := s.calendar.Create(ctx, user, viewer, draft)
	if err != nil {
		return eventError(err)
	}
	s.publishEvent(ctx, created.Log)

	// The creator now hosts it; read their facts again so the answer says so.
	if viewer, err = s.viewerOf(c); err != nil {
		return err
	}
	detail, err := s.eventDetail(ctx, viewer, created.Event)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, detail)
}

func (s *Server) handleUpdateEvent(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	var request eventRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	draft, err := request.draft()
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	changed, err := s.calendar.Update(ctx, user, viewer, id, draft)
	if err != nil {
		return eventError(err)
	}
	s.publishEvent(ctx, changed.Log)
	s.recordManaged(c, viewer, changed.Event.CalendarEvent, audit.ActionEventManaged, nil)
	s.afterEventChanged(ctx, user, changed)

	if viewer, err = s.viewerOf(c); err != nil {
		return err
	}
	detail, err := s.eventDetail(ctx, viewer, changed.Event)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, detail)
}

func (s *Server) handleCancelEvent(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	cancelled, err := s.calendar.Cancel(ctx, user, viewer, id, request.Reason)
	if err != nil {
		return eventError(err)
	}
	s.publishEvent(ctx, cancelled.Log)
	s.recordEventAudit(c, audit.ActionEventCancelled, cancelled.Event.CalendarEvent, map[string]any{
		"title": cancelled.Event.Title, "reason": cancelled.Event.CancelReason,
	})
	s.afterEventCancelled(ctx, user, cancelled)
	detail, err := s.eventDetail(ctx, viewer, cancelled.Event)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, detail)
}

func (s *Server) handleDeleteEvent(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	deleted, err := s.calendar.Delete(ctx, viewer, id)
	if err != nil {
		return eventError(err)
	}
	s.recordEventAudit(c, audit.ActionEventDeleted, deleted.CalendarEvent, map[string]any{"title": deleted.Title})
	return c.NoContent(http.StatusNoContent)
}

/*
handleSetEventCover makes an image already uploaded to the event's own
conversation its cover, or clears it with a null. Uploading is the ordinary
attachment route, so the limits, scanning and storage of every other file
apply.
*/
func (s *Server) handleSetEventCover(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := eventIDOf(c)
	if err != nil {
		return err
	}
	var request struct {
		AttachmentID *string `json:"attachmentId"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	viewer, err := s.viewerOf(c)
	if err != nil {
		return err
	}
	var attachment *uuid.UUID
	room := id
	if request.AttachmentID != nil {
		parsed, err := uuid.Parse(*request.AttachmentID)
		if err != nil || s.attachments == nil {
			return echo.NewHTTPError(http.StatusBadRequest, "attachmentId is an attachment's id")
		}
		stored, err := s.attachments.Get(ctx, parsed)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "attachmentId is an attachment's id")
		}
		if attachmentKind(stored) != "image" {
			return echo.NewHTTPError(http.StatusBadRequest, "a cover is an image")
		}
		attachment, room = &parsed, stored.RoomID
	}
	if err := s.calendar.SetCover(ctx, viewer, id, attachment, room); err != nil {
		return eventError(err)
	}
	event, err := s.calendar.Get(ctx, viewer, id)
	if err != nil {
		return eventError(err)
	}
	detail, err := s.eventDetail(ctx, viewer, event)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, detail)
}

/*
recordManaged writes the audit entry for somebody acting on an event under
event.manage.any — one they neither made nor host. Acting on one's own event is
ordinary and not recorded.
*/
func (s *Server) recordManaged(c *echo.Context, viewer calendar.Viewer, event dbgen.CalendarEvent, action audit.Action, detail map[string]any) {
	if viewer.Runs(event) {
		return
	}
	if detail == nil {
		detail = map[string]any{}
	}
	detail["title"] = event.Title
	s.recordEventAudit(c, action, event, detail)
}

func (s *Server) recordEventAudit(c *echo.Context, action audit.Action, event dbgen.CalendarEvent, detail map[string]any) {
	if s.audit == nil {
		return
	}
	entry := audit.Entry{
		Action: action, TargetKind: "event", TargetID: event.ID.String(),
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(), Detail: detail,
	}
	if user, ok := currentUser(c); ok {
		entry.ActorID = &user.ID
		entry.ActorLabel = user.Username
	}
	if err := s.audit.Record(c.Request().Context(), entry); err != nil {
		s.log.Warn("could not record an event in the audit log", "error", err)
	}
}
