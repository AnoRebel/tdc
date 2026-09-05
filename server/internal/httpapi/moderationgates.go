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
