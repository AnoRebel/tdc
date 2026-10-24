package httpapi

import (
	"context"
	"net/http"
	"strings"
	"unicode"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/buildinfo"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/oauth"
	"github.com/AnoRebel/tdc/server/internal/settings"
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
	/*
		What this server serves, generated from the routing table.

		Public, because a client deciding whether it can talk to this server
		should not have to hold a credential to find out what it could ask for.
	*/
	s.echo.GET("/api/description", s.handleDescription)
	s.echo.GET("/api/openapi.json", s.handleOpenAPI)
	s.echo.GET("/api/version", s.handleVersion)
	s.echo.GET("/api/server", s.handleServerInfo)

	/*
		The shape of the network this community is part of.

		A session rather than an administrative permission: which servers a
		community federates with is not privileged from its own members, and
		hiding it would mean people cannot tell where the rooms they are in
		actually live.
	*/
	s.echo.GET("/api/federation/servers", s.handleConnectedServers, requireAuthentication)
	s.echo.GET("/api/federation/servers/:domain", s.handleConnectedServer, requireAuthentication)
	s.echo.GET("/api/federation/servers/:domain/rooms", s.handleBrowsePeer, requireAuthentication)

	/*
		This server's identity, for peers.

		Unauthenticated and served whether or not federation is enabled: a peer
		must be able to learn who this server is and whether it federates at
		all before it holds any credential here, and "not participating" is a
		more useful answer than a refused connection.
	*/
	s.echo.GET(federation.WellKnownPath, s.handleWellKnown)

	/*
		Federated routes, refused entirely while federation is off.

		The gate is middleware on the group rather than a check inside each
		handler, so that adding a federated endpoint cannot accidentally omit
		it.
	*/
	federated := s.echo.Group("/api/federation", s.requireFederation)
	federated.POST("/version", s.handleFederationVersion)

	/*
		Routes a peer must prove itself to reach.

		Version negotiation above is deliberately not among them: a peer has
		to agree a version before it can sign anything the other side knows
		how to read, so requiring a signature there would be a handshake that
		could never complete.
	*/
	authenticated := federated.Group("", s.requireSignature)
	authenticated.POST("/ping", s.handlePing)

	/*
		A room's events, in both directions.

		The peer is the verified origin of the signed request rather than
		anything it names, so a server cannot read a room by claiming to be
		somebody who participates in it.
	*/
	authenticated.GET("/rooms/:slug/events", s.handleFederatedEvents)
	authenticated.POST("/rooms/:slug/events", s.handleFederatedDelivery)

	/*
		The write path: a peer's user joining a room this server owns, and
		posting into it.

		The member named must be on the peer that signed the request, so a
		server can act for its own users and nobody else's.
	*/
	authenticated.POST("/rooms/:slug/join", s.handleFederatedJoin)
	authenticated.POST("/rooms/:slug/leave", s.handleFederatedLeave)
	authenticated.POST("/rooms/:slug/post", s.handleFederatedPost)

	// The bytes of a file a message in a room carries, for a peer whose
	// member opened it.
	authenticated.GET("/rooms/:slug/files/:digest", s.handleFederatedFile)

	// What this server publishes to a peer browsing it. Signed like
	// everything else here, so the visibility rules see a peer identity this
	// server verified rather than one the request asserted.
	authenticated.GET("/rooms", s.handleFederatedRoomList)

	// First-run setup. Available only while the server has no accounts; the
	// handler refuses once one exists, which is what closes the path.
	s.echo.GET("/api/setup", s.handleSetupStatus)
	s.echo.POST("/api/setup", s.handleCompleteSetup)

	// Authentication.
	s.echo.POST("/api/auth/register", s.handleRegister)
	s.echo.POST("/api/auth/signin", s.handleSignIn)
	s.echo.POST("/api/auth/signout", s.handleSignOut, requireAuthentication)

	/*
		Signing in through a provider somebody else runs.

		Unauthenticated, all three, because signing in is what they are for. The
		start reads a session if one is there — that is somebody already signed
		in adding a provider to their account rather than signing in — but it
		does not require one, so requireAuthentication would be wrong here.

		The callback is a browser navigation from the provider, not a client
		call, which is why it is a GET that answers with a redirect.

		No middleware on the start: s.authenticate already runs for every
		request and leaves the user on the context when there is one, so the
		handler can tell the two cases apart without a route that refuses
		either.
	*/
	s.echo.GET("/api/auth/oauth/:provider/start", s.handleOAuthStart)
	s.echo.GET("/api/auth/oauth/callback", s.handleOAuthCallback)
	s.echo.POST("/api/auth/oauth/finish", s.handleOAuthFinish)

	/*
		Passkeys.

		The sign-in pair is unauthenticated, because it is how somebody gets in.
		The enrolment pair is not: adding a passkey adds a way into an account,
		so it is done by somebody who has already proved they hold it.
	*/
	/*
		The second half of a sign-in that owes a second factor.

		Unauthenticated, because there is no session yet — that is the thing
		being earned. What stands in for one is the ticket, which buys nothing
		except the right to try a code for the account it names.
	*/
	s.echo.POST("/api/auth/second-factor", s.handleSecondFactor)
	// Or with a passkey, for somebody who already carries one rather than an
	// authenticator app.
	s.echo.POST("/api/auth/second-factor/passkey/begin", s.handleBeginSecondFactorPasskey)
	s.echo.POST("/api/auth/second-factor/passkey/finish", s.handleFinishSecondFactorPasskey)
	// Or by mail, for somebody with no phone to hand — where the server sends
	// mail and the address has been confirmed.
	s.echo.POST("/api/auth/second-factor/email", s.handleSendEmailedCode)
	s.echo.POST("/api/auth/second-factor/email/verify", s.handleEmailedCode)

	/*
		Getting back in without a password.

		Unauthenticated, because somebody who could sign in would not be here.
		The first answers identically whatever it finds, which is the whole
		point of it.
	*/
	s.echo.POST("/api/auth/recover", s.handleRequestRecovery)
	s.echo.GET("/api/auth/recover", s.handleCheckRecovery)
	s.echo.POST("/api/auth/recover/redeem", s.handleRedeemRecovery)

	/*
		Proving an address, which is what makes recovery reach anybody.

		The link is unauthenticated because it arrives in a mail client that may
		not be on the machine that asked — and the token is what proves the
		address, which is all that is being proved.
	*/
	s.echo.POST("/api/auth/confirm-email", s.handleConfirmEmail)

	s.echo.POST("/api/auth/passkey/signin/begin", s.handleBeginPasskeySignIn)
	s.echo.POST("/api/auth/passkey/signin/finish", s.handleFinishPasskeySignIn)

	/*
		Somebody's own second factor.

		Every one of these needs a session: setting one up, proving it, and
		removing it are all things done by somebody who has already signed in —
		and removing it needs the password again besides.
	*/
	secondFactor := s.echo.Group("/api/me/second-factor", requireAuthentication)
	secondFactor.GET("", s.handleSecondFactorStatus)
	secondFactor.POST("/begin", s.handleBeginSecondFactor)
	secondFactor.POST("/confirm", s.handleConfirmSecondFactor)
	secondFactor.POST("/recovery-codes", s.handleReissueRecoveryCodes)
	/*
		A POST rather than a DELETE, because turning this off carries a
		password.

		A DELETE with a body is legal but awkward: the shared transport both
		clients use gives DELETE no body, and widening that contract for one
		route is a worse trade than naming the action. It is an action anyway —
		prove who you are, then remove the factor — rather than a plain
		deletion of a thing at a path.
	*/
	secondFactor.POST("/disable", s.handleDisableSecondFactor)

	passkeys := s.echo.Group("/api/me/passkeys", requireAuthentication)
	passkeys.GET("", s.handleListPasskeys)
	passkeys.POST("/begin", s.handleBeginPasskeyEnrolment)
	passkeys.POST("/finish", s.handleFinishPasskeyEnrolment)
	passkeys.PATCH("/:id", s.handleRenamePasskey)
	passkeys.DELETE("/:id", s.handleRevokePasskey)

	// The signed-in user's own account and sessions.
	/*
		The credentials a program carries.

		Under /api/me because they belong to the account rather than the
		server, and beside the passkeys for the same reason: these are the
		things somebody issues in their own name and has to be able to take
		back. Creating one is refused to a caller that is itself a token — a
		program that could mint a successor would make revocation less than
		final.
	*/
	tokens := s.echo.Group("/api/me/tokens", requireAuthentication)
	tokens.GET("", s.handleListTokens)
	tokens.POST("", s.handleCreateToken)
	tokens.DELETE("/:id", s.handleRevokeToken)

	me := s.echo.Group("/api/me", requireAuthentication)
	me.GET("", s.handleCurrentUser)
	me.GET("/permissions", s.handleCurrentPermissions)
	// Adding or changing the address the account can be recovered through. The
	// account keeps its current one until the link is used.
	me.GET("/email", s.handleCurrentEmail)
	me.POST("/email", s.handleRequestEmailConfirmation)
	me.GET("/sessions", s.handleListSessions)
	me.DELETE("/sessions/:id", s.handleRevokeSession)

	// Rooms and messages.
	rooms := s.echo.Group("/api/rooms", requireAuthentication)
	rooms.GET("", s.handleListRooms)
	rooms.POST("", s.handleCreateRoom, requirePermission(authz.PermChannelCreate))
	rooms.GET("/:id", s.handleGetRoom)
	rooms.POST("/:id/join", s.handleJoinRoom)
	rooms.POST("/:id/leave", s.handleLeaveRoom)

	/*
		Joining and posting in a room another server owns.

		Separate paths rather than the ordinary ones branching, because what
		happens differs completely: nothing is sequenced here, the home server
		decides, and a refusal comes from a server this one does not control.
	*/
	rooms.POST("/:id/remote/join", s.handleJoinRemoteRoom)
	rooms.POST("/:id/remote/messages", s.handleSendToRemoteRoom, s.requirePosting())
	rooms.GET("/:id/members", s.handleRoomMembers, requireChannelPermission(authz.PermChannelRead))
	rooms.GET("/:id/timeline", s.handleRoomTimeline, requireChannelPermission(authz.PermChannelRead))
	rooms.POST("/:id/messages", s.handleSendMessage, s.requirePosting())
	rooms.PATCH("/:id/messages/:eventId", s.handleEditMessage, s.requirePosting())
	// When a message expires. Its sender's to set, or a moderator's, and only
	// ever shorter than it already is.
	rooms.PUT("/:id/messages/:eventId/lifetime", s.handleSetMessageLifetime)
	rooms.DELETE("/:id/messages/:eventId", s.handleDeleteMessage)
	// Counts for the whole timeline at once. Without this a client asks per
	// message, which is one request per message drawn.
	rooms.GET("/:id/threads", s.handleThreadSummaries, requireChannelPermission(authz.PermChannelRead))

	// Channel bans, confined to the channel they name.
	rooms.GET("/:id/bans", s.handleListChannelBans,
		requirePermission(authz.PermModerationRestrict))
	rooms.POST("/:id/bans", s.handleBanFromChannel,
		requirePermission(authz.PermModerationRestrict))
	rooms.DELETE("/:id/bans/:userId", s.handleLiftChannelBan,
		requirePermission(authz.PermModerationRestrict))

	// Posting restrictions: in the channel and reading it, but silenced.
	rooms.GET("/:id/restrictions", s.handleListRestrictions,
		requirePermission(authz.PermModerationRestrict))
	rooms.POST("/:id/restrictions", s.handleRestrictPosting,
		requirePermission(authz.PermModerationRestrict))
	rooms.DELETE("/:id/restrictions/:restrictionId", s.handleLiftRestriction,
		requirePermission(authz.PermModerationRestrict))

	// Invitations to a channel. Issuing takes the invite permission, since an
	// invitation admits somebody to a conversation and that is the channel's
	// decision rather than any member's.
	rooms.GET("/:id/invitations", s.handleListChannelInvitations)
	rooms.POST("/:id/invitations", s.handleIssueChannelInvitation,
		requirePermission(authz.PermChannelInvite),
		s.requireFeature(settings.FeatureInvitations))

	// Reactions. Read for the whole timeline at once, for the same reason
	// thread counts are: a request per message is a request per message drawn.
	rooms.GET("/:id/reactions", s.handleRoomReactions, requireChannelPermission(authz.PermChannelRead))
	rooms.PUT("/:id/messages/:eventId/reactions", s.handleReact)
	rooms.DELETE("/:id/messages/:eventId/reactions", s.handleUnreact)

	/*
		Pressing a button on a message.

		POST because it is an action rather than a state to set: the same
		button pressed twice is two attempts at one decision, and the server
		refuses the second rather than making it idempotent. A program
		receiving two events for one choice cannot tell that from somebody
		changing their mind.
	*/
	rooms.POST("/:id/messages/:eventId/actions/:action", s.handlePressAction)

	// Pinned messages. Pinning speaks for the room, so it takes a permission;
	// reading what is pinned is part of reading the room.
	rooms.GET("/:id/pins", s.handleListPinned, requireChannelPermission(authz.PermChannelRead))
	rooms.PUT("/:id/messages/:eventId/pin", s.handlePinMessage,
		requirePermission(authz.PermMessagePin))
	rooms.DELETE("/:id/messages/:eventId/pin", s.handleUnpinMessage,
		requirePermission(authz.PermMessagePin))

	// Forwarding a message to another conversation.
	rooms.POST("/:id/messages/:eventId/forward", s.handleForward, requireChannelPermission(authz.PermChannelRead))

	// Messages written now and sent later.
	rooms.POST("/:id/scheduled", s.handleScheduleMessage,
		s.requireFeature(settings.FeatureScheduledMessages),
		s.requirePosting())
	rooms.GET("/:id/threads/:eventId", s.handleThread, requireChannelPermission(authz.PermChannelRead))
	rooms.POST("/:id/threads/:eventId/replies", s.handleReplyInThread, s.requirePosting())
	// Where somebody stands with one thread: how far they have read it, and
	// whether its replies reach them. Their own, so only membership is asked.
	rooms.POST("/:id/threads/:eventId/read", s.handleMarkThreadRead)
	rooms.PUT("/:id/threads/:eventId/follow", s.handleFollowThread)
	rooms.DELETE("/:id/threads/:eventId/follow", s.handleUnfollowThread)
	rooms.POST("/:id/read", s.handleMarkRead)
	rooms.GET("/:id/presence", s.handleRoomPresence, requireChannelPermission(authz.PermChannelRead))
	rooms.POST("/:id/typing", s.handleStartTyping)
	rooms.GET("/:id/typing", s.handleListTyping, requireChannelPermission(authz.PermChannelRead))

	// Attachments. Uploaded to a room before the message that carries them, so
	// a refusal costs only the upload and a failed send does not lose the file.
	// Only uploading is gated. Turning attachments off should stop new files
	// arriving, not hide the ones people already sent and still refer to.
	rooms.POST("/:id/attachments", s.handleUploadAttachment,
		s.requireFeature(settings.FeatureAttachments))
	s.echo.GET("/api/attachments/limits", s.handleAttachmentLimits, requireAuthentication)
	s.echo.GET("/api/attachments/:id", s.handleDownloadAttachment, requireAuthentication)
	s.echo.GET("/api/attachments/:id/preview", s.handleAttachmentPreview, requireAuthentication)
	// A caption belongs to the file, so several sent together can each say
	// something different.
	s.echo.PUT("/api/attachments/:id/caption", s.handleCaptionAttachment, requireAuthentication)
	// Marking media sensitive, which whoever sent it may do to their own and a
	// moderator may do to anybody's. No permission on the route: which of the
	// two applies is decided in the handler, and requiring the moderation
	// permission here would stop an uploader marking their own file.
	s.echo.PUT("/api/attachments/:id/sensitive", s.handleMarkSensitive, requireAuthentication)

	// Notifications, and what a person wants to be told about.
	me.GET("/notifications", s.handleListNotifications)
	me.GET("/notifications/count", s.handleNotificationCount)
	me.POST("/notifications/:id/read", s.handleMarkNotificationRead)
	// Read back as well as set, so a client can show which conversations are
	// muted rather than only offering to mute them.
	me.GET("/preferences/notifications", s.handleGetNotificationPreferences)
	me.PUT("/preferences/notifications", s.handleSetNotificationPreference)
	// A periodic summary by mail instead of interruptions. Both routes answer
	// 404 on a server that cannot send one, as a disabled feature does.
	me.GET("/preferences/digest", s.handleGetDigest, s.requireDigests)
	me.PUT("/preferences/digest", s.handleSetDigest, s.requireDigests)

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

	// Scheduled messages a person has outstanding: changing one, sending it
	// now rather than later, and cancelling it.
	s.echo.GET("/api/me/scheduled", s.handleListScheduled, requireAuthentication)
	s.echo.PATCH("/api/me/scheduled/:id", s.handleChangeScheduled, requireAuthentication)
	s.echo.POST("/api/me/scheduled/:id/send", s.handleSendScheduledNow, requireAuthentication)
	s.echo.DELETE("/api/me/scheduled/:id", s.handleCancelScheduled, requireAuthentication)

	// Reporting abuse. Filing is open to anybody signed in — requiring
	// membership would stop somebody reporting what they saw before leaving —
	// while the queue is for whoever moderates.
	s.echo.GET("/api/messages/lifetime-kinds", s.handleLifetimeKinds, requireAuthentication)

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
	// Redeeming is gated alongside issuing: turning invitations off leaves
	// outstanding ones unusable, which is the point of the switch rather than
	// an oversight — an operator reaching for it wants the flow stopped now,
	// not once the last issued link expires.
	s.echo.POST("/api/invitations/redeem", s.handleRedeemInvitation, requireAuthentication,
		s.requireFeature(settings.FeatureInvitations))
	s.echo.DELETE("/api/invitations/:id", s.handleRevokeInvitation, requireAuthentication,
		requirePermission(authz.PermChannelInvite))

	s.echo.GET("/api/gifs", s.handleSearchGifs, requireAuthentication,
		s.requireFeature(settings.FeatureGifSearch))

	/*
		Inline translation. Preferences and connections are a person's own;
		the proxy checks the room like every room route and each message
		besides, so it translates only what the person may already read.
	*/
	translate := s.requireFeature(settings.FeatureTranslation)
	s.echo.GET("/api/me/translation", s.handleTranslationOverview, requireAuthentication, translate)
	s.echo.PUT("/api/me/translation/preferences", s.handleSetTranslationPreferences, requireAuthentication, translate)
	s.echo.PUT("/api/me/translation/connections/:provider", s.handleConnectTranslation, requireAuthentication, translate)
	s.echo.DELETE("/api/me/translation/connections/:provider", s.handleDisconnectTranslation, requireAuthentication, translate)
	s.echo.PUT("/api/me/translation/rooms/:id", s.handleSetTranslationRoomRule, requireAuthentication, translate)
	s.echo.DELETE("/api/me/translation/rooms/:id", s.handleClearTranslationRoomRule, requireAuthentication, translate)
	s.echo.POST("/api/rooms/:id/translations", s.handleTranslateMessages, requireAuthentication, translate)

	s.echo.GET("/api/search", s.handleSearch, requireAuthentication)
	// One box, one question: messages, conversations and people together,
	// rather than three requests resolving at different times.
	s.echo.GET("/api/search/everything", s.handleSearchEverything, requireAuthentication)

	// Link previews. A POST because the link is private: a query string ends
	// up in access logs, proxy logs and browser history, and a body does not.
	s.echo.POST("/api/previews", s.handlePreviewLink, requireAuthentication,
		s.requireFeature(settings.FeatureLinkPreviews))
	me.PUT("/preferences/link-previews", s.handleSetPreviewPreference)

	/*
		Notifications a closed browser can receive.

		The configuration is readable without a session, because a client asks
		what the server offers before it asks somebody to grant permission —
		and it carries only the public key, which every subscriber receives
		anyway.
	*/
	s.echo.GET("/api/push/config", s.handlePushConfig)
	me.GET("/push/subscriptions", s.handleListPushSubscriptions)
	me.POST("/push/subscriptions", s.handleSubscribeToPush)
	me.DELETE("/push/subscriptions/:id", s.handleUnsubscribeFromPush)
	me.GET("/preferences/hide-push-content", s.handleGetHidePushContent)
	me.PUT("/preferences/hide-push-content", s.handleSetHidePushContent)
	/*
		What the interface looks like, on the account rather than in a browser,
		so the choice follows somebody to the desktop client and back.
	*/
	me.GET("/preferences/appearance", s.handleGetAppearance)
	me.PUT("/preferences/appearance", s.handleSetAppearance)
	/*
		Which introductions have been given, on the account for the same
		reason: one shown on the web client should not be shown again on the
		desktop one.
	*/
	me.GET("/preferences/tours", s.handleGetTourProgress)
	me.PUT("/preferences/tours", s.handleSetTourProgress)

	// Direct conversations, which are found by their participants rather than
	// by name.
	direct := s.echo.Group("/api/direct", requireAuthentication)
	direct.GET("", s.handleListDirect)
	direct.POST("", s.handleOpenDirect)

	// Presence and unread state for the signed-in person.
	me.GET("/unread", s.handleUnreadCounts)
	me.GET("/status", s.handleGetStatus)
	me.PUT("/status", s.handleSetStatus)
	// What the server says on somebody's behalf while they are away.
	me.GET("/automatic-replies", s.handleListAutomaticReplies)
	me.POST("/automatic-replies", s.handleCreateAutomaticReply)
	me.PUT("/automatic-replies/:id", s.handleUpdateAutomaticReply)
	me.DELETE("/automatic-replies/:id", s.handleDeleteAutomaticReply)
	s.echo.GET("/api/statuses", s.handleListStatuses, requireAuthentication)

	// The live connection. Authenticated like any other route; a connection is
	// never established for an unauthenticated caller.
	s.echo.GET("/api/live", s.handleWebSocket, requireAuthentication)

	// Administration. Each route names the permission that governs it, so
	// authority is visible at the routing table rather than buried in a
	// handler.
	admin := s.echo.Group("/api/admin", requireAuthentication)
	// Reading the catalogue and the roles is open to whoever assigns roles as
	// well as whoever makes them: handing somebody a role means knowing which
	// roles there are and what each allows.
	admin.GET("/permissions", s.handlePermissionCatalogue,
		requireAnyPermission(authz.PermRoleManage, authz.PermRoleAssign))

	/*
		Roles, and who holds them.

		All under role.manage, which is administrative: deciding who may do what
		is the authority that confers every other one. The service beneath has
		been complete since the authorization work — what was missing was any
		way to reach it, which left a server with one role and one person able
		to use it.
	*/
	admin.GET("/roles", s.handleListRoles,
		requireAnyPermission(authz.PermRoleManage, authz.PermRoleAssign))
	admin.POST("/roles", s.handleCreateRole,
		requirePermission(authz.PermRoleManage))
	admin.PATCH("/roles/:id", s.handleUpdateRole,
		requirePermission(authz.PermRoleManage))
	admin.DELETE("/roles/:id", s.handleDeleteRole,
		requirePermission(authz.PermRoleManage))
	admin.PUT("/roles/:id/permissions", s.handleSetRolePermission,
		requirePermission(authz.PermRoleManage))
	admin.DELETE("/roles/:id/permissions/:permission", s.handleClearRolePermission,
		requirePermission(authz.PermRoleManage))
	// Who holds a role, and what somebody may do and why: reading either is
	// what deciding who to assign needs, so either authority opens them.
	admin.GET("/roles/:id/members", s.handleListRoleMembers,
		requireAnyPermission(authz.PermRoleManage, authz.PermRoleAssign))
	admin.GET("/users/:id/permissions", s.handleExplainUser,
		requireAnyPermission(authz.PermRoleManage, authz.PermRoleAssign))
	admin.POST("/roles/:id/members", s.handleAssignRole,
		requirePermission(authz.PermRoleAssign))
	admin.DELETE("/roles/:id/members/:user", s.handleRemoveRole,
		requirePermission(authz.PermRoleAssign))
	admin.GET("/audit", s.handleReadAudit,
		requirePermission(authz.PermServerViewAudit))

	// Invitations to the server itself, for the registration policy that
	// requires one.
	admin.GET("/invitations", s.handleListServerInvitations,
		requirePermission(authz.PermUserInvite))
	admin.POST("/invitations", s.handleIssueServerInvitation,
		requirePermission(authz.PermUserInvite),
		s.requireFeature(settings.FeatureInvitations))

	// Who is on this server: every account, for whoever may read every
	// account, and the place suspension is done from.
	admin.GET("/users", s.handleListPeople,
		requireAnyPermission(authz.PermUserViewAll, authz.PermUserSuspend))

	// Suspension, which is server-wide where a ban is confined to a channel.
	admin.POST("/users/:id/suspend", s.handleSuspendUser,
		requirePermission(authz.PermUserSuspend))
	admin.DELETE("/users/:id/suspend", s.handleLiftSuspension,
		requirePermission(authz.PermUserSuspend))

	// Posting restrictions across the whole server: reading everything still,
	// posting nowhere. The channel kind lives with the channel's routes.
	admin.GET("/restrictions", s.handleListRestrictionsEverywhere,
		requirePermission(authz.PermModerationRestrict))
	admin.POST("/restrictions", s.handleRestrictPostingEverywhere,
		requirePermission(authz.PermModerationRestrict))
	admin.DELETE("/restrictions/:restrictionId", s.handleLiftRestrictionEverywhere,
		requirePermission(authz.PermModerationRestrict))

	// Policy an operator changes while the server runs: who may register, how
	// much this server will hold.
	admin.GET("/settings", s.handleListSettings,
		requirePermission(authz.PermServerConfigure))
	admin.PUT("/settings/:key", s.handleSetSetting,
		requirePermission(authz.PermServerConfigure))

	// Whether a peer can be reached and will accept our signature. A
	// diagnostic an operator runs before trusting anything to the transport.
	admin.POST("/federation/ping", s.handleFederationPing,
		requirePermission(authz.PermServerConfigure))

	// Which rooms cross between servers. A server-level decision rather than a
	// room moderator's: it exposes the room's contents beyond this server.
	admin.PUT("/rooms/:id/federation", s.handleFederateRoom,
		requirePermission(authz.PermServerConfigure))
	admin.GET("/rooms/:id/federation", s.handleRoomPeers,
		requirePermission(authz.PermServerConfigure))
	admin.POST("/rooms/:id/federation/deliver", s.handleDeliverRoom,
		requirePermission(authz.PermServerConfigure))

	// Adopting a room another server owns, and asking that server for what
	// this one has not seen.
	admin.POST("/federation/adopt", s.handleAdoptRoom,
		requirePermission(authz.PermServerConfigure))
	admin.POST("/rooms/:id/federation/catchup", s.handleCatchUp,
		requirePermission(authz.PermServerConfigure))
	admin.POST("/rooms/:id/federation/resume", s.handleResumePeer,
		requirePermission(authz.PermServerConfigure))

	// Feature toggles. Listed and changed separately from settings because a
	// toggle is on or off where a setting is free text, and because one
	// governs both what is advertised and what is accepted.
	// The startup configuration, read-only: these come from the environment or
	// the configuration file and cannot change without a restart.
	// The whole log, as newline-delimited JSON. Separate from reading a page
	// of it because taking a copy is a different act.
	admin.GET("/audit/export", s.handleExportAudit,
		requirePermission(authz.PermServerViewAudit))

	admin.GET("/configuration", s.handleInspectConfiguration,
		requirePermission(authz.PermServerConfigure))

	admin.GET("/features", s.handleListFeatures,
		requirePermission(authz.PermServerConfigure))
	admin.PUT("/features/:feature", s.handleSetFeature,
		requirePermission(authz.PermServerConfigure))

	// The other half of a limit: what is being used against what is allowed,
	// so an operator sees it coming rather than discovering it from a refusal.
	admin.GET("/usage", s.handleUsage,
		requirePermission(authz.PermServerConfigure))

	// Features whose routes live in files of their own.
	for _, register := range routeRegistrars {
		register(s)
	}

	// Registered last: the catch-all serving the web application must not
	// shadow an endpoint above it.
	s.mountWebApp(s.config.WebRoot)
}

