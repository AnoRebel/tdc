package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Links to a conversation's call.

Making, listing and revoking them takes call.link, which nobody holds until an
operator grants it. Reading what a link leads to needs no account — somebody
opening one has to be told what they are about to join — and says only the
conversation's name and who is in the call. Using one needs an account, and
lets the person into the call alone, as merging does.
*/

func init() {
	registerRoutes(func(s *Server) {
		if s.rooms == nil {
			return
		}
		feature := s.requireFeature(settings.FeatureCalls)
		links := s.echo.Group("/api/rooms/:id/call/links", requireAuthentication, feature,
			requireChannelPermission(authz.PermCallLink))
		links.GET("", s.handleListCallLinks)
		links.POST("", s.handleCreateCallLink)
		links.DELETE("/:linkId", s.handleRevokeCallLink)

		s.echo.GET("/api/call-links/:secret", s.handleReadCallLink, feature)
		s.echo.POST("/api/call-links/:secret/join", s.handleUseCallLink, requireAuthentication, feature)
		s.echo.POST("/api/call-links/:secret/guest", s.handleJoinAsGuest, feature)

		lobby := s.echo.Group("/api/calls/:callId/lobby", requireAuthentication, feature)
		lobby.GET("", s.handleCallLobby)
		lobby.POST("/:userId/admit", s.handleAdmit(true))
		lobby.POST("/:userId/turn-away", s.handleAdmit(false))
		lobby.DELETE("", s.handleLeaveLobby)
	})

	documentRoutes(map[string]entry{
		"GET /api/rooms/:id/call/links":               {Summary: "Links to a conversation's call", Authentication: "session or token", Permission: "call.link"},
		"POST /api/rooms/:id/call/links":              {Summary: "Make a link to a conversation's call", Authentication: "session or token", Permission: "call.link"},
		"DELETE /api/rooms/:id/call/links/:linkId":    {Summary: "Revoke a call link", Authentication: "session or token", Permission: "call.link"},
		"GET /api/call-links/:secret":                 {Summary: "What a call link leads to", Authentication: "none"},
		"POST /api/call-links/:secret/guest":          {Summary: "Join a call by its link without an account, where guests are allowed", Authentication: "none"},
		"POST /api/call-links/:secret/join":           {Summary: "Join a call by its link, or wait in its lobby", Authentication: "session or token"},
		"GET /api/calls/:callId/lobby":                {Summary: "Who is waiting to be let into a call", Authentication: "session or token"},
		"POST /api/calls/:callId/lobby/:userId/admit": {Summary: "Let somebody waiting into a call", Authentication: "session or token"},
		"POST /api/calls/:callId/lobby/:userId/turn-away": {
			Summary: "Turn away somebody waiting to join a call", Authentication: "session or token",
		},
		"DELETE /api/calls/:callId/lobby": {Summary: "Stop waiting to join a call", Authentication: "session or token"},
	})
}

// Passcode attempts allowed from one address, across links.
var passcodeAttemptsPerAddress = cache.RateLimit{Attempts: 10, Window: 15 * time.Minute}

// How long a link may be made to last, at most.
const maxLinkLifetime = 90 * 24 * time.Hour

type callLinkRequest struct {
	Label    string `json:"label"`
	Passcode string `json:"passcode"`
	Lobby    bool   `json:"lobby"`
	// Guests lets people without an account use the link, where the server
	// allows guests at all.
	Guests bool `json:"guests"`
	// ExpiresInHours is how long the link lasts; zero lasts until revoked.
	ExpiresInHours int `json:"expiresInHours"`
	// MaxUses is how many people it lets in; zero is unlimited.
	MaxUses int `json:"maxUses"`
}

type callLinkView struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	Protected bool    `json:"protected"`
	Lobby     bool    `json:"lobby"`
	Guests    bool    `json:"guests"`
	ExpiresAt *string `json:"expiresAt,omitempty"`
	MaxUses   *int32  `json:"maxUses,omitempty"`
	Uses      int32   `json:"uses"`
	CreatedAt string  `json:"createdAt"`
	// Secret is present only in the answer to making the link.
	Secret string `json:"secret,omitempty"`
}

func callLinkViewOf(link dbgen.CallLink) callLinkView {
	view := callLinkView{
		ID: link.ID.String(), Label: link.Label, Protected: link.PasscodeHash != nil,
		Lobby: link.Lobby, Guests: link.Guests, MaxUses: link.MaxUses, Uses: link.Uses,
		CreatedAt: link.CreatedAt.UTC().Format(time.RFC3339),
	}
	if link.ExpiresAt != nil {
		at := link.ExpiresAt.UTC().Format(time.RFC3339)
		view.ExpiresAt = &at
	}
	return view
}

