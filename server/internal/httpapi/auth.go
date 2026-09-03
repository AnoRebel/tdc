package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// sessionCookieName is where a browser client keeps its session token.
const sessionCookieName = "tdc_session"

// bearerPrefix is how a non-browser client presents the same token.
const bearerPrefix = "Bearer "

// authenticate resolves the request's session, if it carries one.
//
// It does not refuse an unauthenticated request: some endpoints are public,
// and refusing here would mean every public route had to opt out. Requiring
// authentication is the job of requireAuthentication.
func (s *Server) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		token := sessionToken(c)
		if token == "" {
			return next(c)
		}

		user, session, err := s.identity.AuthenticatedUser(c.Request().Context(), token)
		if err != nil {
			if errors.Is(err, identity.ErrSessionInvalid) {
				// An expired or revoked session is not an error the caller
				// needs explained; the request simply proceeds as
				// unauthenticated and is refused by whatever needs authority.
				// The stale cookie is cleared so the browser stops sending it.
				clearSessionCookie(c, s.config.SecureCookies)
				return next(c)
			}
			return err
		}

		permissions, err := s.authz.PermissionsFor(c.Request().Context(), user.ID)
		if err != nil {
			return err
		}

		c.Set(contextKeyUser, user)
		c.Set(contextKeySession, session)
		c.Set(contextKeyPermissions, permissions)

		return next(c)
	}
}

// sessionToken extracts the session token from the request.
//
// A cookie is what a browser sends; a bearer header is what an integration or
// the desktop client sends. Both name the same token.
func sessionToken(c *echo.Context) string {
	if header := c.Request().Header.Get(echo.HeaderAuthorization); strings.HasPrefix(header, bearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))
	}
	if cookie, err := c.Request().Cookie(sessionCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// requireAuthentication refuses a request that carries no valid session.
func requireAuthentication(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if _, ok := currentUser(c); !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "authentication is required")
		}
		return next(c)
	}
}

// requirePermission refuses a request whose author lacks a permission at
// server scope.
//
// Enforcement happens here, on the server, for every request. A client hiding
// an interface element is a courtesy to the user, never the thing that stops
// the action.
func requirePermission(permission authz.Permission) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if _, ok := currentUser(c); !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication is required")
			}

			decision := currentPermissions(c).AllowsServer(permission)
			if !decision.Allowed {
				// The refusal names the permission that would have allowed
				// it, so a user can ask for the right thing rather than
				// guessing.
				return echo.NewHTTPError(http.StatusForbidden, decision.Error())
			}

			return next(c)
		}
	}
}

// setSessionCookie stores a session token in the browser.
//
// HttpOnly keeps it away from scripts, so an injected script cannot read it;
// SameSite=Lax stops it riding along on a cross-site request; Secure is set
// wherever the deployment is served over TLS.
func setSessionCookie(c *echo.Context, token string, secure bool, maxAgeSeconds int) {
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAgeSeconds,
	})
}

// clearSessionCookie removes the session cookie.
func clearSessionCookie(c *echo.Context, secure bool) {
	http.SetCookie(c.Response(), &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
