package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
mayPost reports why somebody cannot post in a channel, or nil.

Two separate rules that both stop a message. A ban keeps somebody out of the
channel entirely; a restriction leaves them in it, reading, and silenced. The
distinction matters to the person: one says leave, the other says wait.

Both are told plainly. Unlike a blocked direct message, where the refusal must
not disclose why, somebody restricted in a channel needs to know they are —
otherwise they will simply keep trying, and a moderator's decision that nobody
understands changes no behaviour.
*/
func (s *Server) mayPost(ctx context.Context, roomID, userID uuid.UUID) error {
	if s.moderation == nil {
		return nil
	}

	if s.moderation.IsBanned(ctx, roomID, userID) {
		return echo.NewHTTPError(http.StatusForbidden,
			"you are banned from this channel")
	}

	if s.moderation.IsPostingRestricted(ctx, userID, roomID) {
		return echo.NewHTTPError(http.StatusForbidden,
			"you cannot post in this channel at the moment")
	}

	return nil
}

/*
requirePosting refuses a request that would put words into the channel the
route names, from somebody who may not.

channel.post and mayPost together, on every route that does it — sending,
replying, editing, scheduling — rather than in each handler. The handler that
forgets is the way round a restriction: a reply in a thread is as audible as a
message, and an edit can replace a word with a paragraph.
*/
func (s *Server) requirePosting() echo.MiddlewareFunc {
	permission := requireChannelPermission(authz.PermChannelPost)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return permission(func(c *echo.Context) error {
			user, _ := currentUser(c)
			roomID, err := uuid.Parse(c.Param("id"))
			if err != nil {
				return next(c)
			}
			if err := s.mayPost(c.Request().Context(), roomID, user.ID); err != nil {
				return err
			}
			return next(c)
		})
	}
}

/*
mayContactAll reports whether somebody may open a conversation with everybody
in it.

All of them, not any: a group conversation with somebody who does not accept
messages from this person is a conversation that person did not agree to be in.

A moderator is exempt. Somebody must not be able to put themselves beyond
contact by the people responsible for the server — a setting that could hide
its holder from moderation would be a setting worth abusing.
*/
func (s *Server) mayContactAll(
	ctx context.Context,
	actor dbgen.User,
	participants []uuid.UUID,
) bool {
	if s.authz != nil {
		if permissions, err := s.authz.PermissionsFor(ctx, actor.ID); err == nil {
			if permissions.AllowsServer(authz.PermModerationReview).Allowed {
				return true
			}
		}
	}

	for _, participant := range participants {
		if participant == actor.ID {
			continue
		}
		if !s.moderation.MayDirectMessage(ctx, actor.ID, participant) {
			return false
		}
	}

	return true
}
