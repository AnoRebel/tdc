// Package notify decides who is told about a message, and remembers it.
//
// Notifications are stored rather than only pushed, which is what makes them
// work across devices: one read on a phone withdraws what a laptop is showing,
// and a client that was closed finds what it missed on return.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

// Level is how much a person wants to hear about a conversation.
type Level string

const (
	// LevelAll notifies on every message.
	LevelAll Level = "all"
	// LevelMentions notifies only when named. The default, and the useful
	// middle a plain mute lacks: a busy channel somebody still wants pulling
	// into when addressed.
	LevelMentions Level = "mentions"
	// LevelNone never notifies.
	LevelNone Level = "none"
)

// Reason says why a notification was raised, so a client can group and phrase
// them differently.
type Reason string

const (
	ReasonMention Reason = "mention"
	ReasonDirect  Reason = "direct"
	ReasonThread  Reason = "thread"
)

// Preference is the setting that applies to one conversation.
type Preference struct {
	Level Level
	// Muted silences notifications without touching unread state. Somebody who
	// mutes a channel is asking not to be interrupted, not to be told nothing
	// happened — the badge still counts.
	Muted bool
}

// Notification is one thing somebody has been told about.
type Notification struct {
	ID      uuid.UUID
	RoomID  uuid.UUID
	EventID string
	Reason  Reason
	Sender  string
	Preview string
}

// Service raises and reads notifications.
type Service struct {
	queries *dbgen.Queries
	log     *slog.Logger
}

func NewService(pool *db.Pool, log *slog.Logger) *Service {
	return &Service{queries: dbgen.New(pool), log: log}
}

/*
mentionPattern finds an @name in a message.

Deliberately conservative. A pattern that matched too eagerly would notify
somebody every time an email address or a code sample appeared, which teaches
people to ignore notifications — the failure that makes the whole feature
worthless.
*/
var mentionPattern = regexp.MustCompile(`(?:^|\s)@([a-zA-Z0-9._-]{1,64})\b`)

