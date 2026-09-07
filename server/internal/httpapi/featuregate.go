package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
requireFeature refuses a request for a capability an operator has turned off.

404 rather than 403. A disabled feature is not something the caller lacks
permission for — it is something this server does not offer, and 403 would
invite them to go and ask for a permission that would not help. The client is
told the same thing by /api/server, which does not list it: the refusal and the
advertisement agree because both read the same toggle.

Registered as middleware rather than checked inside each handler so that a new
route cannot be added without the gate. A handler that forgot the check would
be a feature that is advertised as off and works anyway, which is worse than
one that was never disabled.
*/
func (s *Server) requireFeature(feature settings.Feature) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if s.settings == nil || !s.settings.Enabled(c.Request().Context(), feature) {
				return echo.NewHTTPError(http.StatusNotFound,
					"this server does not offer that")
			}
			return next(c)
		}
	}
}
