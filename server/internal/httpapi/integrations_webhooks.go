package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
)

/*
Outgoing webhooks, as their owner manages them.

Under /api/me, like tokens and passkeys, because a subscription belongs to the
person who made it: it delivers what they can read, it stops when they can no
longer read it, and nobody else can see or remove it.
*/

func init() {
	registerRoutes(func(s *Server) {
		/*
			Every event this server stores, wherever it was stored from, and
			every account it creates, however it came about — told here once,
			rather than by each route remembering to.
		*/
		if s.rooms != nil {
			s.rooms.OnAppended(s.queueWebhooks)
		}
		if s.identity != nil {
			s.identity.OnCreated(func(ctx context.Context, user dbgen.User) {
				s.queueServerEvent(ctx, webhooks.TypeAccountCreated, s.serverName(),
					map[string]any{"account": s.accountRef(user)})
			})
		}

		own := s.echo.Group("/api/me/webhooks", requireAuthentication)
		own.GET("", s.handleListWebhooks)
		own.POST("", s.handleCreateWebhook)
		own.GET("/:id", s.handleGetWebhook)
		own.DELETE("/:id", s.handleDeleteWebhook)
		own.POST("/:id/enable", s.handleEnableWebhook)

		// What a subscription can ask for, so a client can offer the list
		// rather than guess it.
		s.echo.GET("/api/webhooks/event-types", s.handleWebhookEventTypes, requireAuthentication)
	})

	documentRoutes(map[string]entry{
		"GET /api/me/webhooks":             {Summary: "List your outgoing webhooks", Authentication: "session or token"},
		"POST /api/me/webhooks":            {Summary: "Subscribe an endpoint to a channel's, a project's or the server's events", Authentication: "session or token"},
		"GET /api/me/webhooks/:id":         {Summary: "An outgoing webhook and its delivery history", Authentication: "session or token"},
		"DELETE /api/me/webhooks/:id":      {Summary: "Remove an outgoing webhook", Authentication: "session or token"},
		"POST /api/me/webhooks/:id/enable": {Summary: "Turn a disabled webhook back on", Authentication: "session or token"},
		"GET /api/webhooks/event-types":    {Summary: "Event types a webhook can receive", Authentication: "session or token"},
	})
}

// webhookView is one subscription as its owner sees it. Never the secret.
type webhookView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	URL   string `json:"url"`
	// Scope is channel, project or server.
	Scope string `json:"scope"`
	// RoomID is the channel, or the project's stream; absent for the server.
	RoomID string `json:"roomId,omitempty"`
	// ProjectID is set for a project subscription, the same as RoomID.
	ProjectID      string   `json:"projectId,omitempty"`
	EventTypes     []string `json:"eventTypes"`
	PayloadVersion int32    `json:"payloadVersion"`
	// State is "active" or "disabled".
	State          string     `json:"state"`
	DisabledAt     *time.Time `json:"disabledAt,omitempty"`
	DisabledReason string     `json:"disabledReason,omitempty"`
	// ConsecutiveFailures counts failed attempts since the last success.
	ConsecutiveFailures int32 `json:"consecutiveFailures"`
	// RateLimited counts deliveries held back by the rate limit, and
	// LastRateLimited says when — so a limit is visible rather than inferred.
	RateLimited     int64      `json:"rateLimited"`
	LastRateLimited *time.Time `json:"lastRateLimited,omitempty"`
	// AccessLost is when a delivery last found the owner unable to read the
	// channel. Deliveries stop while that holds.
	AccessLost *time.Time `json:"accessLost,omitempty"`
	Created    time.Time  `json:"created"`
}

func viewOfWebhook(subscription dbgen.WebhookSubscription) webhookView {
	view := webhookView{
		ID:                  subscription.ID.String(),
		Label:               subscription.Label,
		URL:                 subscription.Url,
		Scope:               subscription.Scope,
		EventTypes:          subscription.EventTypes,
		PayloadVersion:      subscription.PayloadVersion,
		State:               subscription.State,
		DisabledAt:          subscription.DisabledAt,
		ConsecutiveFailures: subscription.ConsecutiveFailures,
		RateLimited:         subscription.RateLimitedCount,
		LastRateLimited:     subscription.LastRateLimitedAt,
		AccessLost:          subscription.AccessLostAt,
		Created:             subscription.CreatedAt,
	}
	if subscription.DisabledReason != nil {
		view.DisabledReason = *subscription.DisabledReason
	}
	if subscription.RoomID != nil {
		view.RoomID = subscription.RoomID.String()
		if subscription.Scope == webhooks.ScopeProject {
			view.ProjectID = view.RoomID
		}
	}
	return view
}

