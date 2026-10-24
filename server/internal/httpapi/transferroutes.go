package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/presence"
	"github.com/AnoRebel/tdc/server/internal/realtime"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Direct transfers: one person's device sending a file straight to another's.

The server records the offer and relays the two devices' negotiation over the
live connection they already hold, authorised on every message; the file goes
over a WebRTC data channel between them and never touches this server, unless
the operator's relay carries it. The offer is a row so whichever node either
person is connected to can check a signal belongs to it.
*/
func init() {
	registerRoutes(func(s *Server) {
		if s.rooms == nil {
			return
		}
		gate := s.requireFeature(settings.FeatureDirectTransfers)
		s.echo.POST("/api/rooms/:id/transfers", s.handleOfferTransfer, requireAuthentication, gate)
		s.echo.GET("/api/transfers/ice", s.handleTransferICE, requireAuthentication, gate)
		transfers := s.echo.Group("/api/transfers/:id", requireAuthentication, gate)
		transfers.GET("", s.handleGetTransfer)
		transfers.POST("/accept", s.handleMoveTransfer("accepted", true, false, "offered"))
		transfers.POST("/decline", s.handleMoveTransfer("declined", true, false, "offered"))
		transfers.POST("/cancel", s.handleMoveTransfer("cancelled", true, true, "offered", "accepted"))
		transfers.POST("/finish", s.handleMoveTransfer("finished", true, false, "accepted"))
		transfers.POST("/fail", s.handleMoveTransfer("failed", true, true, "accepted"))
	})

	documentRoutes(map[string]entry{
		"POST /api/rooms/:id/transfers":  {Summary: "Offer a file straight to another person's device", Authentication: "session or token"},
		"GET /api/transfers/ice":         {Summary: "The servers a direct transfer connects through", Authentication: "session or token"},
		"GET /api/transfers/:id":         {Summary: "A direct transfer you are sending or receiving", Authentication: "session or token"},
		"POST /api/transfers/:id/accept": {Summary: "Accept a file offered to you directly", Authentication: "session or token"},
		"POST /api/transfers/:id/decline": {
			Summary: "Decline a file offered to you directly", Authentication: "session or token",
		},
		"POST /api/transfers/:id/cancel": {Summary: "Stop a direct transfer, as either person", Authentication: "session or token"},
		"POST /api/transfers/:id/finish": {Summary: "Say a direct transfer arrived whole", Authentication: "session or token"},
		"POST /api/transfers/:id/fail":   {Summary: "Say a direct transfer could not be completed", Authentication: "session or token"},
	})
}

const (
	// offerLifetime is how long an offer waits for an answer.
	offerLifetime = 10 * time.Minute
	// transferLifetime is how long an accepted transfer has to finish: long
	// enough for several gigabytes over a slow link.
	transferLifetime = 6 * time.Hour
)

type offerTransferRequest struct {
	// Recipient is who it is for, by account.
	Recipient   string `json:"recipient"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
}

type transferView struct {
	ID          string `json:"id"`
	RoomID      string `json:"roomId"`
	SenderID    string `json:"senderId"`
	Sender      string `json:"sender"`
	RecipientID string `json:"recipientId"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType,omitempty"`
	State       string `json:"state"`
	ExpiresAt   string `json:"expiresAt"`
	// RecipientOnline says, to the sender, whether anybody is there to accept.
	RecipientOnline bool `json:"recipientOnline"`
}

