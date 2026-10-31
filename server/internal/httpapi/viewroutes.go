package httpapi

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/projects"
	"github.com/AnoRebel/tdc/server/internal/search"
)

/*
Saved views, and changing many issues at once.

A view keeps how to ask, never what was found: its results are the search or
the board it describes, run again as whoever opens it. So a view shared with
somebody who may see less than its owner shows them less, by construction —
there is no second check to forget, because there is no stored answer.
*/
func init() {
	registerRoutes(func(s *Server) {
		group := s.echo.Group("/api/projects", requireAuthentication, s.requireProjects)
		group.GET("/:key/views", s.handleListProjectViews)
		group.POST("/:key/views", s.handleSaveProjectView)
		group.POST("/:key/issues/bulk", s.handleBulk)

		views := s.echo.Group("/api/views", requireAuthentication, s.requireProjects)
		views.GET("", s.handleListViews)
		views.POST("", s.handleSaveView)
		views.GET("/:id", s.handleGetView)
		views.PATCH("/:id", s.handleUpdateView)
		views.DELETE("/:id", s.handleDeleteView)
		views.GET("/:id/results", s.handleViewResults)
	})
	documentRoutes(map[string]entry{
		"GET /api/projects/:key/views":        {Summary: "A project's saved views", Authentication: "session or token"},
		"POST /api/projects/:key/views":       {Summary: "Save a view of a project", Authentication: "session or token"},
		"POST /api/projects/:key/issues/bulk": {Summary: "Change many issues at once, each reported", Authentication: "session or token"},
		"GET /api/views":                      {Summary: "Saved views across projects", Authentication: "session or token"},
		"POST /api/views":                     {Summary: "Save a view across projects", Authentication: "session or token"},
		"GET /api/views/:id":                  {Summary: "A saved view", Authentication: "session or token"},
		"PATCH /api/views/:id":                {Summary: "Change a saved view", Authentication: "session or token"},
		"DELETE /api/views/:id":               {Summary: "Delete a saved view", Authentication: "session or token"},
		"GET /api/views/:id/results":          {Summary: "Open a saved view, as the caller", Authentication: "session or token"},
	})
}

func viewError(err error) error {
	switch {
	case errors.Is(err, projects.ErrViewNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, projects.ErrNotViewOwner),
		errors.Is(err, projects.ErrShareNeedsMembership):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, projects.ErrViewName),
		errors.Is(err, projects.ErrViewLayout),
		errors.Is(err, projects.ErrViewFilter),
		errors.Is(err, projects.ErrBulkChange),
		errors.Is(err, projects.ErrBulkEmpty),
		errors.Is(err, projects.ErrBulkTooMany),
		// The cycle a batch names, not any issue in it.
		errors.Is(err, projects.ErrCycleNotFound):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return structureError(err)
}

