package httpapi

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
)

// cors permits browser clients served from another origin to call this API.
//
// Origins are listed explicitly by the operator rather than reflected from the
// request: reflecting an arbitrary Origin together with credentials would let
// any site read authenticated responses on a user's behalf. The list is empty
// by default, which is correct for the normal deployment where the web app is
// served from the same origin as the API.
func cors(allowedOrigins []string) echo.MiddlewareFunc {
	allowedHeaders := strings.Join([]string{
		echo.HeaderOrigin,
		echo.HeaderContentType,
		echo.HeaderAccept,
		echo.HeaderAuthorization,
	}, ", ")

	allowedMethods := strings.Join([]string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPatch,
		http.MethodPut,
		http.MethodDelete,
		http.MethodOptions,
	}, ", ")

	const preflightMaxAge = 3600 // seconds

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			origin := c.Request().Header.Get(echo.HeaderOrigin)
			header := c.Response().Header()

			// Caches must not serve one origin's response to another.
			header.Add(echo.HeaderVary, echo.HeaderOrigin)

			if origin != "" && slices.Contains(allowedOrigins, origin) {
				header.Set(echo.HeaderAccessControlAllowOrigin, origin)
				header.Set(echo.HeaderAccessControlAllowCredentials, "true")

				if c.Request().Method == http.MethodOptions {
					header.Set(echo.HeaderAccessControlAllowMethods, allowedMethods)
					header.Set(echo.HeaderAccessControlAllowHeaders, allowedHeaders)
					header.Set(echo.HeaderAccessControlMaxAge, strconv.Itoa(preflightMaxAge))
					return c.NoContent(http.StatusNoContent)
				}
			}

			// A preflight for a disallowed origin is answered without the
			// permitting headers, so the browser refuses the real request.
			if c.Request().Method == http.MethodOptions && origin != "" {
				return c.NoContent(http.StatusNoContent)
			}

			return next(c)
		}
	}
}
