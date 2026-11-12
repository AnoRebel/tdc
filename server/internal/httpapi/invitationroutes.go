package httpapi

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/invitations"
)

/*
Rate limits for redeeming an invitation.

Two, because the two secrets are not comparable. A link token is 256 bits:
guessing is not a strategy, and the limit exists only to stop somebody hammering
the endpoint. A code is about fifty bits and short enough to type, which puts
guessing within reach of a patient script — so it is limited far more strictly,
per address, and that is the difference the requirement asks for.
*/
var (
	redeemLinkPerAddress = cache.RateLimit{
		Attempts: 30,
		Window:   time.Hour,
	}
	redeemCodePerAddress = cache.RateLimit{
		Attempts: 5,
		Window:   time.Hour,
		// A lockout as well, because a script that has burned its allowance
		// should not simply resume on the hour.
		LockoutAfter: 15 * time.Minute,
	}
)

// invitationResponse is an invitation, without its secret.
//
// The secret appears once, in the response to issuing it. Returning it from a
// listing would mean anybody who can list invitations can use them, which
// defeats storing them hashed.
type invitationResponse struct {
	ID     string `json:"id"`
	RoomID string `json:"roomId,omitempty"`
	// Token and Code are present only when an invitation has just been issued.
	Token string `json:"token,omitempty"`
	Code  string `json:"code,omitempty"`
	// URL is the link to share, built once so every client shares the same one.
	URL string `json:"url,omitempty"`

	CreatedBy string `json:"createdBy"`
	BoundTo   string `json:"boundTo,omitempty"`
	MaxUses   *int32 `json:"maxUses,omitempty"`
	Uses      int32  `json:"uses"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	RevokedAt string `json:"revokedAt,omitempty"`
	// Usable says whether it would be accepted now, so a client can show a
	// withdrawn invitation as withdrawn rather than working.
	Usable    bool   `json:"usable"`
	CreatedAt string `json:"createdAt"`
}

type issueInvitationRequest struct {
	// BoundTo restricts the invitation to one email address, so forwarding it
	// does not work.
	BoundTo string `json:"boundTo,omitempty"`
	// MaxUses is how many times it may be accepted. Absent is unlimited.
	MaxUses *int32 `json:"maxUses,omitempty"`
	// ExpiresInHours is how long it lasts. Absent never expires.
	ExpiresInHours int `json:"expiresInHours,omitempty"`
	// WithCode issues a short code alongside the link, for reading aloud.
	WithCode bool `json:"withCode,omitempty"`
}

/*
handleIssueChannelInvitation creates a link, and optionally a code, for a
channel.

Takes the channel invite permission rather than mere membership: an invitation
admits somebody to a conversation, and who may extend that is the channel's
decision rather than any member's.
*/
func (s *Server) handleIssueChannelInvitation(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	// A member of the room, so an invitation cannot be issued to a channel the
	// issuer cannot see.
	room, err := s.rooms.RequireMembership(ctx, roomID, user.ID)
	if err != nil {
		return roomError(err)
	}
	// An event's conversation is joined by answering the event, which is
	// where its capacity and invitations are kept; a link around them would
	// be a way in that neither knows about.
	if room.Kind == "event" {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	var request issueInvitationRequest
	_ = c.Bind(&request)

	issued, err := s.invitations.Issue(ctx, user.ID,
		s.rooms.UserAddress(user.Username), invitations.Terms{
			RoomID:    &roomID,
			BoundTo:   request.BoundTo,
			MaxUses:   request.MaxUses,
			ExpiresIn: time.Duration(request.ExpiresInHours) * time.Hour,
			WithCode:  request.WithCode,
		})
	if err != nil {
		return err
	}

	s.recordInvitation(c, user, issued, audit.ActionInvitationIssued)

	return c.JSON(http.StatusCreated, s.invitationResponseOf(issued))
}

// handleIssueServerInvitation creates an invitation to the server itself.
//
// For the registration policy that requires one. Takes the user-invite
// permission, since it admits somebody to the server rather than to a room.
func (s *Server) handleIssueServerInvitation(c *echo.Context) error {
	user, _ := currentUser(c)

	var request issueInvitationRequest
	_ = c.Bind(&request)

	issued, err := s.invitations.Issue(c.Request().Context(), user.ID,
		s.rooms.UserAddress(user.Username), invitations.Terms{
			BoundTo:   request.BoundTo,
			MaxUses:   request.MaxUses,
			ExpiresIn: time.Duration(request.ExpiresInHours) * time.Hour,
			WithCode:  request.WithCode,
		})
	if err != nil {
		return err
	}

	s.recordInvitation(c, user, issued, audit.ActionInvitationIssued)

	return c.JSON(http.StatusCreated, s.invitationResponseOf(issued))
}

type redeemInvitationRequest struct {
	// Token is a link's secret; Code is the short one. One or the other.
	Token string `json:"token,omitempty"`
	Code  string `json:"code,omitempty"`
}

/*
handleRedeemInvitation accepts an invitation.

Every refusal is the same refusal. Expired, exhausted, revoked, bound to
somebody else and never existed all answer identically, because a response that
distinguished them would tell somebody guessing codes when they had guessed
right — which is most of the work of guessing one.
*/
func (s *Server) handleRedeemInvitation(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request redeemInvitationRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	byCode := request.Token == "" && request.Code != ""
	secret := request.Token
	if byCode {
		secret = request.Code
	}

	// A code is short enough to guess at; a link is not. The limits differ by
	// an order of magnitude for that reason.
	if address := requestIP(c); address.IsValid() {
		limit := redeemLinkPerAddress
		bucket := "redeem-link:"
		if byCode {
			limit = redeemCodePerAddress
			bucket = "redeem-code:"
		}

		decision, err := s.limiter.Allow(ctx, bucket+address.String(), limit)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	email := ""
	if user.Email != nil {
		email = *user.Email
	}

	invitation, err := s.invitations.Redeem(ctx, secret, byCode, user.ID, email)

	switch {
	case errors.Is(err, invitations.ErrAlreadyUsed):
		// Not a refusal: they are already in, which is what they wanted, and
		// they held a working invitation to learn it.
		return c.JSON(http.StatusOK, s.invitationResponseOf(invitation))
	case errors.Is(err, invitations.ErrNotUsable):
		return echo.NewHTTPError(http.StatusNotFound, "that invitation cannot be used")
	case err != nil:
		return err
	}

	/*
		Accepting grants membership of that channel and nothing more.

		The invitation names one room; joining it is the whole effect. A
		server invitation names none, and grants only the account the person
		already has by being here.
	*/
	if invitation.RoomID != nil {
		if err := s.rooms.AddMemberByInvitation(ctx, *invitation.RoomID, user.ID); err != nil {
			return roomError(err)
		}
	}

	s.recordInvitation(c, user, invitation, audit.ActionInvitationAccepted)

	return c.JSON(http.StatusOK, s.invitationResponseOf(invitation))
}

// handleRevokeInvitation stops an invitation working.
func (s *Server) handleRevokeInvitation(c *echo.Context) error {
	user, _ := currentUser(c)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such invitation")
	}

	invitation, err := s.invitations.Revoke(c.Request().Context(), id, user.ID)
	if errors.Is(err, invitations.ErrNotUsable) {
		return echo.NewHTTPError(http.StatusNotFound, "no such invitation")
	}
	if err != nil {
		return err
	}

	s.recordInvitation(c, user, invitation, audit.ActionInvitationRevoked)

	return c.NoContent(http.StatusNoContent)
}

// handleListChannelInvitations lists what a channel has outstanding.
func (s *Server) handleListChannelInvitations(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}

	issued, err := s.invitations.ForRoom(ctx, roomID, 0)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, s.invitationResponsesOf(issued))
}

// handleListServerInvitations lists invitations to the server.
func (s *Server) handleListServerInvitations(c *echo.Context) error {
	issued, err := s.invitations.ForServer(c.Request().Context(), 0)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, s.invitationResponsesOf(issued))
}

func (s *Server) invitationResponsesOf(issued []invitations.Invitation) []invitationResponse {
	out := make([]invitationResponse, 0, len(issued))
	for _, invitation := range issued {
		out = append(out, s.invitationResponseOf(invitation))
	}
	return out
}

func (s *Server) invitationResponseOf(invitation invitations.Invitation) invitationResponse {
	response := invitationResponse{
		ID:        invitation.ID.String(),
		Token:     invitation.Token,
		Code:      invitation.Code,
		CreatedBy: invitation.CreatedBy,
		BoundTo:   invitation.BoundTo,
		MaxUses:   invitation.MaxUses,
		Uses:      invitation.Uses,
		Usable:    invitation.Usable(),
		CreatedAt: invitation.CreatedAt.UTC().Format(time.RFC3339),
	}

	if invitation.RoomID != nil {
		response.RoomID = invitation.RoomID.String()
	}
	if invitation.ExpiresAt != nil {
		response.ExpiresAt = invitation.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if invitation.RevokedAt != nil {
		response.RevokedAt = invitation.RevokedAt.UTC().Format(time.RFC3339)
	}

	// The shareable link, built here so every client shares the same one and
	// none has to know how the path is spelled.
	if invitation.Token != "" {
		response.URL = s.canonicalURL("/invite/" + invitation.Token)
	}

	return response
}

/*
recordInvitation writes an audit entry, without the secret.

The identifier is recorded and the token is not. An audit log holding working
invitations would be a second place they leak from — and the log is exactly the
sort of thing exported, copied and read by people who were not meant to be able
to use what is in it.
*/
func (s *Server) recordInvitation(
	c *echo.Context,
	user dbgen.User,
	invitation invitations.Invitation,
	action audit.Action,
) {
	if s.audit == nil {
		return
	}

	detail := map[string]any{}
	if invitation.RoomID != nil {
		detail["room"] = invitation.RoomID.String()
	}
	if invitation.BoundTo != "" {
		detail["boundTo"] = invitation.BoundTo
	}
	if invitation.MaxUses != nil {
		detail["maxUses"] = *invitation.MaxUses
	}
	if invitation.ExpiresAt != nil {
		detail["expiresAt"] = invitation.ExpiresAt.UTC().Format(time.RFC3339)
	}

	if err := s.audit.Record(c.Request().Context(), audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     action,
		TargetKind: "invitation",
		TargetID:   invitation.ID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail:     detail,
	}); err != nil {
		s.log.Error("cannot record an invitation action",
			"action", action, "invitationId", invitation.ID, "error", err)
	}
}
