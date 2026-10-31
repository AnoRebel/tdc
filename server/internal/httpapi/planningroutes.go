package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
Cycles, milestones, and the calendar they are on.

Every date here is a day — "2026-10-18" — as the team chose it, and every
instant is that day's beginning or end in the project's zone, sent with its
offset. A reader anywhere gets both: the day to show, and the moment it is
over, without working either out from the other in their own zone.
*/
func init() {
	registerRoutes(func(s *Server) {
		group := s.echo.Group("/api/projects", requireAuthentication, s.requireProjects)
		group.PUT("/:key/planning", s.handleSetPlanning)
		group.GET("/:key/cycles", s.handleListCycles)
		group.POST("/:key/cycles", s.handleCreateCycle)
		group.GET("/:key/cycles/:cycle", s.handleGetCycle)
		group.PATCH("/:key/cycles/:cycle", s.handleUpdateCycle)
		group.DELETE("/:key/cycles/:cycle", s.handleDeleteCycle)
		group.GET("/:key/milestones", s.handleListMilestones)
		group.POST("/:key/milestones", s.handleCreateMilestone)
		group.GET("/:key/milestones/:milestone", s.handleGetMilestone)
		group.PATCH("/:key/milestones/:milestone", s.handleUpdateMilestone)
		group.DELETE("/:key/milestones/:milestone", s.handleDeleteMilestone)

		issues := s.echo.Group("/api/issues", requireAuthentication, s.requireProjects)
		issues.PUT("/:ref/cycle", s.handleSetIssueCycle)
		issues.PUT("/:ref/milestone", s.handleSetIssueMilestone)
	})
	documentRoutes(map[string]entry{
		"PUT /api/projects/:key/planning":                 {Summary: "Set a project's time zone and what happens to unfinished work when a cycle ends", Authentication: "session or token", Permission: "project.administer"},
		"GET /api/projects/:key/cycles":                   {Summary: "A project's cycles, with how much of each is done", Authentication: "session or token"},
		"POST /api/projects/:key/cycles":                  {Summary: "Make a cycle", Authentication: "session or token", Permission: "project.plan"},
		"GET /api/projects/:key/cycles/:cycle":            {Summary: "A cycle and the issues in it", Authentication: "session or token"},
		"PATCH /api/projects/:key/cycles/:cycle":          {Summary: "Rename a cycle or change its days", Authentication: "session or token", Permission: "project.plan"},
		"DELETE /api/projects/:key/cycles/:cycle":         {Summary: "Remove a cycle nothing is in", Authentication: "session or token", Permission: "project.plan"},
		"GET /api/projects/:key/milestones":               {Summary: "A project's milestones, with how much of each is done", Authentication: "session or token"},
		"POST /api/projects/:key/milestones":              {Summary: "Make a milestone", Authentication: "session or token", Permission: "project.plan"},
		"GET /api/projects/:key/milestones/:milestone":    {Summary: "A milestone, its issues, and the cycles they are in", Authentication: "session or token"},
		"PATCH /api/projects/:key/milestones/:milestone":  {Summary: "Change a milestone", Authentication: "session or token", Permission: "project.plan"},
		"DELETE /api/projects/:key/milestones/:milestone": {Summary: "Remove a milestone nothing counts towards", Authentication: "session or token", Permission: "project.plan"},
		"PUT /api/issues/:ref/cycle":                      {Summary: "Put an issue into a cycle, or take it out", Authentication: "session or token", Permission: "issue.plan"},
		"PUT /api/issues/:ref/milestone":                  {Summary: "Count an issue towards a milestone, or none", Authentication: "session or token", Permission: "issue.plan"},
	})
}

func planningError(err error) error {
	switch {
	case errors.Is(err, projects.ErrCycleNotFound),
		errors.Is(err, projects.ErrMilestoneNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, projects.ErrCycleOverlap),
		errors.Is(err, projects.ErrCycleInUse):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, projects.ErrTimeZone),
		errors.Is(err, projects.ErrCyclePolicy),
		errors.Is(err, projects.ErrCycleName),
		errors.Is(err, projects.ErrCycleDates),
		errors.Is(err, projects.ErrMilestoneName),
		errors.Is(err, projects.ErrDate):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return projectError(err)
}

