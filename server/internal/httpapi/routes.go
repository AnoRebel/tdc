package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/buildinfo"
)

// routes mounts the endpoints available before any capability is configured.
//
// All three are deliberately unauthenticated: a client needs the server's
// identity and version before it can sign in, and an operator's uptime check
// holds no credential.
func (s *Server) routes() {
	s.echo.GET("/api/health", s.handleHealth)
	s.echo.GET("/api/version", s.handleVersion)
	s.echo.GET("/api/server", s.handleServerInfo)
}

// healthResponse reports whether the server can serve requests.
type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

func (s *Server) handleHealth(c *echo.Context) error {
	checks := map[string]string{}
	status := statusOK

	if s.db != nil {
		if err := s.db.Health(c.Request().Context()); err != nil {
			// The reason is logged, not returned: an unauthenticated caller
			// should learn that the server is unhealthy, not why.
			s.log.Error("database health check failed", "error", err)
			checks["database"] = statusUnavailable
			status = statusUnavailable
		} else {
			checks["database"] = statusOK
		}
	}

	code := http.StatusOK
	if status != statusOK {
		// Report a failing status so a load balancer or uptime check takes
		// this instance out of rotation rather than sending it traffic.
		code = http.StatusServiceUnavailable
	}

	return c.JSON(code, healthResponse{Status: status, Checks: checks})
}

func (s *Server) handleVersion(c *echo.Context) error {
	return c.JSON(http.StatusOK, buildinfo.Current())
}

// serverInfoResponse is what a server tells an unauthenticated caller about
// itself. It mirrors the ServerInfo type shared with the clients.
type serverInfoResponse struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	CanonicalAddress string   `json:"canonicalAddress"`
	Version          string   `json:"version"`
	ProtocolVersions []string `json:"protocolVersions"`
	Capabilities     []string `json:"capabilities"`
	DefaultLanguage  string   `json:"defaultLanguage"`
}

func (s *Server) handleServerInfo(c *echo.Context) error {
	return c.JSON(http.StatusOK, serverInfoResponse{
		Name:             s.config.ServerName,
		Description:      s.config.ServerDescription,
		CanonicalAddress: s.config.CanonicalAddress,
		Version:          buildinfo.Version,
		// Federation is not implemented, so no protocol version is advertised.
		ProtocolVersions: []string{},
		// Optional capabilities are advertised as they are built, so a client
		// adapts to what this server actually offers.
		Capabilities:    []string{},
		DefaultLanguage: s.config.DefaultLanguage,
	})
}