/*
routeRegistrars are features that mount their own routes.

A feature large enough to have a file of its own registers here from init
rather than adding lines to routes(), which every feature would otherwise be
editing at once. They run before the web application's catch-all, so nothing
they mount is shadowed by it.
*/
var routeRegistrars []func(*Server)

// registerRoutes adds a feature's route mounting. Called from init.
func registerRoutes(register func(*Server)) {
	routeRegistrars = append(routeRegistrars, register)
}

// healthResponse reports whether the server can serve requests.
type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
	// Maintenance is set while a restore runs. The status stays "ok": the
	// node is working, and an orchestrator that killed it would kill the
	// restore with it.
	Maintenance bool `json:"maintenance,omitempty"`
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

	response := healthResponse{Status: status, Checks: checks}
	if s.backups != nil && status == statusOK {
		response.Maintenance = s.backups.Maintenance(c.Request().Context()).Active
	}
	return c.JSON(code, response)
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
	/*
		ObscureSensitiveMedia tells a client to cover media marked sensitive
		until the viewer asks to see it.

		Sent here rather than left to each client to decide, because it is the
		operator's policy and a client choosing for itself would mean the same
		server behaving differently in the web app and the desktop one.

		Unauthenticated, like everything else here: the policy is not a secret,
		and a client needs it before the first message renders.
	*/
	ObscureSensitiveMedia bool `json:"obscureSensitiveMedia"`

	/*
		Branding an operator set, applied by clients without a rebuild.

		Sent alongside the canonical address rather than instead of it. Nothing
		here can remove that field: a client shows what the operator chose, and
		still shows which server it is talking to.
	*/
	Branding brandingResponse `json:"branding"`

	/*
		The sign-in providers this server offers, if any.

		Unauthenticated because the sign-in screen needs it before anybody has
		signed in, and it is not a secret: a name and a label, which is what a
		button needs and nothing more.

		Absent rather than empty on a server with none configured, so a client
		can tell "no providers" from "an older server that does not know about
		them".
	*/
	SignInProviders []oauth.Offered `json:"signInProviders,omitempty"`

	/*
		Whether this server signs people in with passkeys.

		A client needs it before the sign-in form is drawn — a button offering
		something the server cannot do is worse than no button — and it is not
		a secret: anybody may ask for a challenge.

		False rather than absent, unlike the providers above: this is one
		boolean with a meaningful default, where an empty list and an older
		server reading nothing are genuinely different things.
	*/
	Passkeys bool `json:"passkeys"`

	/*
		Whether this server can mail somebody a way back into their account.

		A client needs it before drawing "forgotten your password", which
		otherwise leads to a form that answers with a refusal — and on a server
		with no mail that link is worse than absent, because it promises
		something nobody can deliver.
	*/
	Recovery bool `json:"recovery"`

	// Legal links to the operator's own policies, absent where none is set.
	Legal *legalResponse `json:"legal,omitempty"`
}

