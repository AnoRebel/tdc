package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/mail"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
digestInterval is how often due summaries are looked for.

A minute, like scheduled messages: a summary chosen for eight o'clock that
arrives at a minute past is on time, and the query runs against an index of
exactly what is due, so a pass that finds nothing costs almost nothing.
*/
const digestInterval = time.Minute

/*
digestRetryAfter is how long a summary waits after the mail server refused it,
or after the person's status said not to interrupt them.

Long enough not to hammer a mail server that is down, short enough that a
summary held by a meeting goes out soon after it ends.
*/
const digestRetryAfter = 15 * time.Minute

/*
digestsOffered reports whether this server can send summaries at all.

Both halves: the operator has not switched them off, and there is a mail server
to send through. A server without mail does not offer them rather than offering
and failing — and somebody who chose one before the mail went away goes back to
ordinary notifications rather than being left with neither.
*/
func (s *Server) digestsOffered(ctx context.Context) bool {
	if s.mail == nil || !s.mail.Configured() || s.settings == nil {
		return false
	}
	return s.settings.Enabled(ctx, settings.FeatureNotificationDigests)
}

/*
digestChosen is whether somebody's notifications are being held for a summary.

Asked by notify on every notification it would push, so it answers no whenever
a summary could not actually reach them.
*/
func (s *Server) digestChosen(ctx context.Context, userID uuid.UUID) bool {
	if !s.digestsOffered(ctx) {
		return false
	}
	chosen, err := s.queries.NotificationDigestChosen(ctx, userID)
	if err != nil {
		// Interrupting when unsure, for the reason notify pushes when unsure.
		return false
	}
	return chosen
}

// batchWindow is the operator's window for gathering a burst into one push.
func (s *Server) batchWindow(ctx context.Context) time.Duration {
	if s.settings == nil {
		return 0
	}
	return time.Duration(s.settings.Int(ctx, settings.NotificationBatchSeconds)) * time.Second
}

// digestResponse is somebody's summary setting, as a client shows it.
type digestResponse struct {
	// Frequency is "off", "daily" or "weekly".
	Frequency string `json:"frequency"`
	// Hour is the hour of the day in TimeZone.
	Hour int `json:"hour"`
	// Weekday is the day of a weekly summary, Sunday being 0.
	Weekday int `json:"weekday"`
	// TimeZone is the IANA zone the hour is in. Empty for somebody who has
	// never chosen, so a client fills in the zone it is running in rather
	// than showing the server's.
	TimeZone string `json:"timeZone"`
	// NextAt is when the next summary is due, absent while off.
	NextAt *time.Time `json:"nextAt,omitempty"`
	// LastSentAt is when one last went out.
	LastSentAt *time.Time `json:"lastSentAt,omitempty"`
	/*
		AddressConfirmed says whether there is anywhere to send one.

		A summary goes only to an address the person has proved, for the
		reason recovery does: an address that was only typed into a form may
		be somebody else's.
	*/
	AddressConfirmed bool `json:"addressConfirmed"`
}

type digestRequest struct {
	Frequency string `json:"frequency"`
	Hour      int    `json:"hour"`
	Weekday   int    `json:"weekday"`
	TimeZone  string `json:"timeZone"`
}

// requireDigests refuses both routes on a server that cannot send summaries,
// with the answer a disabled feature gives.
func (s *Server) requireDigests(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !s.digestsOffered(c.Request().Context()) {
			return echo.NewHTTPError(http.StatusNotFound, "this server does not offer that")
		}
		return next(c)
	}
}

func (s *Server) handleGetDigest(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	row, err := s.queries.GetNotificationDigest(ctx, user.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// Nobody has chosen: off, with the hour a morning summary would use.
		row = dbgen.NotificationDigest{Frequency: string(notify.FrequencyOff), Hour: 8, Weekday: 1}
	}

	return c.JSON(http.StatusOK, s.describeDigest(ctx, user.ID, row))
}