// planRef names a cycle or milestone, as an issue mentions it.
type planRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// cycleResponse is a cycle: its days, those days as instants in the project's
// zone, and what is in it now.
type cycleResponse struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	StartsOn string    `json:"startsOn"`
	EndsOn   string    `json:"endsOn"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	// Status is "upcoming", "active" or "ended".
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	// When its end was dealt with, and under which policy.
	ClosedAt     *time.Time `json:"closedAt,omitempty"`
	ClosedPolicy string     `json:"closedPolicy,omitempty"`
}

func cycleResponseOf(cycle projects.Cycle, zone *time.Location) cycleResponse {
	response := cycleResponse{
		ID: cycle.ID.String(), Name: cycle.Name,
		StartsAt: cycle.StartsAt.In(zone), EndsAt: cycle.EndsAt.In(zone),
		Status: cycle.Status, Total: cycle.Total, Completed: cycle.Completed,
	}
	if day, ok := projects.DayOf(cycle.StartsOn); ok {
		response.StartsOn = day.String()
	}
	if day, ok := projects.DayOf(cycle.EndsOn); ok {
		response.EndsOn = day.String()
	}
	if cycle.ClosedAt != nil {
		closed := cycle.ClosedAt.In(zone)
		response.ClosedAt = &closed
	}
	if cycle.ClosedPolicy != nil {
		response.ClosedPolicy = *cycle.ClosedPolicy
	}
	return response
}

// milestoneResponse is a milestone and how far along it is.
type milestoneResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// The day it is meant to be reached by, and the instant that day is over.
	TargetDate string     `json:"targetDate,omitempty"`
	DueAt      *time.Time `json:"dueAt,omitempty"`
	// Overdue: its day has passed with work unfinished. Nothing else changes.
	Overdue   bool `json:"overdue"`
	Total     int  `json:"total"`
	Completed int  `json:"completed"`
}

func milestoneResponseOf(milestone projects.Milestone, zone *time.Location) milestoneResponse {
	response := milestoneResponse{
		ID: milestone.ID.String(), Name: milestone.Name, Description: milestone.Description,
		Overdue: milestone.Overdue, Total: milestone.Total, Completed: milestone.Completed,
	}
	if day, ok := projects.DayOf(milestone.TargetDate); ok {
		response.TargetDate = day.String()
	}
	if milestone.DueAt != nil {
		due := milestone.DueAt.In(zone)
		response.DueAt = &due
	}
	return response
}

func (s *Server) handleSetPlanning(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		TimeZone    string `json:"timeZone"`
		CyclePolicy string `json:"cyclePolicy"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	updated, err := s.projects.SetPlanning(c.Request().Context(), actorOf(c), access, request.TimeZone, request.CyclePolicy)
	if err != nil {
		return planningError(err)
	}
	access.Project = updated
	response, err := s.fullProject(c.Request().Context(), access)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleListCycles(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	cycles, err := s.projects.Cycles(c.Request().Context(), access, time.Now())
	if err != nil {
		return err
	}
	zone := projects.Zone(access.Project)
	response := make([]cycleResponse, 0, len(cycles))
	for _, cycle := range cycles {
		response = append(response, cycleResponseOf(cycle, zone))
	}
	return c.JSON(http.StatusOK, response)
}

type cycleRequest struct {
	Name     string `json:"name"`
	StartsOn string `json:"startsOn"`
	EndsOn   string `json:"endsOn"`
}

func (s *Server) respondWithCycle(c *echo.Context, access projects.Access, id uuid.UUID, status int) error {
	cycle, err := s.projects.CycleByID(c.Request().Context(), access, id, time.Now())
	if err != nil {
		return planningError(err)
	}
	return c.JSON(status, cycleResponseOf(cycle, projects.Zone(access.Project)))
}

func (s *Server) handleCreateCycle(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request cycleRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	created, err := s.projects.CreateCycle(c.Request().Context(), actorOf(c), access,
		projects.NewCycle{Name: request.Name, StartsOn: request.StartsOn, EndsOn: request.EndsOn})
	if err != nil {
		return planningError(err)
	}
	return s.respondWithCycle(c, access, created.ID, http.StatusCreated)
}

func pathID(c *echo.Context, name string, missing error) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil(), planningError(missing)
	}
	return id, nil
}

// issuesOf renders issues as the list does, for a cycle or milestone.
func (s *Server) issuesOf(ctx context.Context, project dbgen.Project, issues []dbgen.Issue) ([]issueResponse, error) {
	if len(issues) == 0 {
		return []issueResponse{}, nil
	}
	return s.issueSummaries(ctx, project, issues)
}

func (s *Server) handleGetCycle(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "cycle", projects.ErrCycleNotFound)
	if err != nil {
		return err
	}
	cycle, err := s.projects.CycleByID(ctx, access, id, time.Now())
	if err != nil {
		return planningError(err)
	}
	inside, err := s.projects.IssuesInCycle(ctx, id)
	if err != nil {
		return err
	}
	issues, err := s.issuesOf(ctx, access.Project, inside)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"cycle": cycleResponseOf(cycle, projects.Zone(access.Project)), "issues": issues,
	})
}