// brandingResponse is what an operator may change about how this server looks.
type brandingResponse struct {
	// Name is what the server calls itself in an interface, falling back to
	// the configured name so an operator who sets nothing still sees something.
	Name string `json:"name"`
	// Initials are shown where there is no room for a name.
	Initials string `json:"initials"`
	// LogoURL is shown in place of the initials, when set.
	LogoURL string `json:"logoUrl,omitempty"`
	// Accent is a CSS colour used for emphasis.
	Accent string `json:"accent,omitempty"`
	/*
		Palette is which set of colours this instance uses by default.

		What somebody sees who has chosen nothing for themselves, which is most
		people and everybody on their first visit.
	*/
	Palette string `json:"palette,omitempty"`
}

/*
capabilities lists the optional features this server actually offers.

Advertised rather than assumed, because every one of these depends on an
operator's choice: a client that offered GIF search on a server without a
provider would show a control that always fails.

Read from the feature toggles — the same values requireFeature enforces on. A
capability in this list is therefore one the server will actually accept
requests for, and one absent from it will be refused. Two separate lists would
drift, and the direction they drift in is the dangerous one: a feature
advertised as off that still works.

A toggle is necessary but not sufficient. GIF search and link previews also
need something configured — a provider, a fetcher — and a server with the
toggle on but no provider would advertise a control that always fails. So both
must hold.
*/
func (s *Server) capabilities(ctx context.Context) []string {
	if s.settings == nil {
		return []string{}
	}

	capabilities := []string{}
	for _, feature := range s.settings.EnabledSet(ctx) {
		switch settings.Feature(feature) {
		case settings.FeatureGifSearch:
			if s.gifs == nil || !s.gifs.Enabled() {
				continue
			}
		case settings.FeatureLinkPreviews:
			if s.unfurl == nil || !s.unfurl.Enabled() {
				continue
			}
		case settings.FeatureNotificationDigests:
			// A summary is a mail, and a server with no mail server cannot
			// send one however the toggle is set.
			if s.mail == nil || !s.mail.Configured() {
				continue
			}
		}
		capabilities = append(capabilities, feature)
		// Recording is a setting within calls, offered only where calls are.
		if settings.Feature(feature) == settings.FeatureCalls && s.settings.Get(ctx, settings.CallsRecording) == "true" {
			capabilities = append(capabilities, "calls.recording")
		}
	}

	return capabilities
}

