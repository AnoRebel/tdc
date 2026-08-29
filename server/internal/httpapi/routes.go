package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/buildinfo"
)

// routes mounts the endpoints available before any capability is configured.
//
// Both are deliberately unauthenticated: a client needs the version to decide
// whether it can talk to this server at all, and an operator's uptime check
// needs health without holding a credential.
func (s *Server) routes() {
	s.echo.GET("/api/health", s.handleHealth)
	s.echo.GET("/api/version", s.handleVersion)
}

// healthResponse reports whether the server can serve requests.
type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (s *Server) handleHealth(c *echo.Context) error {
	// Subsystem checks are registered as they are introduced; with none
	// registered, a server that can answer at all is healthy.
	return c.JSON(http.StatusOK, healthResponse{
		Status: "ok",
		Checks: map[string]string{},
	})
}

func (s *Server) handleVersion(c *echo.Context) error {
	return c.JSON(http.StatusOK, buildinfo.Current())
}
