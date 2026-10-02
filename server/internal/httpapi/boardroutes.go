package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
A project's board, and the rest of a column.

Moving a card has no route of its own: it is POST /api/issues/:ref/state with
the state the card was seen in, the same request a move from the issue's own
page makes. So a card dragged, a card moved from the keyboard and a state chosen
from a menu are one code path, and two people moving the same card at once are
settled by the one compare-and-set every move goes through.
*/
func init() {
	registerRoutes(func(s *Server) {
		group := s.echo.Group("/api/projects", requireAuthentication, s.requireProjects)
		group.GET("/:key/board", s.handleBoard)
		group.GET("/:key/board/:state", s.handleBoardColumn)
	})
	documentRoutes(map[string]entry{
		"GET /api/projects/:key/board":        {Summary: "A project's board: each state's first page of cards", Authentication: "session or token"},
		"GET /api/projects/:key/board/:state": {Summary: "More of one board column", Authentication: "session or token"},
	})
}

func (s *Server) handleBoard(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	board, err := s.projects.Board(c.Request().Context(), access, limit)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, board)
}

func (s *Server) handleBoardColumn(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	stateID, err := uuid.Parse(c.Param("state"))
	if err != nil {
		return projectError(projects.ErrStateNotFound)
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	column, err := s.projects.Column(c.Request().Context(), access, stateID, c.QueryParam("after"), limit)
	if errors.Is(err, projects.ErrBadCursor) {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err != nil {
		return projectError(err)
	}
	return c.JSON(http.StatusOK, column)
}