/*
handleSetDigest records how often somebody wants a summary.

The next one is scheduled from the hour and zone they gave, here, so the first
arrives when they expect rather than whenever a pass next happens to look.
*/
func (s *Server) handleSetDigest(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request digestRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	schedule := notify.DigestSchedule{
		Frequency: notify.Frequency(request.Frequency),
		Hour:      request.Hour,
		Weekday:   time.Weekday(request.Weekday),
		Zone:      strings.TrimSpace(request.TimeZone),
	}
	// Turning it off needs no zone. Somebody whose browser reports none can
	// still stop the mail.
	if schedule.Frequency == notify.FrequencyOff && schedule.Zone == "" {
		schedule.Zone = "UTC"
	}
	if err := schedule.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if schedule.Frequency != notify.FrequencyOff && !s.addressConfirmed(ctx, user.ID) {
		return echo.NewHTTPError(http.StatusConflict,
			"a summary is sent only to an email address you have confirmed; confirm one first")
	}

	next, err := schedule.Next(time.Now())
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	row, err := s.queries.SetNotificationDigest(ctx, dbgen.SetNotificationDigestParams{
		UserID:    user.ID,
		Frequency: string(schedule.Frequency),
		Hour:      int16(schedule.Hour),
		Weekday:   int16(schedule.Weekday),
		TimeZone:  schedule.Zone,
		NextDueAt: next,
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, s.describeDigest(ctx, user.ID, row))
}

func (s *Server) describeDigest(ctx context.Context, userID uuid.UUID, row dbgen.NotificationDigest) digestResponse {
	response := digestResponse{
		Frequency:        row.Frequency,
		Hour:             int(row.Hour),
		Weekday:          int(row.Weekday),
		TimeZone:         row.TimeZone,
		LastSentAt:       row.LastSentAt,
		AddressConfirmed: s.addressConfirmed(ctx, userID),
	}
	if row.Frequency != string(notify.FrequencyOff) {
		response.NextAt = row.NextDueAt
	}
	return response
}

// addressConfirmed is whether somebody has an address a summary may go to.
func (s *Server) addressConfirmed(ctx context.Context, userID uuid.UUID) bool {
	user, err := s.queries.GetUserByID(ctx, userID)
	return err == nil && user.EmailVerified && user.Email != nil && *user.Email != ""
}

/*
RunDigests sends summaries as they come due, until ctx ends.

Every node runs it. Each pass claims what it will send in the statement that
finds it, so two nodes sharing a database never mail anybody twice.
*/
func (s *Server) RunDigests(ctx context.Context) {
	ticker := time.NewTicker(digestInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.DeliverDigests(ctx)
		}
	}
}

/*
DeliverDigests makes one pass, returning how many summaries it mailed.

Exported so a test can make a pass when it chooses rather than wait out the
interval.
*/
func (s *Server) DeliverDigests(ctx context.Context) int {
	if !s.digestsOffered(ctx) {
		return 0
	}

	due, err := s.queries.ClaimDueNotificationDigests(ctx, 50)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("cannot claim due summaries", slog.String("error", err.Error()))
		}
		return 0
	}

	sent := 0
	for _, row := range due {
		if s.deliverDigest(ctx, row) {
			sent++
		}
	}
	if sent > 0 {
		s.log.Info("sent notification summaries", "count", sent)
	}
	return sent
}

// deliverDigest sends one person's summary, reporting whether a message went.
func (s *Server) deliverDigest(ctx context.Context, row dbgen.NotificationDigest) bool {
	now := time.Now()
	schedule := notify.DigestSchedule{
		Frequency: notify.Frequency(row.Frequency),
		Hour:      int(row.Hour),
		Weekday:   time.Weekday(row.Weekday),
		Zone:      row.TimeZone,
	}

	next, err := schedule.Next(now)
	if err != nil {
		// Validated when it was stored, so this is a zone database that has
		// changed under it. Stopped rather than retried every minute.
		s.log.Warn("a summary's schedule can no longer be kept",
			"userId", row.UserID, "error", err)
	}

	complete := func(sent bool) {
		if err := s.queries.CompleteNotificationDigest(ctx, dbgen.CompleteNotificationDigestParams{
			UserID:       row.UserID,
			CoveredUntil: now,
			NextDueAt:    next,
			Sent:         sent,
		}); err != nil {
			// Logged loudly: a summary that went but was not recorded will go
			// again when the claim lapses.
			s.log.Error("cannot record a summary as done", "userId", row.UserID, "error", err)
		}
	}
	postpone := func() {
		later := now.Add(digestRetryAfter)
		if err := s.queries.PostponeNotificationDigest(ctx, dbgen.PostponeNotificationDigestParams{
			UserID:    row.UserID,
			NextDueAt: &later,
		}); err != nil {
			s.log.Error("cannot postpone a summary", "userId", row.UserID, "error", err)
		}
	}

	user, err := s.queries.GetUserByID(ctx, row.UserID)
	if err != nil || !user.EmailVerified || user.Email == nil || *user.Email == "" {
		// Nowhere it may go. Moved on rather than retried, so an address
		// confirmed later starts from then rather than from a backlog.
		complete(false)
		return false
	}

	/*
		Their status says not to interrupt them, and a summary arriving is an
		interruption like any other. Held rather than skipped, so what it
		would have said is in the one that goes when the status ends.
	*/
	if s.presence != nil && s.presence.SuppressesNotifications(ctx, row.UserID) {
		postpone()
		return false
	}

	activity, err := s.queries.ListDigestActivity(ctx, dbgen.ListDigestActivityParams{
		UserID: row.UserID,
		After:  row.CoveredUntil,
		Until:  now,
	})
	if err != nil {
		s.log.Error("cannot read what a summary covers", "userId", row.UserID, "error", err)
		postpone()
		return false
	}

	conversations := s.digestConversations(ctx, row.UserID, activity)

	if len(conversations) == 0 {
		// Nothing missed, or nothing they may still read. No mail saying so.
		complete(false)
		return false
	}

	limit := 20
	if s.settings != nil {
		if configured := s.settings.Int(ctx, settings.NotificationDigestMaxConversations); configured > 0 {
			limit = int(configured)
		}
	}
	more := 0
	if len(conversations) > limit {
		more = len(conversations) - limit
		conversations = conversations[:limit]
	}

	digest := notify.ComposeDigest(s.serverDisplayName(ctx), identity.NameOf(user.Username, user.DisplayName), schedule.Frequency,
		conversations, more, s.appLink("/settings"))

	if err := s.mail.Send(ctx, mail.Message{
		To:      *user.Email,
		Subject: digest.Subject,
		Body:    digest.Body,
	}); err != nil {
		if errors.Is(err, mail.ErrNotConfigured) {
			return false
		}
		s.log.Warn("a summary could not be sent", "userId", row.UserID, "error", err)
		postpone()
		return false
	}

	complete(true)
	return true
}