// createdWebhook is the one response carrying the secret.
type createdWebhook struct {
	webhookView
	// Secret keys the HMAC on every delivery. Shown now and never again.
	Secret string `json:"secret"`
}

type createWebhookRequest struct {
	// Scope is channel (the default), project or server.
	Scope string `json:"scope"`
	// RoomID names the channel, for a channel subscription.
	RoomID string `json:"roomId"`
	// ProjectID names the project, for a project subscription.
	ProjectID  string   `json:"projectId"`
	Label      string   `json:"label"`
	URL        string   `json:"url"`
	EventTypes []string `json:"eventTypes"`
}

func (s *Server) handleListWebhooks(c *echo.Context) error {
	user, _ := currentUser(c)
	service := s.webhookService()
	if service == nil {
		return webhooksUnavailable()
	}

	subscriptions, err := service.List(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	views := make([]webhookView, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		views = append(views, viewOfWebhook(subscription))
	}
	return c.JSON(http.StatusOK, views)
}

func (s *Server) handleCreateWebhook(c *echo.Context) error {
	user, _ := currentUser(c)
	service := s.webhookService()
	if service == nil {
		return webhooksUnavailable()
	}

	var request createWebhookRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	scope := request.Scope
	if scope == "" {
		scope = webhooks.ScopeChannel
	}
	var roomID uuid.UUID
	switch scope {
	case webhooks.ScopeChannel, webhooks.ScopeProject:
		named := request.RoomID
		if scope == webhooks.ScopeProject {
			named = request.ProjectID
		}
		parsed, err := uuid.Parse(named)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "no such "+scope)
		}
		roomID = parsed
	case webhooks.ScopeServer:
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "scope is channel, project or server")
	}

	subscription, err := service.Subscribe(c.Request().Context(), webhooks.NewSubscription{
		OwnerID:    user.ID,
		Scope:      scope,
		RoomID:     roomID,
		Label:      request.Label,
		URL:        request.URL,
		EventTypes: request.EventTypes,
	})
	if err != nil {
		return webhookError(err)
	}

	s.recordModeration(c, user, audit.ActionWebhookSubscribed, "webhook", subscription.ID.String(),
		map[string]any{"scope": subscription.Scope, "room": auditedID(subscription.RoomID),
			"url": subscription.Url, "eventTypes": subscription.EventTypes})

	return c.JSON(http.StatusCreated, createdWebhook{
		webhookView: viewOfWebhook(subscription),
		Secret:      subscription.Secret,
	})
}

// historyView is what has been tried for one subscription recently.
type historyView struct {
	webhookView
	Deliveries []deliveryView `json:"deliveries"`
	Attempts   []attemptView  `json:"attempts"`
}

type deliveryView struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Event string `json:"event,omitempty"`
	// State is pending, delivering, delivered, abandoned or cancelled.
	State         string     `json:"state"`
	Attempts      int32      `json:"attempts"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
	Created       time.Time  `json:"created"`
	Completed     *time.Time `json:"completed,omitempty"`
}

type attemptView struct {
	ID       string    `json:"id"`
	Delivery string    `json:"delivery"`
	Kind     string    `json:"kind"`
	Attempt  int32     `json:"attempt"`
	At       time.Time `json:"at"`
	// Outcome is delivered, failed, or rate_limited.
	Outcome    string `json:"outcome"`
	Status     int32  `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	Response   string `json:"response,omitempty"`
	DurationMs int32  `json:"durationMs"`
}

// historyLimit bounds what one request returns: recent history, not all of it.
const historyLimit = 100

