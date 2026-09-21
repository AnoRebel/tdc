package httpapi

import (
	"net/netip"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/apitokens"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Keys under which per-request values are stored on the Echo context.
//
// Unexported so that only this package can set them: a handler must not be
// able to fabricate an identity by writing to the context itself.
const (
	contextKeyUser        = "tdc.user"
	contextKeySession     = "tdc.session"
	contextKeyPermissions = "tdc.permissions"
	// contextKeyAPIToken marks a request made by a program rather than a
	// person, and names which token. Absent for a session.
	contextKeyAPIToken = "tdc.apiToken"
)

/*
currentAPIToken returns the token this request was made with, if it was made by
a program rather than a person.

Absent for a session, which is what distinguishes the two at the point where it
matters: an action a person may take interactively is not always one a program
should be able to take unattended.
*/
func currentAPIToken(c *echo.Context) (apitokens.Token, bool) {
	token, ok := c.Get(contextKeyAPIToken).(apitokens.Token)
	return token, ok
}

// currentUser returns the authenticated user, if the request carried a valid
// session.
func currentUser(c *echo.Context) (dbgen.User, bool) {
	user, ok := c.Get(contextKeyUser).(dbgen.User)
	return user, ok
}

// currentSession returns the session the request was made with.
func currentSession(c *echo.Context) (dbgen.Session, bool) {
	session, ok := c.Get(contextKeySession).(dbgen.Session)
	return session, ok
}

// currentPermissions returns the requester's resolved authority.
//
// Resolved once per request and consulted many times: a request typically
// tests several permissions, and querying per check would be a round trip
// each.
func currentPermissions(c *echo.Context) authz.Permissions {
	permissions, ok := c.Get(contextKeyPermissions).(authz.Permissions)
	if !ok {
		// An unauthenticated request holds nothing. Returning an empty set
		// rather than an error means every check refuses by default.
		return authz.Resolve(nil)
	}
	return permissions
}

// requestIP returns the client's address.
//
// Echo's extractor is used because it applies the operator's configured trust
// policy: taking a forwarded header at face value would let any caller forge
// their address and so evade a per-address rate limit.
func requestIP(c *echo.Context) netip.Addr {
	address, err := netip.ParseAddr(c.RealIP())
	if err != nil {
		// Fall back to the transport address, stripping any port.
		host := c.Request().RemoteAddr
		if index := strings.LastIndex(host, ":"); index > 0 {
			host = host[:index]
		}
		if parsed, parseErr := netip.ParseAddr(strings.Trim(host, "[]")); parseErr == nil {
			return parsed
		}
		return netip.Addr{}
	}
	return address
}
