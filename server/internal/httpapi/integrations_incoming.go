package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/actions"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Incoming webhooks: an address an outside system posts into one channel through.

The address is the credential. It will be pasted into a CI configuration, an
alerting tool, a script in somebody's home directory — and some of those will
leak. So it is built on the assumption that it has: it posts into the one
channel it was made for and does nothing else, it is looked up without any
cache so a revocation is final on the very next call, it is limited in how fast
it can post, and everything it posts says it came from the webhook rather than
from anybody.
*/

// incomingPrefix marks an incoming webhook's credential, the way tdc_ marks an
// API token, so a secret scanner finding one in a public repository knows what
// it has found.
const incomingPrefix = "tdc_hook_"

func init() {
	registerRoutes(func(s *Server) {
		/*
			Unauthenticated in the ordinary sense: the credential is in the
			path, because the tools that call webhooks are configured with an
			address and nothing else. The request log records the route
			pattern, never the path, so the credential does not reach it.
		*/
		s.echo.POST("/api/hooks/:token", s.handleIncomingPost)

		rooms := s.echo.Group("/api/rooms", requireAuthentication)
		rooms.GET("/:id/incoming-webhooks", s.handleListIncoming)
		rooms.POST("/:id/incoming-webhooks", s.handleCreateIncoming)
		rooms.DELETE("/:id/incoming-webhooks/:hookId", s.handleRevokeIncoming)
	})

	documentRoutes(map[string]entry{
		"POST /api/hooks/:token": {
			Summary: "Post into a channel through an incoming webhook", Authentication: "none",
		},
		"GET /api/rooms/:id/incoming-webhooks": {
			Summary: "A channel's incoming webhooks", Authentication: "session or token",
			Permission: string(authz.PermChannelWebhooks),
		},
		"POST /api/rooms/:id/incoming-webhooks": {
			Summary: "Create an incoming webhook", Authentication: "session or token",
			Permission: string(authz.PermChannelWebhooks),
		},
		"DELETE /api/rooms/:id/incoming-webhooks/:hookId": {
			Summary: "Revoke an incoming webhook", Authentication: "session or token",
			Permission: string(authz.PermChannelWebhooks),
		},
	})
}

// incomingPostRequest is what a caller sends. text is accepted as well as body
// because a great many tools already speak that shape.
type incomingPostRequest struct {
	Body    string           `json:"body"`
	Text    string           `json:"text"`
	Actions []actions.Action `json:"actions"`
	// Room and Channel may name the channel, for a caller that insists. Any
	// value other than the one bound at creation is refused.
	Room    string `json:"room"`
	Channel string `json:"channel"`
}

// rateLimitedResponse says when to come back, in the body as well as the
// header, because the caller is a program that may not look at headers.
type rateLimitedResponse struct {
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
}

func (s *Server) handleIncomingPost(c *echo.Context) error {
	ctx := c.Request().Context()

	presented := c.Param("token")
	if !strings.HasPrefix(presented, incomingPrefix) {
		return echo.NewHTTPError(http.StatusNotFound, "no such webhook")
	}

	/*
		Revoked and never-existed are one answer. Somebody holding a dead
		address learns it does not work, not whether it once did.
	*/
	hook, err := s.queries.GetActiveIncomingWebhookByHash(ctx, hashCredential(presented))
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "no such webhook")
	}
	if err != nil {
		return err
	}

	// Answered already when refused: the refusal is written here, with its
	// body, so there is nothing for the rest of this handler to do.
	if refused, err := s.throttleIncoming(c, hook); refused || err != nil {
		return err
	}

	var request incomingPostRequest
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 64<<10))
	if err != nil || json.Unmarshal(raw, &request) != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read as JSON")
	}

	room, err := s.queries.GetRoomByID(ctx, hook.RoomID)
	if err != nil {
		return err
	}

	// Naming any other channel is refused outright, not quietly ignored: a
	// caller that thinks it is posting somewhere else should find out.
	for _, named := range []string{request.Room, request.Channel} {
		if named != "" && !namesRoom(named, room) {
			return echo.NewHTTPError(http.StatusForbidden,
				"this webhook posts only into the channel it was made for")
		}
	}

	body := request.Body
	if body == "" {
		body = request.Text
	}

	// The operator's filter applies exactly as it does to a person: what a
	// program says to a channel is read by the same people.
	filtered, err := s.applyContentFilter(ctx, body)
	if err != nil {
		return err
	}

	via := rooms.Via{Kind: "webhook", ID: hook.ID.String(), Name: hook.Label}
	localPart := "webhook-" + strings.ReplaceAll(hook.ID.String(), "-", "")[:12]
	event, err := s.rooms.PostAs(ctx, hook.RoomID, via, localPart, filtered.Body, request.Actions)
	if err != nil {
		return incomingError(err)
	}

	if err := s.queries.RecordIncomingWebhookPost(ctx, hook.ID); err != nil {
		s.log.Warn("could not count an incoming webhook's post", "webhook", hook.ID, "error", err)
	}
	if filtered.Flagged {
		s.flagForReview(c, hook.RoomID, event.ID, event.SenderAddress, filtered.Matched, filtered.Body)
	}

	// The same as any message after it is stored: searchable, notified by
	// each reader's own preferences, delivered live.
	s.inBackground(ctx, "index", func(ctx context.Context) {
		s.indexForSearch(ctx, event, filtered.Body)
	})
	s.inBackground(ctx, "notify", func(ctx context.Context) {
		s.notifyRecipients(ctx, event, filtered.Body, hook.RoomID)
	})
	s.publishEvent(ctx, event)

	return c.JSON(http.StatusCreated, map[string]string{"id": event.ID, "roomId": hook.RoomID.String()})
}