func (s *Server) transferViewOf(ctx context.Context, row dbgen.DirectTransfer) transferView {
	view := transferView{
		ID: row.ID.String(), RoomID: row.RoomID.String(),
		SenderID: row.SenderID.String(), RecipientID: row.RecipientID.String(),
		Filename: row.Filename, Size: row.SizeBytes, ContentType: row.ContentType,
		State: row.State, ExpiresAt: row.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if sender, err := s.identity.UserByID(ctx, row.SenderID); err == nil {
		view.Sender = shownUsername(sender)
	}
	// Appearing offline is honoured: somebody who chose it is reported
	// offline to the sender as to everybody else.
	if s.presence != nil {
		if seen, err := s.presence.Of(ctx, row.RecipientID); err == nil {
			view.RecipientOnline = seen.State != presence.StateOffline
		}
	}
	return view
}

/*
handleOfferTransfer offers a file to one other member of a conversation.

Both must be members, and neither may have blocked the other: an offer is a
way to put something in front of somebody, and a block says not to.
*/
func (s *Server) handleOfferTransfer(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	var request offerTransferRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	recipient, err := uuid.Parse(request.Recipient)
	if err != nil || recipient == user.ID {
		return echo.NewHTTPError(http.StatusBadRequest, "name one other person to send it to")
	}
	if request.Size < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a size cannot be negative")
	}

	if _, err := s.rooms.RequireMembership(ctx, roomID, user.ID); err != nil {
		return roomError(err)
	}
	// Not in the conversation, or blocking: reported alike, so an offer is
	// not a way to learn who has blocked whom.
	unreachable := echo.NewHTTPError(http.StatusNotFound, "that person is not in this conversation")
	if _, err := s.rooms.RequireMembership(ctx, roomID, recipient); err != nil {
		return unreachable
	}
	if s.moderation != nil &&
		(s.moderation.HasBlocked(ctx, recipient, user.ID) || s.moderation.HasBlocked(ctx, user.ID, recipient)) {
		return unreachable
	}

	row, err := s.queries.CreateDirectTransfer(ctx, dbgen.CreateDirectTransferParams{
		ID: uuid.New(), RoomID: roomID, SenderID: user.ID, RecipientID: recipient,
		Filename: shortName(request.Filename), SizeBytes: request.Size,
		ContentType: shortName(request.ContentType), ExpiresAt: time.Now().Add(offerLifetime),
	})
	if err != nil {
		return err
	}

	view := s.transferViewOf(ctx, row)
	s.tellTransfer(ctx, row, realtime.FrameTransferOffered, view, recipient)
	return c.JSON(http.StatusCreated, view)
}

// shortName bounds a claim shown to somebody else.
func shortName(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len([]rune(value)) > 200 {
		value = string([]rune(value)[:200])
	}
	return value
}

// transferFor reads a transfer one of its two people may see.
func (s *Server) transferFor(c *echo.Context) (dbgen.DirectTransfer, error) {
	user, _ := currentUser(c)
	missing := echo.NewHTTPError(http.StatusNotFound, "no such transfer")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return dbgen.DirectTransfer{}, missing
	}
	row, err := s.queries.GetDirectTransfer(c.Request().Context(), id)
	if err != nil || (row.SenderID != user.ID && row.RecipientID != user.ID) {
		return dbgen.DirectTransfer{}, missing
	}
	return row, nil
}

func (s *Server) handleGetTransfer(c *echo.Context) error {
	row, err := s.transferFor(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s.transferViewOf(c.Request().Context(), row))
}

/*
handleMoveTransfer changes a transfer's state, for whichever of its two people
the change belongs to — only the recipient accepts, declines or says it
arrived; either may stop it — and tells both.
*/
func (s *Server) handleMoveTransfer(to string, recipient, sender bool, from ...string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		user, _ := currentUser(c)
		ctx := c.Request().Context()
		row, err := s.transferFor(c)
		if err != nil {
			return err
		}
		allowed := (recipient && row.RecipientID == user.ID) || (sender && row.SenderID == user.ID)
		if !allowed {
			return echo.NewHTTPError(http.StatusForbidden, "that is the other person's to decide")
		}

		params := dbgen.MoveDirectTransferParams{ToState: to, ID: row.ID, FromStates: from}
		if to == "accepted" {
			lasts := time.Now().Add(transferLifetime)
			params.ExpiresAt = &lasts
		}
		moved, err := s.queries.MoveDirectTransfer(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusConflict, "that transfer has already ended, or lapsed")
		}
		if err != nil {
			return err
		}

		view := s.transferViewOf(ctx, moved)
		s.tellTransfer(ctx, moved, realtime.FrameTransferUpdated, view, moved.SenderID, moved.RecipientID)
		return c.JSON(http.StatusOK, view)
	}
}