/*
digestConversations keeps only what the person may still read, as of now.

Each notification was raised when its message arrived, under the permissions of
that moment. A summary is sent later, and in between somebody may have left the
channel, been banned from it, had channel.read withdrawn there, or muted it.
Every one of those removes the conversation from the summary — including its
count, which would otherwise say that something is happening in a channel they
can no longer see.
*/
func (s *Server) digestConversations(
	ctx context.Context,
	userID uuid.UUID,
	activity []dbgen.ListDigestActivityRow,
) []notify.DigestConversation {
	var permissions *authz.Permissions
	if s.authz != nil {
		resolved, err := s.authz.PermissionsFor(ctx, userID)
		if err != nil {
			// Unable to tell what they may read is reason to include nothing.
			s.log.Error("cannot resolve permissions for a summary", "userId", userID, "error", err)
			return nil
		}
		permissions = &resolved
	}

	conversations := make([]notify.DigestConversation, 0, len(activity))
	for _, entry := range activity {
		if s.rooms == nil {
			break
		}
		if _, err := s.rooms.RequireMembership(ctx, entry.RoomID, userID); err != nil {
			continue
		}
		if permissions != nil && !permissions.AllowsChannel(authz.PermChannelRead, entry.RoomID).Allowed {
			continue
		}
		if s.moderation != nil && s.moderation.IsBanned(ctx, entry.RoomID, userID) {
			continue
		}
		if s.notify != nil && s.notify.Muted(ctx, userID, entry.RoomID) {
			continue
		}

		conversation := notify.DigestConversation{
			Name:          entry.Name,
			Direct:        entry.Kind == "direct",
			Notifications: entry.Notifications,
			People:        entry.Actors,
			Addressed:     entry.Addressed,
		}
		switch entry.Kind {
		case "channel", "direct", "disposable":
			conversation.Link = s.appLink("/rooms/" + entry.RoomID.String())
		default:
			conversation.Link = s.appLink("/")
		}
		conversations = append(conversations, conversation)
	}
	return conversations
}

/*
appLink is an address in the web application, or empty on a server with no
canonical address — which has no name it could put in a link that would work
from somebody's mail.
*/
func (s *Server) appLink(path string) string {
	base := strings.TrimSuffix(strings.TrimSpace(s.config.CanonicalAddress), "/")
	if base == "" {
		return ""
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return base + path
}

// serverDisplayName is what the server calls itself in a summary: the
// operator's brand if they set one, the configured name otherwise.
func (s *Server) serverDisplayName(ctx context.Context) string {
	if s.settings != nil {
		if name := strings.TrimSpace(s.settings.Get(ctx, settings.BrandName)); name != "" {
			return name
		}
	}
	if s.config.ServerName != "" {
		return s.config.ServerName
	}
	return "TDC"
}
