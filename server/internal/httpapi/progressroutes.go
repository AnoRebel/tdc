package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
How far along a project, a cycle or a milestone is.

Counted on every request and never cached: a move made a moment ago is in the
next answer, because there is no stored figure to wait for. At this scale —
hundreds of issues, not millions — counting is cheaper than keeping a count
honest.
*/
func init() {
	registerRoutes(func(s *Server) {
		group := s.echo.Group("/api/projects", requireAuthentication, s.requireProjects)
		group.GET("/:key/progress", s.handleProjectProgress)
		group.GET("/:key/cycles/:cycle/progress", s.handleCycleProgress)
		group.GET("/:key/milestones/:milestone/progress", s.handleMilestoneProgress)
	})
	documentRoutes(map[string]entry{
		"GET /api/projects/:key/progress":                       {Summary: "How much of a project is done, now and day by day", Authentication: "session or token"},
		"GET /api/projects/:key/cycles/:cycle/progress":         {Summary: "How much of a cycle is done, now and day by day", Authentication: "session or token"},
		"GET /api/projects/:key/milestones/:milestone/progress": {Summary: "How much of a milestone is done, now and day by day", Authentication: "session or token"},
	})
}

func progressError(err error) error {
	if errors.Is(err, projects.ErrProgressPeriod) {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return planningError(err)
}

/*
period reads from and to, days in the project's calendar, falling back to the
given defaults.
*/
func period(c *echo.Context, from, to projects.Day) (projects.Day, projects.Day, error) {
	if text := c.QueryParam("from"); text != "" {
		day, err := projects.ParseDay(text)
		if err != nil {
			return from, to, echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		from = day
	}
	if text := c.QueryParam("to"); text != "" {
		day, err := projects.ParseDay(text)
		if err != nil {
			return from, to, echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		to = day
	}
	return from, to, nil
}

func dayOfInstant(instant time.Time, access projects.Access) projects.Day {
	local := instant.In(projects.Zone(access.Project))
	return projects.Day{Year: local.Year(), Month: local.Month(), Date: local.Day()}
}

func (s *Server) handleProjectProgress(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	now := time.Now()
	// The last two weeks, unless asked otherwise.
	from, to, err := period(c, dayOfInstant(now.AddDate(0, 0, -13), access), dayOfInstant(now, access))
	if err != nil {
		return err
	}
	progress, err := s.projects.Progress(c.Request().Context(), access, projects.ProgressScope{}, from, to, now)
	if err != nil {
		return progressError(err)
	}
	return c.JSON(http.StatusOK, progress)
}

func (s *Server) handleCycleProgress(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "cycle", projects.ErrCycleNotFound)
	if err != nil {
		return err
	}
	now := time.Now()
	cycle, err := s.projects.CycleByID(ctx, access, id, now)
	if err != nil {
		return planningError(err)
	}
	// The cycle's own days.
	starts, _ := projects.DayOf(cycle.StartsOn)
	ends, _ := projects.DayOf(cycle.EndsOn)
	from, to, err := period(c, starts, ends)
	if err != nil {
		return err
	}
	if today := dayOfInstant(now, access); today.String() < from.String() {
		// Not begun: nothing to have happened yet, but the figures still.
		from, to = today, today
	}
	progress, err := s.projects.Progress(ctx, access, projects.ProgressScope{Cycle: &id}, from, to, now)
	if err != nil {
		return progressError(err)
	}
	return c.JSON(http.StatusOK, progress)
}

func (s *Server) handleMilestoneProgress(c *echo.Context) error {
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
	// From the day it was made, at most a year back.
	made := milestone.CreatedAt
	if yearAgo := now.AddDate(-1, 0, 1); made.Before(yearAgo) {
		made = yearAgo
	}
	from, to, err := period(c, dayOfInstant(made, access), dayOfInstant(now, access))
	if err != nil {
		return err
	}
	progress, err := s.projects.Progress(ctx, access, projects.ProgressScope{Milestone: &id}, from, to, now)
	if err != nil {
		return progressError(err)
	}
	return c.JSON(http.StatusOK, progress)
}
