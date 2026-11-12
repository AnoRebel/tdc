package httpapi

import (
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
Administering a channel: renaming it, archiving and restoring it, changing who
may find it, and taking somebody out of it.

Each route is gated by its permission in the channel the path names, so a role
can be given one channel to look after without being given the server. Each
change is recorded three times over, for three different readers: as an event
in the channel's log, which its members read; live, to whoever has it open; and
in the audit log, which is where somebody asking afterwards "who did that" looks.

Changing visibility has no permission of its own in the catalogue. It is
channel.rename, the permission that already governs how a channel presents
itself — with one addition. Making a channel public discloses its whole history
to everybody on the server, which reaches beyond the channel a channel-scoped
grant was made for; so that direction needs channel.rename granted for the whole
server. Making one private only narrows who can see it, and a channel's own
keeper may do that.
*/
func init() {
	registerRoutes(func(s *Server) {
		group := s.echo.Group("/api/rooms", requireAuthentication)
		group.GET("/:id/administration", s.handleChannelAdministration)
		group.PATCH("/:id", s.handleRenameChannel,
			requireChannelPermission(authz.PermChannelRename))
		group.PUT("/:id/visibility", s.handleSetChannelVisibility,
			requireChannelPermission(authz.PermChannelRename))
		group.POST("/:id/archive", s.handleArchiveChannel,
			requireChannelPermission(authz.PermChannelArchive))
		group.DELETE("/:id/archive", s.handleRestoreChannel,
			requireChannelPermission(authz.PermChannelArchive))
		group.DELETE("/:id/members/:userId", s.handleRemoveFromChannel,
			requireChannelPermission(authz.PermChannelRemove))
	})
	documentRoutes(map[string]entry{
		"GET /api/rooms/:id/administration":     {Summary: "What the caller may change about a channel", Authentication: "session or token"},
		"PATCH /api/rooms/:id":                  {Summary: "Rename a channel or change its topic", Authentication: "session or token", Permission: "channel.rename"},
		"PUT /api/rooms/:id/visibility":         {Summary: "Make a channel public or private", Authentication: "session or token", Permission: "channel.rename"},
		"POST /api/rooms/:id/archive":           {Summary: "Archive a channel", Authentication: "session or token", Permission: "channel.archive"},
		"DELETE /api/rooms/:id/archive":         {Summary: "Restore an archived channel", Authentication: "session or token", Permission: "channel.archive"},
		"DELETE /api/rooms/:id/members/:userId": {Summary: "Remove somebody from a channel", Authentication: "session or token", Permission: "channel.member.remove"},
	})
}

/*
channelAdministration says what the caller may change about a channel.

Asked of the server rather than worked out by the client, because the answer
depends on grants made for this one channel, which the client's list of
server-wide permissions does not carry. A client shows a control only where
this says yes; the routes themselves decide regardless.
*/
type channelAdministration struct {
	// Administrable is false for a conversation these do not apply to at all:
	// a direct conversation, a project's, or a channel another server holds.
	Administrable bool `json:"administrable"`
	Rename        bool `json:"rename"`
	Archive       bool `json:"archive"`
	// MakePrivate and MakePublic are separate, because making a channel
	// public needs the permission for the whole server.
	MakePrivate   bool `json:"makePrivate"`
	MakePublic    bool `json:"makePublic"`
	RemoveMembers bool `json:"removeMembers"`
	// Federated says the channel is shared with other servers, which keeps
	// its visibility as it is.
	Federated bool `json:"federated,omitempty"`
}

func (s *Server) handleChannelAdministration(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// Membership, as every change requires: somebody who is not in the
	// channel may change nothing about it, whatever they hold.
	room, err := s.rooms.RequireMembership(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	answer := channelAdministration{Administrable: s.rooms.Administrable(room)}
	if !answer.Administrable {
		return c.JSON(http.StatusOK, answer)
	}

	permissions := currentPermissions(c)
	allows := func(permission authz.Permission) bool {
		return permissions.AllowsChannel(permission, roomID).Allowed
	}
	answer.Rename = allows(authz.PermChannelRename)
	answer.Archive = allows(authz.PermChannelArchive)
	answer.RemoveMembers = allows(authz.PermChannelRemove)
	answer.Federated = room.Federated
	if !room.Federated {
		answer.MakePrivate = answer.Rename
		answer.MakePublic = answer.Rename && permissions.AllowsServer(authz.PermChannelRename).Allowed
	}

	return c.JSON(http.StatusOK, answer)
}

type renameChannelRequest struct {
	// Each is optional: absent leaves it as it is. An empty topic clears it;
	// an empty name is ignored, since a channel needs one.
	Name  *string `json:"name"`
	Topic *string `json:"topic"`
}

func (s *Server) handleRenameChannel(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request renameChannelRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Name == nil && request.Topic == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "say what to change: a name, a topic, or both")
	}

	current, err := s.rooms.RequireMembership(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}
	name, topic := current.Name, current.Topic
	if request.Name != nil {
		name = *request.Name
	}
	if request.Topic != nil {
		topic = *request.Topic
	}

	updated, event, err := s.rooms.Rename(ctx, roomID, user.ID, name, topic)
	if err != nil {
		return channelAdminError(err)
	}

	if event.ID != "" {
		s.publishEvent(ctx, event)
		s.recordModeration(c, user, audit.ActionChannelRenamed, "room", roomID.String(),
			map[string]any{
				"previousName":  current.Name,
				"name":          updated.Name,
				"previousTopic": current.Topic,
				"topic":         updated.Topic,
			})
	}

	return c.JSON(http.StatusOK, s.roomResponse(updated, true))
}

type channelVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

func (s *Server) handleSetChannelVisibility(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request channelVisibilityRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	current, err := s.rooms.RequireMembership(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}

	/*
		Making a channel public opens everything ever said in it to the whole
		server, so it needs the permission for the whole server. Checked only
		when that is what would happen: a request that changes nothing, or
		narrows who can see the channel, needs no more than the route asked.
	*/
	if request.Visibility == "public" && current.Visibility != "public" {
		if decision := currentPermissions(c).AllowsServer(authz.PermChannelRename); !decision.Allowed {
			return echo.NewHTTPError(http.StatusForbidden,
				"making a channel public opens its history to the whole server, which "+
					decision.Error()+" for the whole server")
		}
	}

	updated, event, err := s.rooms.SetVisibility(ctx, roomID, user.ID, request.Visibility)
	if err != nil {
		return channelAdminError(err)
	}

	if event.ID != "" {
		s.publishEvent(ctx, event)
		// Both values, as the specification asks: an entry that said only
		// "private" would not say whether anything had changed.
		s.recordModeration(c, user, audit.ActionChannelVisibilityChanged, "room", roomID.String(),
			map[string]any{"from": current.Visibility, "to": updated.Visibility})
	}

	return c.JSON(http.StatusOK, s.roomResponse(updated, true))
}

func (s *Server) handleArchiveChannel(c *echo.Context) error {
	return s.archiveOrRestore(c, true)
}

func (s *Server) handleRestoreChannel(c *echo.Context) error {
	return s.archiveOrRestore(c, false)
}

/*
archiveOrRestore is archiving and its reversal, which differ only in direction.

Doing either to a channel already in that state succeeds and records nothing:
two moderators archiving the same channel at once have both got what they
asked for.
*/
func (s *Server) archiveOrRestore(c *echo.Context, archive bool) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var event dbgen.Event
	action := audit.ActionChannelRestored
	if archive {
		action = audit.ActionChannelArchived
		event, err = s.rooms.Archive(ctx, roomID, user.ID)
	} else {
		event, err = s.rooms.Restore(ctx, roomID, user.ID)
	}
	if err != nil {
		return channelAdminError(err)
	}

	if event.ID != "" {
		s.publishEvent(ctx, event)
		s.recordModeration(c, user, action, "room", roomID.String(), nil)
	}

	room, err := s.rooms.Visible(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}
	return c.JSON(http.StatusOK, s.roomResponse(room, true))
}

/*
handleRemoveFromChannel takes somebody out of a channel.

Not a ban: they may come back to a public channel by joining it, and to a
private one by being invited again. A ban is what keeps somebody out, and has
its own route and its own permission.
*/
func (s *Server) handleRemoveFromChannel(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	target, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "that person is not in this channel")
	}
	if target == user.ID {
		return echo.NewHTTPError(http.StatusBadRequest, "to leave a channel, leave it")
	}

	event, err := s.rooms.Remove(ctx, roomID, user.ID, target)
	if err != nil {
		return channelAdminError(err)
	}

	// The channel's members see who was removed, as they see who joins.
	s.publishEvent(ctx, event)
	// And so does the person removed, who is no longer a member and so no
	// longer among those the channel's events go to.
	s.tellRemoved(ctx, target, event)

	s.recordModeration(c, user, audit.ActionChannelMemberRemoved, "room", roomID.String(),
		map[string]any{"user": target.String()})

	return c.NoContent(http.StatusNoContent)
}

/*
tellRemoved delivers a removal to the connections of the person removed.

Membership is read when an event is delivered, which is what stops a removed
person receiving the channel at once — and also what would keep them from
hearing that they were removed, leaving a client showing a conversation that has
quietly stopped. So the removal goes to them by who they are, on this node and
any other, the way a program's private answer does.
*/
func (s *Server) tellRemoved(ctx context.Context, target uuid.UUID, event dbgen.Event) {
	if s.hub == nil {
		return
	}
	frame, err := eventFrame(event, nil, s.rooms.SenderName(ctx, event))
	if err != nil {
		s.log.Error("cannot render a removal for the person removed", "eventId", event.ID, "error", err)
		return
	}
	here := s.hub.SendToUser(target, frame)
	s.whisperElsewhere(ctx, target, frame, here)
}

// channelAdminError maps an administration refusal to a response, and
// anything else as roomError does.
func channelAdminError(err error) error {
	switch {
	case errors.Is(err, rooms.ErrNotAChannel),
		errors.Is(err, rooms.ErrHostedElsewhere),
		errors.Is(err, rooms.ErrFederatedVisibility):
		// Conflict: the request is well formed, and the conversation it names
		// is not one it can apply to.
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, rooms.ErrInvalidVisibility),
		errors.Is(err, rooms.ErrInvalidDetails):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, rooms.ErrNoSuchMember):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, rooms.ErrArchived):
		return echo.NewHTTPError(http.StatusConflict, "this channel is archived; restore it first")
	default:
		return roomError(err)
	}
}
