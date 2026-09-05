package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/moderation"
)

type banRequest struct {
	// User is who is being banned.
	User string `json:"user"`
	// Reason is required: the person banned is entitled to know why, and a
	// decision nobody can explain is one nobody can review.
	Reason string `json:"reason"`
	// Hours is how long. Absent or zero is permanent.
	Hours int `json:"hours,omitempty"`
}

// banResponse is one active ban.
type banResponse struct {
	UserID    string `json:"userId"`
	Username  string `json:"username"`
	BannedBy  string `json:"bannedBy"`
	Reason    string `json:"reason"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	CreatedAt string `json:"createdAt"`
}

/*
handleBanFromChannel keeps somebody out of one conversation.

Confined to that channel. Somebody banned from one keeps every other
membership, which is the difference between a ban and a suspension and the
reason both exist.
*/
func (s *Server) handleBanFromChannel(c *echo.Context) error {
	actor, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request banRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	target, err := uuid.Parse(request.User)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a person")
	}

	var until *time.Time
	if request.Hours > 0 {
		expiry := time.Now().Add(time.Duration(request.Hours) * time.Hour)
		until = &expiry
	}

	err = s.moderation.BanFromChannel(ctx, roomID, target, actor.ID,
		s.rooms.UserAddress(actor.Username), request.Reason, until)
	if errors.Is(err, moderation.ErrReasonRequired) {
		return echo.NewHTTPError(http.StatusBadRequest, "a reason is required")
	}
	if err != nil {
		return err
	}

	// Removed from the channel as well as barred from returning: a ban that
	// left somebody sitting in the conversation would be a ban in name only.
	if err := s.rooms.Leave(ctx, roomID, target); err != nil {
		s.log.Warn("cannot remove a banned person from the channel",
			"roomId", roomID, "userId", target, "error", err)
	}

	s.recordModeration(c, actor, audit.ActionChannelBan, "room", roomID.String(),
		map[string]any{
			"user":   target.String(),
			"reason": request.Reason,
			"hours":  request.Hours,
		})

	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleLiftChannelBan(c *echo.Context) error {
	actor, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	target, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such person")
	}

	if err := s.moderation.LiftBan(c.Request().Context(), roomID, target); err != nil {
		return err
	}

	s.recordModeration(c, actor, audit.ActionChannelBanLifted, "room", roomID.String(),
		map[string]any{"user": target.String()})

	return c.NoContent(http.StatusNoContent)
}

// handleListChannelBans shows who is currently kept out.
func (s *Server) handleListChannelBans(c *echo.Context) error {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	bans, err := s.moderation.BansIn(c.Request().Context(), roomID)
	if err != nil {
		return err
	}

	response := make([]banResponse, 0, len(bans))
	for _, ban := range bans {
		item := banResponse{
			UserID:    ban.UserID.String(),
			Username:  ban.Username,
			BannedBy:  ban.BannedBy,
			Reason:    ban.Reason,
			CreatedAt: ban.CreatedAt.UTC().Format(time.RFC3339),
		}
		if ban.ExpiresAt != nil {
			item.ExpiresAt = ban.ExpiresAt.UTC().Format(time.RFC3339)
		}
		response = append(response, item)
	}

	return c.JSON(http.StatusOK, response)
}

type blockRequest struct {
	User string `json:"user"`
}

/*
handleBlock ends contact from one person, without telling the other.

Never disclosed. The person blocked is not told, and nothing in what they can
see says so — somebody who learns they have been blocked frequently escalates,
and the point is to end contact rather than to announce it.
*/
func (s *Server) handleBlock(c *echo.Context) error {
	user, _ := currentUser(c)

	var request blockRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	target, err := uuid.Parse(request.User)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a person")
	}

	if err := s.moderation.Block(c.Request().Context(), user.ID, target); err != nil {
		return err
	}

	// Deliberately not audited as a moderation action. Blocking is a personal
	// boundary rather than an exercise of authority, and a log a moderator
	// reads should not turn it into one.
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleUnblock(c *echo.Context) error {
	user, _ := currentUser(c)

	target, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such person")
	}

	if err := s.moderation.Unblock(c.Request().Context(), user.ID, target); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// blockedResponse is somebody the reader has blocked.
type blockedResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	BlockedAt   string `json:"blockedAt"`
}

// handleListBlocked shows who somebody has blocked, which only they can see.
func (s *Server) handleListBlocked(c *echo.Context) error {
	user, _ := currentUser(c)

	rows, err := s.moderation.BlockedBy(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make([]blockedResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, blockedResponse{
			ID:          row.ID.String(),
			Username:    row.Username,
			DisplayName: row.DisplayName,
			BlockedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	return c.JSON(http.StatusOK, response)
}

type privacyRequest struct {
	// DirectMessages is "anyone", "shared", or "nobody".
	DirectMessages string `json:"directMessages"`
}

// handleSetPrivacy records who may open a conversation with somebody.
func (s *Server) handleSetPrivacy(c *echo.Context) error {
	user, _ := currentUser(c)

	var request privacyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	err := s.moderation.SetDirectMessagePolicy(c.Request().Context(), user.ID,
		moderation.DirectMessagePolicy(request.DirectMessages))
	if errors.Is(err, moderation.ErrUnknownPolicy) {
		return echo.NewHTTPError(http.StatusBadRequest,
			`direct messages must be "anyone", "shared" or "nobody"`)
	}
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// handleGetPrivacy returns somebody's own settings.
func (s *Server) handleGetPrivacy(c *echo.Context) error {
	user, _ := currentUser(c)

	policy := s.moderation.DirectMessagePolicyFor(c.Request().Context(), user.ID)

	return c.JSON(http.StatusOK, privacyRequest{DirectMessages: string(policy)})
}

// recordModeration writes an audit entry for an exercise of authority.
func (s *Server) recordModeration(
	c *echo.Context,
	actor dbgen.User,
	action audit.Action,
	targetKind, targetID string,
	detail map[string]any,
) {
	if s.audit == nil {
		return
	}

	if err := s.audit.Record(c.Request().Context(), audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     action,
		TargetKind: targetKind,
		TargetID:   targetID,
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail:     detail,
	}); err != nil {
		s.log.Error("cannot record a moderation action", "action", action, "error", err)
	}
}

type suspendRequest struct {
	// Reason is required: somebody suspended is entitled to know why.
	Reason string `json:"reason"`
	// Hours is how long. Absent or zero is indefinite.
	Hours int `json:"hours,omitempty"`
}

/*
handleSuspendUser bars somebody from the server.

Server-wide, where a ban is confined to a channel. Their sessions end at once —
the flag is checked on every request, and the sessions are revoked as well so
nothing is left looking live.
*/
func (s *Server) handleSuspendUser(c *echo.Context) error {
	actor, _ := currentUser(c)
	ctx := c.Request().Context()

	target, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such person")
	}

	// Suspending yourself would lock the server's own administrator out of it,
	// which is a mistake nobody recovers from without database access.
	if target == actor.ID {
		return echo.NewHTTPError(http.StatusBadRequest,
			"you cannot suspend your own account")
	}

	var request suspendRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	var until *time.Time
	if request.Hours > 0 {
		expiry := time.Now().Add(time.Duration(request.Hours) * time.Hour)
		until = &expiry
	}

	err = s.identity.Suspend(ctx, target, actor.ID, request.Reason, until)
	if errors.Is(err, identity.ErrReasonRequired) {
		return echo.NewHTTPError(http.StatusBadRequest, "a reason is required")
	}
	if err != nil {
		return err
	}

	s.recordModeration(c, actor, audit.ActionUserSuspended, "user", target.String(),
		map[string]any{"reason": request.Reason, "hours": request.Hours})

	return c.NoContent(http.StatusNoContent)
}

// handleLiftSuspension restores an account.
//
// Sessions are not restored: somebody returning signs in again, rather than a
// device they may have lost coming back with them.
func (s *Server) handleLiftSuspension(c *echo.Context) error {
	actor, _ := currentUser(c)

	target, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such person")
	}

	if err := s.identity.LiftSuspension(c.Request().Context(), target); err != nil {
		return err
	}

	s.recordModeration(c, actor, audit.ActionUserReinstated, "user", target.String(), nil)

	return c.NoContent(http.StatusNoContent)
}