func (s *Server) handleServerInfo(c *echo.Context) error {
	/*
		A server that is not discoverable by this caller says nothing about
		itself.

		404 rather than 403: a refusal that distinguished "you may not know"
		from "there is nothing here" would answer the question it is refusing.
		Somebody scanning for servers learns the same thing from this address
		as from one that has never existed.

		Somebody signed in is past this, so a private server's own members are
		unaffected — and somebody holding an invitation still reaches it, since
		this governs discovery rather than access.
	*/
	if !s.mayDiscover(c) {
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}

	return c.JSON(http.StatusOK, serverInfoResponse{
		Name:             s.config.ServerName,
		Description:      s.config.ServerDescription,
		CanonicalAddress: s.config.CanonicalAddress,
		Version:          buildinfo.Version,
		// The federation protocols this server speaks, newest first, or none
		// where the operator has not switched federation on.
		ProtocolVersions: s.protocolVersions(c),
		// Optional capabilities are advertised as they are built, so a client
		// adapts to what this server actually offers rather than presenting a
		// control that cannot work.
		Capabilities:          s.capabilities(c.Request().Context()),
		DefaultLanguage:       s.config.DefaultLanguage,
		ObscureSensitiveMedia: s.obscureSensitiveMedia(c.Request().Context()),
		Branding:              s.branding(c.Request().Context()),
		SignInProviders:       s.signInProviders(),
		Passkeys:              s.passkeys.Enabled(),
		Recovery:              s.recovery != nil && s.recovery.Available(),
		Legal:                 s.legal(c.Request().Context()),
	})
}

