package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/buildinfo"
)

// routes mounts the endpoints available before any capability is configured.
//
// All three are deliberately unauthenticated: a client needs the server's
// identity and version before it can sign in, and an operator's uptime check
// holds no credential.
func (s *Server) routes() {
	// Public: a client needs these before it can sign in, and an uptime check
	// holds no credential.
	s.echo.GET("/api/health", s.handleHealth)
	s.echo.GET("/api/version", s.handleVersion)
	s.echo.GET("/api/server", s.handleServerInfo)

	// First-run setup. Available only while the server has no accounts; the
	// handler refuses once one exists, which is what closes the path.
	s.echo.GET("/api/setup", s.handleSetupStatus)
	s.echo.POST("/api/setup", s.handleCompleteSetup)

	// Authentication.
	s.echo.POST("/api/auth/register", s.handleRegister)
	s.echo.POST("/api/auth/signin", s.handleSignIn)
	s.echo.POST("/api/auth/signout", s.handleSignOut, requireAuthentication)

	// The signed-in user's own account and sessions.
	me := s.echo.Group("/api/me", requireAuthentication)
	me.GET("", s.handleCurrentUser)
	me.GET("/permissions", s.handleCurrentPermissions)
	me.GET("/sessions", s.handleListSessions)
	me.DELETE("/sessions/:id", s.handleRevokeSession)

	// Rooms and messages.
	rooms := s.echo.Group("/api/rooms", requireAuthentication)
	rooms.GET("", s.handleListRooms)
	rooms.POST("", s.handleCreateRoom, requirePermission(authz.PermChannelCreate))
	rooms.GET("/:id", s.handleGetRoom)
	rooms.POST("/:id/join", s.handleJoinRoom)
	rooms.POST("/:id/leave", s.handleLeaveRoom)
	rooms.GET("/:id/members", s.handleRoomMembers)
	rooms.GET("/:id/timeline", s.handleRoomTimeline)
	rooms.POST("/:id/messages", s.handleSendMessage)
	rooms.PATCH("/:id/messages/:eventId", s.handleEditMessage)
	rooms.DELETE("/:id/messages/:eventId", s.handleDeleteMessage)
	// Counts for the whole timeline at once. Without this a client asks per
	// message, which is one request per message drawn.
	rooms.GET("/:id/threads", s.handleThreadSummaries)

	// Channel bans, confined to the channel they name.
	rooms.GET("/:id/bans", s.handleListChannelBans,
		requirePermission(authz.PermModerationRestrict))
	rooms.POST("/:id/bans", s.handleBanFromChannel,
		requirePermission(authz.PermModerationRestrict))
	rooms.DELETE("/:id/bans/:userId", s.handleLiftChannelBan,
		requirePermission(authz.PermModerationRestrict))

	// Invitations to a channel. Issuing takes the invite permission, since an
	// invitation admits somebody to a conversation and that is the channel's
	// decision rather than any member's.
	rooms.GET("/:id/invitations", s.handleListChannelInvitations)
	rooms.POST("/:id/invitations", s.handleIssueChannelInvitation,
		requirePermission(authz.PermChannelInvite))

	// Reactions. Read for the whole timeline at once, for the same reason
	// thread counts are: a request per message is a request per message drawn.
	rooms.GET("/:id/reactions", s.handleRoomReactions)
	rooms.PUT("/:id/messages/:eventId/reactions", s.handleReact)
	rooms.DELETE("/:id/messages/:eventId/reactions", s.handleUnreact)

	// Pinned messages. Pinning speaks for the room, so it takes a permission;
	// reading what is pinned is part of reading the room.
	rooms.GET("/:id/pins", s.handleListPinned)
	rooms.PUT("/:id/messages/:eventId/pin", s.handlePinMessage,
		requirePermission(authz.PermMessagePin))
	rooms.DELETE("/:id/messages/:eventId/pin", s.handleUnpinMessage,
		requirePermission(authz.PermMessagePin))

	// Forwarding a message to another conversation.
	rooms.POST("/:id/messages/:eventId/forward", s.handleForward)

	// Messages written now and sent later.
	rooms.POST("/:id/scheduled", s.handleScheduleMessage)
	rooms.GET("/:id/threads/:eventId", s.handleThread)
	rooms.POST("/:id/threads/:eventId/replies", s.handleReplyInThread)
	rooms.POST("/:id/read", s.handleMarkRead)
	rooms.GET("/:id/presence", s.handleRoomPresence)
	rooms.POST("/:id/typing", s.handleStartTyping)
	rooms.GET("/:id/typing", s.handleListTyping)

	// Attachments. Uploaded to a room before the message that carries them, so
	// a refusal costs only the upload and a failed send does not lose the file.
	rooms.POST("/:id/attachments", s.handleUploadAttachment)
	s.echo.GET("/api/attachments/limits", s.handleAttachmentLimits, requireAuthentication)
	s.echo.GET("/api/attachments/:id", s.handleDownloadAttachment, requireAuthentication)
	s.echo.GET("/api/attachments/:id/preview", s.handleAttachmentPreview, requireAuthentication)
	// A caption belongs to the file, so several sent together can each say
	// something different.
	s.echo.PUT("/api/attachments/:id/caption", s.handleCaptionAttachment, requireAuthentication)

	// Notifications, and what a person wants to be told about.
	me.GET("/notifications", s.handleListNotifications)
	me.GET("/notifications/count", s.handleNotificationCount)
	me.POST("/notifications/:id/read", s.handleMarkNotificationRead)
	me.PUT("/preferences/notifications", s.handleSetNotificationPreference)

	// Search. A GET, so a search is shareable and bookmarkable: the term is
	// the caller's own words about their own conversations.
	// A person's own pinned conversations, which is a bookmark rather than an
	// announcement and so is kept per person.
	// Dismissing everything unread in one conversation, which is what a
	// grouped notification entry stands for.
	me.POST("/notifications/rooms/:id/read", s.handleDismissRoomNotifications)

	// Blocking, which is a personal boundary rather than an exercise of
	// authority: no permission, and never disclosed to the person blocked.
	me.GET("/blocks", s.handleListBlocked)
	me.POST("/blocks", s.handleBlock)
	me.DELETE("/blocks/:id", s.handleUnblock)

	// Who may open a conversation with this person.
	me.GET("/privacy", s.handleGetPrivacy)
	me.PUT("/privacy", s.handleSetPrivacy)

	s.echo.GET("/api/me/pins", s.handleListPinnedRooms, requireAuthentication)
	s.echo.PUT("/api/me/pins/:id", s.handlePinRoom, requireAuthentication)
	s.echo.DELETE("/api/me/pins/:id", s.handleUnpinRoom, requireAuthentication)

	// Scheduled messages a person has outstanding, and cancelling one.
	s.echo.GET("/api/me/scheduled", s.handleListScheduled, requireAuthentication)
	s.echo.DELETE("/api/me/scheduled/:id", s.handleCancelScheduled, requireAuthentication)

	// Reporting abuse. Filing is open to anybody signed in — requiring
	// membership would stop somebody reporting what they saw before leaving —
	// while the queue is for whoever moderates.
	s.echo.GET("/api/reports/reasons", s.handleReportReasons, requireAuthentication)
	s.echo.POST("/api/reports", s.handleReport, requireAuthentication)
	s.echo.GET("/api/reports", s.handleListReports, requireAuthentication,
		requirePermission(authz.PermModerationReview))
	s.echo.GET("/api/reports/count", s.handleReportCount, requireAuthentication,
		requirePermission(authz.PermModerationReview))
	s.echo.POST("/api/reports/:id/resolve", s.handleResolveReport, requireAuthentication,
		requirePermission(authz.PermModerationReview))

	// Searching a third-party GIF library, when an operator has configured
	// one. Authenticated, so this server is not an open proxy to somebody
	// else's API on the operator's key.
	// Redeeming an invitation. Authenticated: an invitation admits an account
	// to something, so there has to be an account.
	s.echo.POST("/api/invitations/redeem", s.handleRedeemInvitation, requireAuthentication)
	s.echo.DELETE("/api/invitations/:id", s.handleRevokeInvitation, requireAuthentication,
		requirePermission(authz.PermChannelInvite))

	s.echo.GET("/api/gifs", s.handleSearchGifs, requireAuthentication)

	s.echo.GET("/api/search", s.handleSearch, requireAuthentication)
	// One box, one question: messages, conversations and people together,
	// rather than three requests resolving at different times.
	s.echo.GET("/api/search/everything", s.handleSearchEverything, requireAuthentication)

	// Link previews. A POST because the link is private: a query string ends
	// up in access logs, proxy logs and browser history, and a body does not.
	s.echo.POST("/api/previews", s.handlePreviewLink, requireAuthentication)
	me.PUT("/preferences/link-previews", s.handleSetPreviewPreference)

	// Direct conversations, which are found by their participants rather than
	// by name.
	direct := s.echo.Group("/api/direct", requireAuthentication)
	direct.GET("", s.handleListDirect)
	direct.POST("", s.handleOpenDirect)

	// Presence and unread state for the signed-in person.
	me.GET("/unread", s.handleUnreadCounts)
	me.PUT("/status", s.handleSetStatus)
	s.echo.GET("/api/statuses", s.handleListStatuses, requireAuthentication)

	// The live connection. Authenticated like any other route; a connection is
	// never established for an unauthenticated caller.
	s.echo.GET("/api/live", s.handleWebSocket, requireAuthentication)

	// Administration. Each route names the permission that governs it, so
	// authority is visible at the routing table rather than buried in a
	// handler.
	admin := s.echo.Group("/api/admin", requireAuthentication)
	admin.GET("/permissions", s.handlePermissionCatalogue,
		requirePermission(authz.PermRoleManage))
	admin.GET("/audit", s.handleReadAudit,
		requirePermission(authz.PermServerViewAudit))

	// Invitations to the server itself, for the registration policy that
	// requires one.
	admin.GET("/invitations", s.handleListServerInvitations,
		requirePermission(authz.PermUserInvite))
	admin.POST("/invitations", s.handleIssueServerInvitation,
		requirePermission(authz.PermUserInvite))

	// Policy an operator changes while the server runs: who may register, how
	// much this server will hold.
	admin.GET("/settings", s.handleListSettings,
		requirePermission(authz.PermServerConfigure))
	admin.PUT("/settings/:key", s.handleSetSetting,
		requirePermission(authz.PermServerConfigure))

	// The other half of a limit: what is being used against what is allowed,
	// so an operator sees it coming rather than discovering it from a refusal.
	admin.GET("/usage", s.handleUsage,
		requirePermission(authz.PermServerConfigure))

	// Registered last: the catch-all serving the web application must not
	// shadow an endpoint above it.
	s.mountWebApp(s.config.WebRoot)
}