func (s *Server) handleGetWebhook(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()
	service := s.webhookService()
	if service == nil {
		return webhooksUnavailable()
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return webhookError(webhooks.ErrNotFound)
	}
	subscription, err := service.Owned(ctx, user.ID, id)
	if err != nil {
		return webhookError(err)
	}

	history, err := service.History(ctx, subscription.ID, historyLimit)
	if err != nil {
		return err
	}

	view := historyView{
		webhookView: viewOfWebhook(subscription),
		Deliveries:  make([]deliveryView, 0, len(history.Deliveries)),
		Attempts:    make([]attemptView, 0, len(history.Attempts)),
	}
	for _, row := range history.Deliveries {
		entry := deliveryView{
			ID: row.ID.String(), Kind: row.Kind, State: row.State, Attempts: row.Attempts,
			Created: row.CreatedAt, Completed: row.CompletedAt,
		}
		if row.EventID != nil {
			entry.Event = *row.EventID
		}
		if row.State == "pending" {
			next := row.NextAttemptAt
			entry.NextAttemptAt = &next
		}
		view.Deliveries = append(view.Deliveries, entry)
	}
	for _, row := range history.Attempts {
		entry := attemptView{
			ID: row.ID.String(), Delivery: row.DeliveryID.String(), Kind: row.Kind,
			Attempt: row.Attempt, At: row.AttemptedAt, Outcome: row.Outcome, DurationMs: row.DurationMs,
		}
		if row.StatusCode != nil {
			entry.Status = *row.StatusCode
		}
		if row.Error != nil {
			entry.Error = *row.Error
		}
		if row.Response != nil {
			entry.Response = *row.Response
		}
		view.Attempts = append(view.Attempts, entry)
	}

	return c.JSON(http.StatusOK, view)
}

func (s *Server) handleDeleteWebhook(c *echo.Context) error {
	user, _ := currentUser(c)
	service := s.webhookService()
	if service == nil {
		return webhooksUnavailable()
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return webhookError(webhooks.ErrNotFound)
	}
	removed, err := service.Unsubscribe(c.Request().Context(), user.ID, id)
	if err != nil {
		return webhookError(err)
	}

	s.recordModeration(c, user, audit.ActionWebhookUnsubscribed, "webhook", removed.ID.String(),
		map[string]any{"scope": removed.Scope, "room": auditedID(removed.RoomID), "url": removed.Url})
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleEnableWebhook(c *echo.Context) error {
	user, _ := currentUser(c)
	service := s.webhookService()
	if service == nil {
		return webhooksUnavailable()
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return webhookError(webhooks.ErrNotFound)
	}
	enabled, err := service.Enable(c.Request().Context(), user.ID, id)
	if err != nil {
		return webhookError(err)
	}
	return c.JSON(http.StatusOK, viewOfWebhook(enabled))
}

type eventTypesView struct {
	// EventTypes are the names alone, as clients written before the
	// catalogue described them read them.
	EventTypes []string `json:"eventTypes"`
	// Catalogue describes each: its group, where it can be asked for, and
	// the permission it needs, if any.
	Catalogue      []catalogueEntry `json:"catalogue"`
	PayloadVersion int32            `json:"payloadVersion"`
}

type catalogueEntry struct {
	webhooks.EventType
	// Available says whether the caller could subscribe to it now: false
	// for a type whose permission they do not hold.
	Available bool `json:"available"`
}

func (s *Server) handleWebhookEventTypes(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()
	held := map[string]bool{}
	catalogue := make([]catalogueEntry, 0, len(webhooks.Catalogue))
	for _, kind := range webhooks.Catalogue {
		available := true
		if kind.Permission != "" {
			if _, asked := held[kind.Permission]; !asked {
				held[kind.Permission] = s.holdsForWebhooks(ctx, user.ID, kind.Permission)
			}
			available = held[kind.Permission]
		}
		catalogue = append(catalogue, catalogueEntry{EventType: kind, Available: available})
	}
	return c.JSON(http.StatusOK, eventTypesView{
		EventTypes:     webhooks.EventTypes,
		Catalogue:      catalogue,
		PayloadVersion: webhooks.CurrentVersion(),
	})
}

// auditedID is an identifier for the audit log, or nothing.
func auditedID(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func webhooksUnavailable() error {
	return echo.NewHTTPError(http.StatusNotFound, "this server does not offer webhooks")
}

func webhookError(err error) error {
	switch {
	case errors.Is(err, webhooks.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, webhooks.ErrCannotRead):
		// As missing, like every other route: subscribing must not be a way
		// to learn a private channel exists.
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	case errors.Is(err, webhooks.ErrUnknownEventType),
		errors.Is(err, webhooks.ErrNoEventTypes),
		errors.Is(err, webhooks.ErrLabelRequired),
		errors.Is(err, webhooks.ErrWrongScope):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, webhooks.ErrNotPermitted):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, delivery.ErrPrivateDestination), errors.Is(err, delivery.ErrUnusableURL):
		return destinationError(err)
	default:
		return err
	}
}
