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

	// Search. A GET, so a search is shareable and bookmarkable: the term is
	// the caller's own words about their own conversations.
	s.echo.GET("/api/search", s.handleSearch, requireAuthentication)

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

func (s *Server) handleServerInfo(c *echo.Context) error {
	return c.JSON(http.StatusOK, serverInfoResponse{
		Name:             s.config.ServerName,
		Description:      s.config.ServerDescription,
		CanonicalAddress: s.config.CanonicalAddress,
		Version:          buildinfo.Version,
		// Federation is not implemented, so no protocol version is advertised.
		ProtocolVersions: []string{},
		// Optional capabilities are advertised as they are built, so a client
		// adapts to what this server actually offers.
		Capabilities:    []string{},
		DefaultLanguage: s.config.DefaultLanguage,
	})
}