// healthResponse reports whether the server can serve requests.
type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

func (s *Server) handleHealth(c *echo.Context) error {
	checks := map[string]string{}
	status := statusOK

	if s.db != nil {
		if err := s.db.Health(c.Request().Context()); err != nil {
			// The reason is logged, not returned: an unauthenticated caller
			// should learn that the server is unhealthy, not why.
			s.log.Error("database health check failed", "error", err)
			checks["database"] = statusUnavailable
			status = statusUnavailable
		} else {
			checks["database"] = statusOK
		}
	}

	code := http.StatusOK
	if status != statusOK {
		// Report a failing status so a load balancer or uptime check takes
		// this instance out of rotation rather than sending it traffic.
		code = http.StatusServiceUnavailable
	}

	return c.JSON(code, healthResponse{Status: status, Checks: checks})
}

func (s *Server) handleVersion(c *echo.Context) error {
	return c.JSON(http.StatusOK, buildinfo.Current())
}

// serverInfoResponse is what a server tells an unauthenticated caller about
// itself. It mirrors the ServerInfo type shared with the clients.
type serverInfoResponse struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	CanonicalAddress string   `json:"canonicalAddress"`
	Version          string   `json:"version"`
	ProtocolVersions []string `json:"protocolVersions"`
	Capabilities     []string `json:"capabilities"`
	DefaultLanguage  string   `json:"defaultLanguage"`
}

/*
capabilities lists the optional features this server actually offers.

Advertised rather than assumed, because every one of these depends on an
operator's choice: a client that offered GIF search on a server without a
provider would show a control that always fails.
*/
func (s *Server) capabilities() []string {
	capabilities := []string{}

	if s.gifs != nil && s.gifs.Enabled() {
		capabilities = append(capabilities, "gif-search")
	}
	if s.unfurl != nil && s.unfurl.Enabled() {
		capabilities = append(capabilities, "link-previews")
	}

	return capabilities
}

func (s *Server) handleServerInfo(c *echo.Context) error {
	return c.JSON(http.StatusOK, serverInfoResponse{
		Name:             s.config.ServerName,
		Description:      s.config.ServerDescription,
		CanonicalAddress: s.config.CanonicalAddress,
		Version:          buildinfo.Version,
		// Federation is not implemented, so no protocol version is advertised.
		ProtocolVersions: []string{},
		// Optional capabilities are advertised as they are built, so a client
		// adapts to what this server actually offers rather than presenting a
		// control that cannot work.
		Capabilities:    s.capabilities(),
		DefaultLanguage: s.config.DefaultLanguage,
	})
}
