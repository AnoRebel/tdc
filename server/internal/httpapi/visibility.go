package httpapi

import (
	"context"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
How discoverable this server is.

The same three-way choice a room has, one level up. An operator running a
server for a particular community decides who may find it at all, and the
server enforces that rather than trusting whatever is asking.

What this governs is discovery, not access. Somebody holding an invitation
reaches a private server perfectly well — being undiscoverable is not being
unreachable, and conflating the two would mean a private server nobody could
join.
*/

// Visibility is how discoverable this server is.
type Visibility string

const (
	// VisibilityPublic lets anybody find this server and read what it says
	// about itself.
	VisibilityPublic Visibility = "public"
	// VisibilityLimited answers only the peers an operator has listed.
	VisibilityLimited Visibility = "limited"
	// VisibilityPrivate answers nobody: the server is reachable only by
	// somebody who already knows where it is.
	VisibilityPrivate Visibility = "private"
)

/*
visibility reports this server's setting.

Public when it cannot be read. A server that made itself undiscoverable because
a settings query timed out would drop off the network for a reason nobody could
see, where the opposite failure is visible and correctable.
*/
func (s *Server) visibility(ctx context.Context) Visibility {
	if s.settings == nil {
		return VisibilityPublic
	}

	switch Visibility(s.settings.Get(ctx, settings.ServerVisibility)) {
	case VisibilityLimited:
		return VisibilityLimited
	case VisibilityPrivate:
		return VisibilityPrivate
	default:
		return VisibilityPublic
	}
}

/*
mayDiscover reports whether this caller may learn the server exists.

Somebody signed in always may: they are already here, and hiding a server from
its own members would be absurd. The question is only what an unauthenticated
stranger is told.

A limited server answers the peers its operator listed, identified by the
server they are asking on behalf of. There is no federation yet, so that header
is not something a peer can currently set — which is documented rather than
pretended otherwise: the listing is enforced, and nothing legitimately populates
it until peers exist.
*/
func (s *Server) mayDiscover(c *echo.Context) bool {
	ctx := c.Request().Context()

	// Anybody with a session is past the question.
	if _, signedIn := currentUser(c); signedIn {
		return true
	}

	switch s.visibility(ctx) {
	case VisibilityPublic:
		return true

	case VisibilityPrivate:
		return false

	case VisibilityLimited:
		asking := strings.TrimSpace(c.Request().Header.Get("TDC-Peer-Server"))
		if asking == "" {
			return false
		}
		return s.listsPeer(ctx, asking)

	default:
		return true
	}
}

// listsPeer reports whether a server is one this operator admits.
func (s *Server) listsPeer(ctx context.Context, peer string) bool {
	if s.settings == nil {
		return false
	}

	for _, listed := range splitList(s.settings.Get(ctx, settings.VisibilityAllowedServers)) {
		// Compared without regard to case, since a domain is not case
		// sensitive and an operator typing one capitalised means the same
		// server.
		if strings.EqualFold(listed, peer) {
			return true
		}
	}
	return false
}
