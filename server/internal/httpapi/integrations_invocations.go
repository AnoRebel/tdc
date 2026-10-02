package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
)

/*
Invocations: telling an application about a press at an address of its own.

Set by whoever manages applications, like everything else about one; delivered
through the webhook queue, so it is signed, retried and bounded exactly as a
webhook's delivery is.
*/

func init() {
	registerRoutes(func(s *Server) {
		manage := s.echo.Group("/api/admin/applications", requireAuthentication,
			requirePermission(authz.PermApplicationsManage))
		manage.PUT("/:appId/invocation", s.handleSetInvocation)
		manage.GET("/:appId/invocations", s.handleListInvocations)
	})

	manage := "server.applications.manage"
	documentRoutes(map[string]entry{
		"PUT /api/admin/applications/:appId/invocation": {
			Summary: "Set where an application is told about presses", Authentication: "session or token",
			Permission: manage,
		},
		"GET /api/admin/applications/:appId/invocations": {
			Summary: "An application's recent invocations", Authentication: "session or token",
			Permission: manage,
		},
	})
}

type invocationRequest struct {
	// URL is where presses are delivered, or empty to stop.
	URL string `json:"url"`
}

type invocationResponse struct {
	URL string `json:"url"`
	// Secret signs every invocation. Returned this once.
	Secret string `json:"secret,omitempty"`
}

func (s *Server) handleSetInvocation(c *echo.Context) error {
	user, _ := currentUser(c)
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}
	service := s.webhookService()
	if service == nil {
		return applicationsUnavailable()
	}

	var request invocationRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	updated, err := service.SetInvocation(c.Request().Context(), application.ID, strings.TrimSpace(request.URL))
	if errors.Is(err, webhooks.ErrNoSuchApplication) {
		return echo.NewHTTPError(http.StatusConflict, "only an approved application can be told about presses")
	}
	if err != nil {
		return webhookError(err)
	}

	s.recordModeration(c, user, audit.ActionApplicationInvocationSet, "application", application.ID.String(),
		map[string]any{"name": application.Name, "url": updated.InvocationUrl})
	return c.JSON(http.StatusOK, invocationResponse{URL: updated.InvocationUrl, Secret: updated.InvocationSecret})
}

type invocationView struct {
	ID         string     `json:"id"`
	EventID    string     `json:"eventId,omitempty"`
	Type       string     `json:"type"`
	State      string     `json:"state"`
	Attempts   int32      `json:"attempts"`
	Created    time.Time  `json:"created"`
	Completed  *time.Time `json:"completed,omitempty"`
	NextTry    *time.Time `json:"nextAttempt,omitempty"`
	LastStatus *int32     `json:"lastStatus,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
}

func (s *Server) handleListInvocations(c *echo.Context) error {
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}
	rows, err := s.queries.ListApplicationInvocations(c.Request().Context(),
		dbgen.ListApplicationInvocationsParams{ApplicationID: &application.ID, Limit: 50})
	if err != nil {
		return err
	}
	views := make([]invocationView, 0, len(rows))
	for _, row := range rows {
		view := invocationView{
			ID: row.ID.String(), Type: row.Kind, State: row.State, Attempts: row.Attempts,
			Created: row.CreatedAt, Completed: row.CompletedAt, LastStatus: row.LastStatus,
		}
		if row.EventID != nil {
			view.EventID = *row.EventID
		}
		if row.State == "pending" {
			next := row.NextAttemptAt
			view.NextTry = &next
		}
		if row.LastError != nil {
			view.LastError = *row.LastError
		}
		views = append(views, view)
	}
	return c.JSON(http.StatusOK, views)
}

/*
queueInvocation hands a press to the application whose message was pressed, if
it asked to be told at an address.

Off the request, as every webhook is: the person pressing never waits on the
application's endpoint. Called only with a press that was recorded — one whose
presser held whatever the control requires — so a press refused for want of a
permission never reaches here and nothing is sent.
*/
func (s *Server) queueInvocation(ctx context.Context, message dbgen.Event, press dbgen.Event) {
	if message.SenderID == nil {
		return
	}
	service := s.webhookService()
	if service == nil {
		return
	}
	accountID := *message.SenderID
	s.inBackground(ctx, "queue an invocation", func(ctx context.Context) {
		application, err := s.queries.GetApplicationByAccount(ctx, &accountID)
		if err != nil {
			return
		}
		if err := service.Invoke(ctx, application, press); err != nil {
			s.log.Error("cannot queue an invocation", "application", application.ID, "error", err)
		}
	})
}

// invocationHost is the host an application's presses are sent to, for
// everybody who can see it is installed: where a press goes is part of what an
// application sends off the server.
func invocationHost(application dbgen.Application) string {
	if application.InvocationUrl == "" {
		return ""
	}
	parsed, err := url.Parse(application.InvocationUrl)
	if err != nil {
		return ""
	}
	return parsed.Host
}