func (s *Server) handleCreateCallLink(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request callLinkRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if len([]rune(request.Label)) > 80 {
		return echo.NewHTTPError(http.StatusBadRequest, "a link's label is a few words")
	}
	if request.Passcode != "" && len([]rune(request.Passcode)) < 4 {
		return echo.NewHTTPError(http.StatusBadRequest, "a passcode needs at least four characters")
	}
	options := rooms.LinkOptions{
		Label: request.Label, Passcode: request.Passcode, Lobby: request.Lobby, Guests: request.Guests,
	}
	if request.ExpiresInHours < 0 || request.MaxUses < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a link's lifetime and uses cannot be negative")
	}
	if request.ExpiresInHours > 0 {
		lifetime := min(time.Duration(request.ExpiresInHours)*time.Hour, maxLinkLifetime)
		at := time.Now().Add(lifetime)
		options.Expires = &at
	}
	if request.MaxUses > 0 {
		uses := int32(min(request.MaxUses, 10000))
		options.MaxUses = &uses
	}
	link, secret, err := s.rooms.CreateCallLink(ctx, roomID, user.ID, options)
	if err != nil {
		return callError(err)
	}
	s.recordLinkAudit(c, audit.ActionCallLinkCreated, link.ID)
	view := callLinkViewOf(link)
	view.Secret = secret
	return c.JSON(http.StatusCreated, view)
}

func (s *Server) handleListCallLinks(c *echo.Context) error {
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if _, err := s.rooms.RequireMembership(c.Request().Context(), roomID, user.ID); err != nil {
		return roomError(err)
	}
	links, err := s.rooms.CallLinks(c.Request().Context(), roomID)
	if err != nil {
		return err
	}
	views := make([]callLinkView, 0, len(links))
	for _, link := range links {
		views = append(views, callLinkViewOf(link))
	}
	return c.JSON(http.StatusOK, views)
}

func (s *Server) handleRevokeCallLink(c *echo.Context) error {
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	linkID, err := uuid.Parse(c.Param("linkId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such link")
	}
	if _, err := s.rooms.RequireMembership(c.Request().Context(), roomID, user.ID); err != nil {
		return roomError(err)
	}
	if err := s.rooms.RevokeCallLink(c.Request().Context(), roomID, linkID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such link")
	}
	s.recordLinkAudit(c, audit.ActionCallLinkRevoked, linkID)
	return c.NoContent(http.StatusNoContent)
}

// callLinkTarget is what somebody holding a link is told before using it.
type callLinkTarget struct {
	RoomName  string `json:"roomName"`
	Protected bool   `json:"protected"`
	Lobby     bool   `json:"lobby"`
	// Guests says whether somebody without an account may use this link now.
	Guests bool `json:"guests"`
	// Started says whether the call is going on; a link to one that has not
	// started yet waits for it.
	Started bool `json:"started"`
	// People are who is in the call, by name.
	People []string `json:"people"`
}

/*
handleReadCallLink says what a link leads to: the conversation's name, whether
it asks for a passcode or waits in a lobby, and who is in the call. Nothing
else of the conversation — not its members, not its messages.

Unusable links are refused alike, whatever made them so.
*/
func (s *Server) handleReadCallLink(c *echo.Context) error {
	ctx := c.Request().Context()
	link, err := s.rooms.CallLink(ctx, c.Param("secret"))
	if err != nil {
		return linkError(err)
	}
	room, err := s.rooms.ByID(ctx, link.RoomID)
	if err != nil {
		return linkError(rooms.ErrLinkUnusable)
	}
	target := callLinkTarget{
		RoomName: room.Name, Protected: link.PasscodeHash != nil, Lobby: link.Lobby,
		Guests: link.Guests && s.settings.Get(ctx, settings.CallsGuests) == "true", People: []string{},
	}
	if call, err := s.rooms.OpenCallIn(ctx, link.RoomID); err == nil {
		target.Started = true
		for _, participant := range call.Participants {
			target.People = append(target.People, participant.Username)
		}
	}
	return c.JSON(http.StatusOK, target)
}

type useLinkRequest struct {
	Passcode string `json:"passcode"`
}

// linkUse is what using a link answers: the call to join, or that the person
// is waiting to be let in.
type linkUse struct {
	CallID  string `json:"callId"`
	RoomID  string `json:"roomId"`
	Waiting bool   `json:"waiting"`
}