func (s *Server) handleUpdateCycle(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "cycle", projects.ErrCycleNotFound)
	if err != nil {
		return err
	}
	var request cycleRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if _, err := s.projects.UpdateCycle(c.Request().Context(), actorOf(c), access, id,
		projects.NewCycle{Name: request.Name, StartsOn: request.StartsOn, EndsOn: request.EndsOn}); err != nil {
		return planningError(err)
	}
	return s.respondWithCycle(c, access, id, http.StatusOK)
}

func (s *Server) handleDeleteCycle(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "cycle", projects.ErrCycleNotFound)
	if err != nil {
		return err
	}
	if err := s.projects.DeleteCycle(c.Request().Context(), actorOf(c), access, id); err != nil {
		return planningError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleListMilestones(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	milestones, err := s.projects.Milestones(c.Request().Context(), access, time.Now())
	if err != nil {
		return err
	}
	zone := projects.Zone(access.Project)
	response := make([]milestoneResponse, 0, len(milestones))
	for _, milestone := range milestones {
		response = append(response, milestoneResponseOf(milestone, zone))
	}
	return c.JSON(http.StatusOK, response)
}

type milestoneRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TargetDate  string `json:"targetDate"`
}

func (r milestoneRequest) input() projects.NewMilestone {
	return projects.NewMilestone{Name: r.Name, Description: r.Description, TargetDate: r.TargetDate}
}

func (s *Server) respondWithMilestone(c *echo.Context, access projects.Access, id uuid.UUID, status int) error {
	milestone, err := s.projects.MilestoneByID(c.Request().Context(), access, id, time.Now())
	if err != nil {
		return planningError(err)
	}
	return c.JSON(status, milestoneResponseOf(milestone, projects.Zone(access.Project)))
}

func (s *Server) handleCreateMilestone(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request milestoneRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	created, err := s.projects.CreateMilestone(c.Request().Context(), actorOf(c), access, request.input())
	if err != nil {
		return planningError(err)
	}
	return s.respondWithMilestone(c, access, created.ID, http.StatusCreated)
}

func (s *Server) handleGetMilestone(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "milestone", projects.ErrMilestoneNotFound)
	if err != nil {
		return err
	}
	now := time.Now()
	milestone, err := s.projects.MilestoneByID(ctx, access, id, now)
	if err != nil {
		return planningError(err)
	}
	inside, err := s.projects.IssuesInMilestone(ctx, id)
	if err != nil {
		return err
	}
	issues, err := s.issuesOf(ctx, access.Project, inside)
	if err != nil {
		return err
	}
	spanned, err := s.projects.MilestoneCycles(ctx, id)
	if err != nil {
		return err
	}
	zone := projects.Zone(access.Project)
	cycles := make([]cycleResponse, 0, len(spanned))
	for _, cycle := range spanned {
		full, err := s.projects.CycleByID(ctx, access, cycle.ID, now)
		if err != nil {
			return err
		}
		cycles = append(cycles, cycleResponseOf(full, zone))
	}
	return c.JSON(http.StatusOK, map[string]any{
		"milestone": milestoneResponseOf(milestone, zone), "issues": issues, "cycles": cycles,
	})
}

func (s *Server) handleUpdateMilestone(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "milestone", projects.ErrMilestoneNotFound)
	if err != nil {
		return err
	}
	var request milestoneRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if _, err := s.projects.UpdateMilestone(c.Request().Context(), actorOf(c), access, id, request.input()); err != nil {
		return planningError(err)
	}
	return s.respondWithMilestone(c, access, id, http.StatusOK)
}

func (s *Server) handleDeleteMilestone(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "milestone", projects.ErrMilestoneNotFound)
	if err != nil {
		return err
	}
	if err := s.projects.DeleteMilestone(c.Request().Context(), actorOf(c), access, id); err != nil {
		return planningError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// optionalID reads an identifier that may be null, meaning none.
func optionalID(value *string, missing error) (*uuid.UUID, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return nil, missing
	}
	return &id, nil
}

func (s *Server) handleSetIssueCycle(c *echo.Context) error {
	ctx := c.Request().Context()
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		CycleID *string `json:"cycleId"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	id, err := optionalID(request.CycleID, projects.ErrCycleNotFound)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if _, err := s.projects.SetIssueCycle(ctx, actorOf(c), access, issue, id); err != nil {
		if errors.Is(err, projects.ErrCycleNotFound) {
			// The issue exists; the cycle it names does not.
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return planningError(err)
	}
	response, err := s.fullIssue(ctx, c, access, issue)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleSetIssueMilestone(c *echo.Context) error {
	ctx := c.Request().Context()
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		MilestoneID *string `json:"milestoneId"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	id, err := optionalID(request.MilestoneID, projects.ErrMilestoneNotFound)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if _, err := s.projects.SetIssueMilestone(ctx, actorOf(c), access, issue, id); err != nil {
		if errors.Is(err, projects.ErrMilestoneNotFound) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return planningError(err)
	}
	response, err := s.fullIssue(ctx, c, access, issue)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}