// tellTransfer sends a frame about a transfer to the people named, on this
// node and the others.
func (s *Server) tellTransfer(ctx context.Context, row dbgen.DirectTransfer, kind string, payload any, people ...uuid.UUID) {
	if s.hub == nil {
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	frame := realtime.Frame{Type: kind, RoomID: row.RoomID.String(), Payload: encoded}
	for _, person := range people {
		here := s.hub.SendToUser(person, frame)
		s.whisperElsewhere(ctx, person, frame, here)
	}
}

/*
SignalTransfer passes one piece of a direct transfer's negotiation to the other
of its two people.

Only while the recipient has accepted and the transfer has not lapsed, only
between its two people, and only while both are still in the conversation —
checked on every message, as a call's are, so leaving or being removed ends
somebody's part at once.
*/
func (s *Server) SignalTransfer(ctx context.Context, from, transferID uuid.UUID, signal json.RawMessage) error {
	row, err := s.queries.GetDirectTransfer(ctx, transferID)
	if err != nil {
		return err
	}
	if row.State != "accepted" || !row.ExpiresAt.After(time.Now()) {
		return errors.New("that transfer is not under way")
	}
	var to uuid.UUID
	switch from {
	case row.SenderID:
		to = row.RecipientID
	case row.RecipientID:
		to = row.SenderID
	default:
		return errors.New("not a party to that transfer")
	}
	for _, person := range []uuid.UUID{from, to} {
		if _, err := s.rooms.RequireMembership(ctx, row.RoomID, person); err != nil {
			return err
		}
	}

	s.tellTransfer(ctx, row, realtime.FrameTransferSignal, map[string]any{
		"transfer": row.ID.String(), "from": from.String(), "signal": signal,
	}, to)
	return nil
}

/*
handleTransferICE gives a client the servers a direct transfer connects
through: the same as a call's, without the relay when the operator keeps it for
calls — a relayed transfer passes the whole file through it twice.
*/
func (s *Server) handleTransferICE(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	servers := []iceServer{}
	if stun := settings.ListOf(s.settings.Get(ctx, settings.CallsSTUNURLs)); len(stun) > 0 {
		servers = append(servers, iceServer{URLs: stun})
	}
	relay := false
	turn := settings.ListOf(s.settings.Get(ctx, settings.CallsTURNURLs))
	if len(turn) > 0 && s.liveConfig().turnSecret != "" && s.settings.Get(ctx, settings.FilesDirectTransferRelay) == "true" {
		username, credential := turnCredential(s.liveConfig().turnSecret, user.ID, time.Now().Add(turnCredentialLifetime))
		servers = append(servers, iceServer{URLs: turn, Username: username, Credential: credential})
		relay = true
	}
	return c.JSON(http.StatusOK, map[string]any{"iceServers": servers, "relay": relay})
}

// sweepTransfers ends offers and transfers that lapsed, telling both people,
// and forgets them an hour after they end.
func (s *Server) sweepTransfers(ctx context.Context) {
	expired, err := s.queries.ExpireDirectTransfers(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("expiring direct transfers failed", "error", err)
		}
		return
	}
	for _, row := range expired {
		s.tellTransfer(ctx, row, realtime.FrameTransferUpdated, s.transferViewOf(ctx, row), row.SenderID, row.RecipientID)
	}
	forgotten := time.Now().Add(-time.Hour)
	if err := s.queries.DeleteEndedDirectTransfers(ctx, &forgotten); err != nil && ctx.Err() == nil {
		s.log.Error("forgetting ended direct transfers failed", "error", err)
	}
}