// viewResponse is a saved view.
type viewResponse struct {
	ID string `json:"id"`
	// Project is the key of the project it views, absent for a view across
	// projects.
	Project   string              `json:"project,omitempty"`
	Name      string              `json:"name"`
	Layout    string              `json:"layout"`
	Filter    projects.ViewFilter `json:"filter"`
	Shared    bool                `json:"shared"`
	Owner     string              `json:"owner"`
	Mine      bool                `json:"mine"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

func viewResponseOf(view projects.View, projectKey string, viewer uuid.UUID) viewResponse {
	return viewResponse{
		ID: view.ID.String(), Project: projectKey, Name: view.Name, Layout: view.Layout,
		Filter: view.Filter, Shared: view.Shared, Owner: view.Owner,
		Mine: view.OwnerID == viewer, UpdatedAt: view.UpdatedAt,
	}
}

type viewRequest struct {
	Name   string              `json:"name"`
	Layout string              `json:"layout"`
	Filter projects.ViewFilter `json:"filter"`
	Shared bool                `json:"shared"`
}

func (r viewRequest) input() projects.NewView {
	layout := r.Layout
	if layout == "" {
		layout = "list"
	}
	return projects.NewView{Name: r.Name, Layout: layout, Filter: r.Filter, Shared: r.Shared}
}

func (s *Server) handleListProjectViews(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	views, err := s.projects.Views(c.Request().Context(), actorOf(c), &access)
	if err != nil {
		return err
	}
	user, _ := currentUser(c)
	response := make([]viewResponse, 0, len(views))
	for _, view := range views {
		response = append(response, viewResponseOf(view, access.Project.Key, user.ID))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleSaveProjectView(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request viewRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	view, err := s.projects.SaveView(c.Request().Context(), actorOf(c), &access, request.input())
	if err != nil {
		return viewError(err)
	}
	user, _ := currentUser(c)
	return c.JSON(http.StatusCreated, viewResponseOf(view, access.Project.Key, user.ID))
}

func (s *Server) handleListViews(c *echo.Context) error {
	views, err := s.projects.Views(c.Request().Context(), actorOf(c), nil)
	if err != nil {
		return err
	}
	user, _ := currentUser(c)
	response := make([]viewResponse, 0, len(views))
	for _, view := range views {
		response = append(response, viewResponseOf(view, "", user.ID))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleSaveView(c *echo.Context) error {
	var request viewRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	view, err := s.projects.SaveView(c.Request().Context(), actorOf(c), nil, request.input())
	if err != nil {
		return viewError(err)
	}
	user, _ := currentUser(c)
	return c.JSON(http.StatusCreated, viewResponseOf(view, "", user.ID))
}

// openView resolves the view in the path for the caller, or answers that
// there is none.
func (s *Server) openView(c *echo.Context) (projects.View, *projects.Access, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return projects.View{}, nil, viewError(projects.ErrViewNotFound)
	}
	view, access, err := s.projects.OpenView(c.Request().Context(), actorOf(c), id)
	if err != nil {
		return projects.View{}, nil, viewError(err)
	}
	return view, access, nil
}

func keyOf(access *projects.Access) string {
	if access == nil {
		return ""
	}
	return access.Project.Key
}

func (s *Server) handleGetView(c *echo.Context) error {
	view, access, err := s.openView(c)
	if err != nil {
		return err
	}
	user, _ := currentUser(c)
	return c.JSON(http.StatusOK, viewResponseOf(view, keyOf(access), user.ID))
}

func (s *Server) handleUpdateView(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return viewError(projects.ErrViewNotFound)
	}
	var request viewRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	view, err := s.projects.UpdateView(c.Request().Context(), actorOf(c), id, request.input())
	if err != nil {
		return viewError(err)
	}
	_, access, err := s.openView(c)
	if err != nil {
		return err
	}
	user, _ := currentUser(c)
	return c.JSON(http.StatusOK, viewResponseOf(view, keyOf(access), user.ID))
}

func (s *Server) handleDeleteView(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return viewError(projects.ErrViewNotFound)
	}
	if err := s.projects.DeleteView(c.Request().Context(), actorOf(c), id); err != nil {
		return viewError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

/*
handleViewResults opens a view as the caller: a board view is its project's
board, filtered; a list view is a search, in its project or across every
project the caller can see. The caller's own access decides what is in it.
*/
func (s *Server) handleViewResults(c *echo.Context) error {
	ctx := c.Request().Context()
	view, access, err := s.openView(c)
	if err != nil {
		return err
	}
	user, _ := currentUser(c)
	described := viewResponseOf(view, keyOf(access), user.ID)

	assignee := func() (*uuid.UUID, bool) {
		if view.Filter.Assignee == "" {
			return nil, true
		}
		person, err := s.queries.GetUserByUsername(ctx, identity.NormaliseUsername(view.Filter.Assignee))
		if err != nil {
			return nil, false
		}
		return &person.ID, true
	}

	if view.Layout == "board" && access != nil {
		filter := projects.BoardFilter{Label: view.Filter.Label, Priority: view.Filter.Priority}
		id, ok := assignee()
		if !ok {
			nobody := uuid.Nil()
			id = &nobody
		}
		filter.Assignee = id
		board, err := s.projects.Board(ctx, *access, filter, 0)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]any{"view": described, "board": board})
	}

	query := search.IssueQuery{
		Text: view.Filter.Q, State: view.Filter.State, Label: view.Filter.Label,
		Priority: view.Filter.Priority,
	}
	if access != nil {
		query.Project = &access.Project.ID
	}
	id, ok := assignee()
	issues := []issueSearchResult{}
	if ok {
		query.Assignee = id
		found, err := s.search.SearchIssues(ctx, user.ID, query)
		if err != nil {
			return err
		}
		issues = searchResults(found)
	}
	return c.JSON(http.StatusOK, map[string]any{"view": described, "issues": issues})
}

/*
handleBulk makes one change to many issues of one project, and reports on
every one.

200 whatever happened to the individual issues: the request was understood
and carried out, and what became of each issue is in the report. A request the
server could not make sense of at all — a change that is not one, a state that
is not this project's — is refused as a whole before anything is touched.
*/
func (s *Server) handleBulk(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		Issues []string            `json:"issues"`
		Change projects.BulkChange `json:"change"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	results, err := s.projects.Bulk(c.Request().Context(), actorOf(c), access, request.Issues, request.Change)
	if err != nil {
		return viewError(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}