func (s *Server) handleUseCallLink(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	var request useLinkRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	link, err := s.rooms.CallLink(ctx, c.Param("secret"))
	if err != nil {
		return linkError(err)
	}
	if err := s.checkLinkPasscode(c, link, request.Passcode); err != nil {
		return err
	}
	return s.enterByLink(c, link, user.ID)
}

/*
checkLinkPasscode applies a protected link's passcode, with failures limited
from each address across links and on the link itself, and every wrong one
recorded where an operator reads the audit log.
*/
func (s *Server) checkLinkPasscode(c *echo.Context, link dbgen.CallLink, passcode string) error {
	if link.PasscodeHash == nil {
		return nil
	}
	ctx := c.Request().Context()
	if s.limiter != nil {
		decision, err := s.limiter.Allow(ctx, "call.link.passcode:"+requestIP(c).String(), passcodeAttemptsPerAddress)
		if err == nil && !decision.Allowed {
			return echo.NewHTTPError(http.StatusTooManyRequests, "too many passcodes tried from here; try again later")
		}
	}
	err := s.rooms.CheckPasscode(ctx, link, passcode)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, rooms.ErrLinkLocked):
		return echo.NewHTTPError(http.StatusTooManyRequests, err.Error())
	case errors.Is(err, rooms.ErrWrongPasscode):
		s.recordLinkAudit(c, audit.ActionCallLinkPasscodeFailed, link.ID)
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	return err
}

// enterByLink lets somebody in by a link whose passcode, if any, was right.
func (s *Server) enterByLink(c *echo.Context, link dbgen.CallLink, userID uuid.UUID) error {
	ctx := c.Request().Context()
	call, waiting, err := s.rooms.UseCallLink(ctx, link, userID)
	if err != nil {
		return linkError(err)
	}
	if waiting {
		s.announceLobby(ctx, call)
	}
	return c.JSON(http.StatusOK, linkUse{CallID: call.ID.String(), RoomID: call.RoomID.String(), Waiting: waiting})
}

type lobbyEntry struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Since       string `json:"since"`
}

type lobbyView struct {
	Call    string       `json:"call"`
	RoomID  string       `json:"roomId"`
	Waiting []lobbyEntry `json:"waiting"`
}

func (s *Server) lobbyOf(ctx context.Context, call rooms.Call) lobbyView {
	view := lobbyView{Call: call.ID.String(), RoomID: call.RoomID.String(), Waiting: []lobbyEntry{}}
	waiting, err := s.rooms.CallLobby(ctx, call.ID)
	if err != nil {
		return view
	}
	for _, each := range waiting {
		view.Waiting = append(view.Waiting, lobbyEntry{
			UserID: each.UserID.String(), Username: each.Username, DisplayName: each.DisplayName,
			Since: each.RequestedAt.UTC().Format(time.RFC3339),
		})
	}
	return view
}

// announceLobby tells everybody in a call who is waiting to be let in.
func (s *Server) announceLobby(ctx context.Context, call rooms.Call) {
	if s.hub == nil {
		return
	}
	payload, _ := json.Marshal(s.lobbyOf(ctx, call))
	frame := realtime.Frame{Type: realtime.FrameCallLobby, Payload: payload}
	for _, participant := range call.Participants {
		here := s.hub.SendToUser(participant.UserID, frame)
		s.whisperElsewhere(ctx, participant.UserID, frame, here)
	}
}

func (s *Server) handleCallLobby(c *echo.Context) error {
	call, err := s.callByID(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s.lobbyOf(c.Request().Context(), call))
}

func (s *Server) handleAdmit(admit bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		user, _ := currentUser(c)
		call, err := s.callByID(c)
		if err != nil {
			return err
		}
		waitingID, err := uuid.Parse(c.Param("userId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "nobody is waiting by that name")
		}
		if err := s.rooms.Admit(ctx, call.ID, user.ID, waitingID, admit); err != nil {
			if errors.Is(err, rooms.ErrLinkUnusable) {
				return linkError(err)
			}
			return echo.NewHTTPError(http.StatusNotFound, "nobody is waiting by that name")
		}
		frameType := realtime.FrameCallTurnedAway
		if admit {
			frameType = realtime.FrameCallAdmitted
		}
		payload, _ := json.Marshal(linkUse{CallID: call.ID.String(), RoomID: call.RoomID.String()})
		frame := realtime.Frame{Type: frameType, Payload: payload}
		here := s.hub.SendToUser(waitingID, frame)
		s.whisperElsewhere(ctx, waitingID, frame, here)
		s.announceLobby(ctx, call)
		return c.NoContent(http.StatusNoContent)
	}
}