// MentionsIn returns the usernames a message names.
func MentionsIn(body string) []string {
	matches := mentionPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	// Deduplicated, so naming somebody twice in one message notifies once.
	seen := make(map[string]struct{}, len(matches))
	names := make([]string, 0, len(matches))

	for _, match := range matches {
		name := strings.ToLower(match[1])
		if _, already := seen[name]; already {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	return names
}

/*
ForMessage raises notifications for a message that has just been sent.

Never notifies the sender about their own message, whatever their settings say.
Somebody who has just typed something knows about it, and a notification for it
is pure noise — this is checked here rather than left to a preference, because
no preference should be able to turn it on.

Failures are logged rather than returned. A message that did not send because a
notification could not be written would be a much worse outcome than a
notification that was missed.
*/
func (s *Service) ForMessage(
	ctx context.Context,
	event dbgen.Event,
	body string,
	recipients []dbgen.ListRoomMembersRow,
	directRoom bool,
) {
	mentioned := MentionsIn(body)

	for _, member := range recipients {
		// The sender is never told about their own message.
		if event.SenderID != nil && member.ID == *event.SenderID {
			continue
		}

		reason, wanted := s.reasonFor(
			ctx, member.ID, member.Username, event.RoomID, mentioned, directRoom)
		if !wanted {
			continue
		}

		err := s.queries.CreateNotification(ctx, dbgen.CreateNotificationParams{
			ID:      uuid.New(),
			UserID:  member.ID,
			RoomID:  event.RoomID,
			EventID: event.ID,
			Reason:  string(reason),
		})
		if err != nil {
			s.log.Error("cannot raise a notification",
				"userId", member.ID, "eventId", event.ID, "error", err)
		}
	}
}

/*
reasonFor decides whether one person hears about a message, and why.

Muting is checked before the level, because the two answer different questions:
a level says what is interesting, and muting says not to interrupt regardless.
Somebody who mutes a channel they are set to "all" on wants silence, not an
argument between two settings.
*/
func (s *Service) reasonFor(
	ctx context.Context,
	userID uuid.UUID,
	username string,
	roomID uuid.UUID,
	mentioned []string,
	directRoom bool,
) (Reason, bool) {
	preference := s.preferenceFor(ctx, userID, roomID)

	if preference.Muted {
		return "", false
	}

	// A direct message is a message to one person. Treating it as an ordinary
	// channel post would mean somebody on "mentions" never heard from anyone
	// speaking to them privately.
	if directRoom {
		return ReasonDirect, preference.Level != LevelNone
	}

	if namedIn(username, mentioned) {
		return ReasonMention, preference.Level != LevelNone
	}

	if preference.Level == LevelAll {
		return ReasonThread, true
	}

	return "", false
}

// preferenceFor reads the setting that applies, falling back to the default.
func (s *Service) preferenceFor(ctx context.Context, userID, roomID uuid.UUID) Preference {
	row, err := s.queries.GetNotificationPreference(ctx,
		dbgen.GetNotificationPreferenceParams{UserID: userID, RoomID: roomID})

	if err != nil {
		if err != pgx.ErrNoRows {
			s.log.Debug("cannot read a notification preference", "error", err)
		}
		// Nothing configured: mentions only, which is the setting that annoys
		// the fewest people while still reaching somebody who is addressed.
		return Preference{Level: LevelMentions}
	}

	return Preference{Level: Level(row.Level), Muted: row.Muted}
}

// namedIn reports whether a person is among those a message named.
func namedIn(username string, mentioned []string) bool {
	lowered := strings.ToLower(username)
	for _, name := range mentioned {
		if name == lowered {
			return true
		}
	}
	return false
}

// Unread returns what somebody has not yet seen.
func (s *Service) Unread(ctx context.Context, userID uuid.UUID, limit int32) ([]Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.queries.ListUnreadNotifications(ctx,
		dbgen.ListUnreadNotificationsParams{UserID: userID, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("notify: reading notifications: %w", err)
	}

	notifications := make([]Notification, 0, len(rows))
	for _, row := range rows {
		notifications = append(notifications, Notification{
			ID:      row.ID,
			RoomID:  row.RoomID,
			EventID: row.EventID,
			Reason:  Reason(row.Reason),
			Sender:  row.SenderAddress,
			Preview: previewOf(row.Content),
		})
	}

	return notifications, nil
}

// UnreadCount is the number for a badge.
func (s *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	count, err := s.queries.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("notify: counting notifications: %w", err)
	}
	return count, nil
}

// MarkRead withdraws one notification.
func (s *Service) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	err := s.queries.MarkNotificationRead(ctx,
		dbgen.MarkNotificationReadParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("notify: marking read: %w", err)
	}
	return nil
}

/*
MarkRoomRead withdraws every notification for a conversation.

Called when somebody reads a room, which is what makes notifications behave
across devices: opening a channel on a phone clears the badge on a laptop.
Without it, a notification would have to be dismissed once per device, which
nobody does.
*/
func (s *Service) MarkRoomRead(ctx context.Context, userID, roomID uuid.UUID) error {
	err := s.queries.MarkRoomNotificationsRead(ctx,
		dbgen.MarkRoomNotificationsReadParams{UserID: userID, RoomID: roomID})
	if err != nil {
		return fmt.Errorf("notify: clearing a room's notifications: %w", err)
	}
	return nil
}

// SetPreference records what somebody wants to hear about.
//
// A nil room sets their global default; a room sets that conversation, and
// overrides the default for it.
func (s *Service) SetPreference(
	ctx context.Context,
	userID uuid.UUID,
	room *uuid.UUID,
	preference Preference,
) error {
	if room == nil {
		return s.queries.SetGlobalNotificationPreference(ctx,
			dbgen.SetGlobalNotificationPreferenceParams{
				UserID: userID,
				Level:  string(preference.Level),
				Muted:  preference.Muted,
			})
	}

	return s.queries.SetRoomNotificationPreference(ctx,
		dbgen.SetRoomNotificationPreferenceParams{
			UserID: userID,
			RoomID: room,
			Level:  string(preference.Level),
			Muted:  preference.Muted,
		})
}

// previewOf pulls the readable part out of a message's content.
//
// Short, because a notification is a summary. Sending the whole message would
// also put its full text into whatever displays the notification, which on some
// platforms is a lock screen.
func previewOf(content []byte) string {
	var parsed struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(content, &parsed) != nil {
		return ""
	}

	body := strings.Join(strings.Fields(parsed.Body), " ")
	if len([]rune(body)) > 140 {
		return string([]rune(body)[:140]) + "…"
	}
	return body
}