// namesRoom reports whether a caller's name for a channel is this one's.
func namesRoom(named string, room dbgen.Room) bool {
	named = strings.TrimPrefix(strings.TrimSpace(named), "#")
	if named == room.ID.String() {
		return true
	}
	return room.Slug != nil && strings.EqualFold(named, *room.Slug)
}

/*
throttleIncoming holds one webhook to its rate, reporting whether it refused
the call — in which case the refusal has been written.

Refused with the time to wait, and counted on the webhook so its owner sees
that calls were turned away — a limit that dropped messages silently would look,
from the channel, exactly like the other system having stopped.
*/
func (s *Server) throttleIncoming(c *echo.Context, hook dbgen.IncomingWebhook) (bool, error) {
	if s.limiter == nil || s.settings == nil {
		return false, nil
	}
	ctx := c.Request().Context()
	perMinute := s.settings.Int(ctx, settings.IncomingWebhookPostsPerMinute)
	if perMinute <= 0 {
		return false, nil
	}

	decision, err := s.limiter.Allow(ctx, "incoming:"+hook.ID.String(),
		cache.RateLimit{Attempts: int(perMinute), Window: time.Minute})
	if err != nil || decision.Allowed {
		// A limiter that cannot count lets the call through, as elsewhere.
		return false, nil
	}

	if err := s.queries.RecordIncomingWebhookRefused(ctx, hook.ID); err != nil {
		s.log.Warn("could not count a refused incoming webhook call", "webhook", hook.ID, "error", err)
	}

	seconds := int(decision.RetryAfter.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	c.Response().Header().Set("Retry-After", strconv.Itoa(seconds))
	return true, c.JSON(http.StatusTooManyRequests, rateLimitedResponse{
		Message:           "this webhook is posting faster than the channel allows; wait and send again",
		RetryAfterSeconds: seconds,
	})
}

// incomingView is one incoming webhook as its channel's managers see it.
type incomingView struct {
	ID            string     `json:"id"`
	Label         string     `json:"label"`
	RoomID        string     `json:"roomId"`
	Created       time.Time  `json:"created"`
	CreatedBy     string     `json:"createdBy,omitempty"`
	Revoked       *time.Time `json:"revoked,omitempty"`
	LastUsed      *time.Time `json:"lastUsed,omitempty"`
	Posted        int64      `json:"posted"`
	Refused       int64      `json:"refused"`
	LastRefused   *time.Time `json:"lastRefused,omitempty"`
	MessageSender string     `json:"sender"`
}

// createdIncoming is the one response carrying the credential.
type createdIncoming struct {
	incomingView
	// URL is where to post. It contains the credential, and is shown once.
	URL   string `json:"url"`
	Token string `json:"token"`
}

func (s *Server) viewOfIncoming(hook dbgen.IncomingWebhook) incomingView {
	view := incomingView{
		ID:            hook.ID.String(),
		Label:         hook.Label,
		RoomID:        hook.RoomID.String(),
		Created:       hook.CreatedAt,
		Revoked:       hook.RevokedAt,
		LastUsed:      hook.LastUsedAt,
		Posted:        hook.PostedCount,
		Refused:       hook.RefusedCount,
		LastRefused:   hook.LastRefusedAt,
		MessageSender: s.rooms.UserAddress("webhook-" + strings.ReplaceAll(hook.ID.String(), "-", "")[:12]),
	}
	if hook.CreatedBy != nil {
		view.CreatedBy = hook.CreatedBy.String()
	}
	return view
}

/*
mayManageIncoming answers whether somebody may see and change a channel's
incoming webhooks: they can read the channel, and hold the permission in it.

Missing and forbidden are told apart only once the channel is known to be
visible, so this is not a way to find out which private channels exist.
*/
func (s *Server) mayManageIncoming(c *echo.Context) (uuid.UUID, error) {
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil(), echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if _, err := s.rooms.Visible(c.Request().Context(), roomID, user.ID); err != nil {
		return uuid.Nil(), roomError(err)
	}
	decision := currentPermissions(c).AllowsChannel(authz.PermChannelWebhooks, roomID)
	if !decision.Allowed {
		return uuid.Nil(), echo.NewHTTPError(http.StatusForbidden, decision.Error())
	}
	return roomID, nil
}

func (s *Server) handleListIncoming(c *echo.Context) error {
	roomID, err := s.mayManageIncoming(c)
	if err != nil {
		return err
	}
	hooks, err := s.queries.ListIncomingWebhooksForRoom(c.Request().Context(), roomID)
	if err != nil {
		return err
	}
	views := make([]incomingView, 0, len(hooks))
	for _, hook := range hooks {
		views = append(views, s.viewOfIncoming(hook))
	}
	return c.JSON(http.StatusOK, views)
}

type createIncomingRequest struct {
	Label string `json:"label"`
}

func (s *Server) handleCreateIncoming(c *echo.Context) error {
	user, _ := currentUser(c)
	roomID, err := s.mayManageIncoming(c)
	if err != nil {
		return err
	}

	var request createIncomingRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	label := strings.TrimSpace(request.Label)
	if label == "" || len([]rune(label)) > 80 {
		return echo.NewHTTPError(http.StatusBadRequest, "a webhook needs a label of at most 80 characters, saying what posts through it")
	}

	credential, err := newIncomingCredential()
	if err != nil {
		return err
	}

	hook, err := s.queries.CreateIncomingWebhook(c.Request().Context(), dbgen.CreateIncomingWebhookParams{
		ID:        uuid.New(),
		RoomID:    roomID,
		Label:     label,
		TokenHash: hashCredential(credential),
		CreatedBy: &user.ID,
	})
	if err != nil {
		return err
	}

	s.recordModeration(c, user, audit.ActionIncomingWebhookCreated, "incoming_webhook", hook.ID.String(),
		map[string]any{"room": roomID.String(), "label": label})

	return c.JSON(http.StatusCreated, createdIncoming{
		incomingView: s.viewOfIncoming(hook),
		URL:          s.canonicalURL("/api/hooks/" + credential),
		Token:        credential,
	})
}

func (s *Server) handleRevokeIncoming(c *echo.Context) error {
	user, _ := currentUser(c)
	roomID, err := s.mayManageIncoming(c)
	if err != nil {
		return err
	}
	hookID, err := uuid.Parse(c.Param("hookId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such webhook")
	}

	revoked, err := s.queries.RevokeIncomingWebhook(c.Request().Context(), dbgen.RevokeIncomingWebhookParams{
		ID:        hookID,
		RoomID:    roomID,
		RevokedBy: &user.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "no such webhook")
	}
	if err != nil {
		return err
	}

	s.recordModeration(c, user, audit.ActionIncomingWebhookRevoked, "incoming_webhook", revoked.ID.String(),
		map[string]any{"room": roomID.String(), "label": revoked.Label})
	return c.NoContent(http.StatusNoContent)
}

func newIncomingCredential() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return incomingPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashCredential is the stored form: SHA-256, as for sessions and tokens, since
// the value already carries full entropy and is checked on every call.
func hashCredential(credential string) []byte {
	sum := sha256.Sum256([]byte(credential))
	return sum[:]
}

func incomingError(err error) error {
	switch {
	case errors.Is(err, rooms.ErrOnlyLinks):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	default:
		return roomError(err)
	}
}
