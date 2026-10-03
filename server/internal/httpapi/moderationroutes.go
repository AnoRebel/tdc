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
	"github.com/AnoRebel/tdc/server/internal/presence"
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

// restrictionResponse is somebody silenced in a channel.
type restrictionResponse struct {
	ID           string `json:"id"`
	UserID       string `json:"userId"`
	Username     string `json:"username"`
	RestrictedBy string `json:"restrictedBy"`
	Reason       string `json:"reason"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

/*
handleRestrictPosting silences somebody in one channel without removing them.

The proportionate response to somebody arguing rather than attacking: they keep
reading the conversation they are part of and stop adding to it, for as long as
the moderator says. The request has the same shape as a ban's, because it is
the same decision made more gently.
*/
func (s *Server) handleRestrictPosting(c *echo.Context) error {
	actor, _ := currentUser(c)

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

	id, err := s.moderation.RestrictPosting(c.Request().Context(), target, &roomID, actor.ID,
		s.rooms.UserAddress(actor.Username), request.Reason, until)
	if errors.Is(err, moderation.ErrReasonRequired) {
		return echo.NewHTTPError(http.StatusBadRequest, "a reason is required")
	}
	if err != nil {
		return err
	}

	s.recordModeration(c, actor, audit.ActionPostingRestricted, "room", roomID.String(),
		map[string]any{
			"user":   target.String(),
			"reason": request.Reason,
			"hours":  request.Hours,
		})

	return c.JSON(http.StatusCreated, map[string]string{"id": id.String()})
}

func (s *Server) handleLiftRestriction(c *echo.Context) error {
	actor, _ := currentUser(c)

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	id, err := uuid.Parse(c.Param("restrictionId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such restriction")
	}

	err = s.moderation.LiftRestriction(c.Request().Context(), id, roomID, actor.ID)
	if errors.Is(err, moderation.ErrNoSuchRestriction) {
		return echo.NewHTTPError(http.StatusNotFound, "no such restriction")
	}
	if err != nil {
		return err
	}

	s.recordModeration(c, actor, audit.ActionRestrictionLifted, "room", roomID.String(),
		map[string]any{"restriction": id.String()})

	return c.NoContent(http.StatusNoContent)
}

// handleListRestrictions shows who is currently silenced in a channel.
func (s *Server) handleListRestrictions(c *echo.Context) error {
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	restrictions, err := s.moderation.RestrictionsIn(c.Request().Context(), roomID)
	if err != nil {
		return err
	}

	response := make([]restrictionResponse, 0, len(restrictions))
	for _, restriction := range restrictions {
		item := restrictionResponse{
			ID:           restriction.ID.String(),
			UserID:       restriction.UserID.String(),
			Username:     restriction.Username,
			RestrictedBy: restriction.RestrictedBy,
			Reason:       restriction.Reason,
			CreatedAt:    restriction.CreatedAt.UTC().Format(time.RFC3339),
		}
		if restriction.ExpiresAt != nil {
			item.ExpiresAt = restriction.ExpiresAt.UTC().Format(time.RFC3339)
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

	/*
		What a member list may say about this person beyond their name.

		Pointers so that a request omitting them leaves them alone. An older
		client sending only directMessages must not silently withdraw somebody's
		presence because its request body had no field for it.
	*/
	ShowsPresence *bool `json:"showsPresence,omitempty"`
	ShowsStatus   *bool `json:"showsStatus,omitempty"`
	ShowsLastSeen *bool `json:"showsLastSeen,omitempty"`
}

// handleSetPrivacy records who may open a conversation with somebody, and what
// a member list may say about them.
func (s *Server) handleSetPrivacy(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request privacyRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if request.DirectMessages != "" {
		err := s.moderation.SetDirectMessagePolicy(ctx, user.ID,
			moderation.DirectMessagePolicy(request.DirectMessages))
		if errors.Is(err, moderation.ErrUnknownPolicy) {
			return echo.NewHTTPError(http.StatusBadRequest,
				`direct messages must be "anyone", "shared" or "nobody"`)
		}
		if err != nil {
			return err
		}
	}

	/*
		Read before written, so a request naming one detail leaves the other two
		as they were. The alternative — writing all three from the request — would
		make a client that knows about two settings withdraw the third.
	*/
	if request.ShowsPresence != nil || request.ShowsStatus != nil || request.ShowsLastSeen != nil {
		if s.presence == nil {
			return echo.NewHTTPError(http.StatusNotFound, "this server does not track presence")
		}

		disclosure, err := s.presence.DisclosureFor(ctx, user.ID)
		if err != nil {
			return err
		}
		if request.ShowsPresence != nil {
			disclosure.Presence = *request.ShowsPresence
		}
		if request.ShowsStatus != nil {
			disclosure.Status = *request.ShowsStatus
		}
		if request.ShowsLastSeen != nil {
			disclosure.LastSeen = *request.ShowsLastSeen
		}
		if err := s.presence.SetDisclosure(ctx, user.ID, disclosure); err != nil {
			return err
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// handleGetPrivacy returns somebody's own settings.
func (s *Server) handleGetPrivacy(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	response := privacyRequest{
		DirectMessages: string(s.moderation.DirectMessagePolicyFor(ctx, user.ID)),
	}

	// Their own settings, so nothing is withheld here: this is the screen on
	// which somebody changes them, and it has to show what they are.
	disclosure := presence.DisclosesEverything()
	if s.presence != nil {
		read, err := s.presence.DisclosureFor(ctx, user.ID)
		if err != nil {
			return err
		}
		disclosure = read
	}
	response.ShowsPresence = &disclosure.Presence
	response.ShowsStatus = &disclosure.Status
	response.ShowsLastSeen = &disclosure.LastSeen

	return c.JSON(http.StatusOK, response)
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