/*
obscureSensitiveMedia reports the operator's policy on covering marked media.

Defaults to covering when the setting cannot be read. A server that showed
everything because a settings query timed out would expose exactly what
somebody marked to avoid, where the opposite failure covers something
harmlessly.
*/
func (s *Server) obscureSensitiveMedia(ctx context.Context) bool {
	if s.settings == nil {
		return true
	}
	return s.settings.Get(ctx, settings.ObscureSensitiveMedia) != "false"
}

/*
branding is what an operator has chosen about how this server looks.

Falls back to the configured name, so a server nobody has branded still calls
itself something. The canonical address is not part of this and cannot be
changed by it — which is the whole point: an operator may decide what their
server is called and never what it is.
*/
func (s *Server) branding(ctx context.Context) brandingResponse {
	branding := brandingResponse{Name: s.config.ServerName}

	if s.settings == nil {
		branding.Initials = initialsOf(branding.Name)
		return branding
	}

	if name := strings.TrimSpace(s.settings.Get(ctx, settings.BrandName)); name != "" {
		branding.Name = name
	}
	branding.Initials = strings.TrimSpace(s.settings.Get(ctx, settings.BrandInitials))
	if branding.Initials == "" {
		branding.Initials = initialsOf(branding.Name)
	}

	branding.LogoURL = strings.TrimSpace(s.settings.Get(ctx, settings.BrandLogoURL))
	branding.Accent = strings.TrimSpace(s.settings.Get(ctx, settings.BrandAccent))
	branding.Palette = strings.TrimSpace(s.settings.Get(ctx, settings.BrandPalette))

	return branding
}

/*
initialsOf reduces a name to the letters shown where there is no room for it.

The first letter of each of the first three words, which turns "Tanzania
Developers Community" into TDC and leaves a one-word name as its first letter
rather than nothing.
*/
func initialsOf(name string) string {
	var initials []rune

	for _, word := range strings.Fields(name) {
		for _, letter := range word {
			initials = append(initials, unicode.ToUpper(letter))
			break
		}
		if len(initials) == 3 {
			break
		}
	}

	if len(initials) == 0 {
		return "?"
	}
	return string(initials)
}

// signInProviders is what a sign-in screen draws its buttons from.
func (s *Server) signInProviders() []oauth.Offered {
	if s.oauth == nil {
		return nil
	}
	return s.oauth.Offered()
}

// protocolVersions are the federation protocols advertised to a caller: the
// ones this build speaks, while federation is on, and none while it is off.
func (s *Server) protocolVersions(c *echo.Context) []string {
	if !s.federationEnabled(c) {
		return []string{}
	}
	return federation.SupportedVersions()
}