func (s *Server) handleLeaveLobby(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	callID, err := uuid.Parse(c.Param("callId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "there is no call here")
	}
	s.rooms.LeaveLobby(ctx, callID, user.ID)
	if record, err := s.rooms.CallRecord(ctx, callID); err == nil {
		if call, err := s.rooms.OpenCallIn(ctx, record.RoomID); err == nil {
			s.announceLobby(ctx, call)
		}
	}
	return c.NoContent(http.StatusNoContent)
}

func linkError(err error) error {
	switch {
	case errors.Is(err, rooms.ErrLinkUnusable):
		return echo.NewHTTPError(http.StatusNotFound, "this link has expired, been used up or been withdrawn")
	case errors.Is(err, rooms.ErrCallNotStarted):
		return echo.NewHTTPError(http.StatusConflict, "the call has not started yet")
	}
	return callError(err)
}

func (s *Server) recordLinkAudit(c *echo.Context, action audit.Action, linkID uuid.UUID) {
	if s.audit == nil {
		return
	}
	entry := audit.Entry{
		Action: action, TargetKind: "call_link", TargetID: linkID.String(),
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}
	if user, ok := currentUser(c); ok {
		entry.ActorID = &user.ID
		entry.ActorLabel = user.Username
	}
	if err := s.audit.Record(c.Request().Context(), entry); err != nil {
		s.log.Warn("could not record a call link in the audit log", "error", err)
	}
}

// Guests one address may let in, so a link cannot be used to fill a call or the
// accounts table from one machine.
var guestsPerAddress = cache.RateLimit{Attempts: 5, Window: 10 * time.Minute}

type guestRequest struct {
	// Name is what the guest is shown as in the call.
	Name     string `json:"name"`
	Passcode string `json:"passcode"`
}

/*
handleJoinAsGuest lets somebody without an account into a call, through a link
made to allow guests, on a server that allows them at all.

They give a name to be shown by, and the link's passcode if it has one. They
are given a guest account for this call and a session for it — which reaches
the call and nothing else, and stops when the call ends — and then go in, or
into the lobby, exactly as anybody using the link does. Everybody in the call
sees them marked as a guest.
*/
func (s *Server) handleJoinAsGuest(c *echo.Context) error {
	ctx := c.Request().Context()
	var request guestRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	link, err := s.rooms.CallLink(ctx, c.Param("secret"))
	if err != nil {
		return linkError(err)
	}
	if !link.Guests || s.settings.Get(ctx, settings.CallsGuests) != "true" {
		return echo.NewHTTPError(http.StatusForbidden, "this call takes only people with an account here; sign in to join")
	}
	if s.limiter != nil {
		decision, err := s.limiter.Allow(ctx, "call.guest:"+requestIP(c).String(), guestsPerAddress)
		if err == nil && !decision.Allowed {
			return echo.NewHTTPError(http.StatusTooManyRequests, "too many guests from here just now; try again later")
		}
	}
	if err := s.checkLinkPasscode(c, link, request.Passcode); err != nil {
		return err
	}
	call, err := s.rooms.OpenCallIn(ctx, link.RoomID)
	if err != nil {
		return linkError(rooms.ErrCallNotStarted)
	}
	guest, session, err := s.identity.CreateGuest(ctx, call.ID, request.Name, identity.DeviceContext{
		UserAgent: c.Request().UserAgent(), IPAddress: requestIP(c),
	})
	if err != nil {
		if errors.Is(err, identity.ErrInvalidDisplayName) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return err
	}
	_, waiting, err := s.rooms.UseCallLink(ctx, link, guest.ID)
	if err != nil {
		return linkError(err)
	}
	if waiting {
		s.announceLobby(ctx, call)
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.Entry{
			ActorID: &guest.ID, ActorLabel: guest.DisplayName + " (guest)",
			Action: audit.ActionCallGuestAdmitted, TargetKind: "call_link", TargetID: link.ID.String(),
			SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
		})
	}
	setSessionCookie(c, session.Token, s.config.SecureCookies, int(time.Until(session.Record.ExpiresAt).Seconds()))
	return c.JSON(http.StatusOK, map[string]any{
		"user":  s.userResponse(guest),
		"token": session.Token,
		"use":   linkUse{CallID: call.ID.String(), RoomID: call.RoomID.String(), Waiting: waiting},
	})
}
