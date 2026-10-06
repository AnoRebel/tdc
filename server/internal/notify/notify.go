// Package notify decides who is told about a message, and remembers it.
//
// Notifications are stored rather than only pushed, which is what makes them
// work across devices: one read on a phone withdraws what a laptop is showing,
// and a client that was closed finds what it missed on return.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

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
	// ReasonReaction is somebody reacting to something you wrote.
	ReasonReaction Reason = "reaction"
	// ReasonReply is somebody replying in a thread you started.
	ReasonReply Reason = "reply"
	// ReasonJoin is somebody joining a conversation you are in.
	ReasonJoin Reason = "join"
	// ReasonForward is something you wrote being forwarded elsewhere.
	ReasonForward Reason = "forward"
	// ReasonPin is something you wrote being pinned.
	ReasonPin Reason = "pin"
	// ReasonMissedCall is a call somebody was rung into and did not answer.
	ReasonMissedCall Reason = "missed_call"
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
	// Actor is who did the thing. The same as Sender for a message, and
	// different for everything else: a reaction's event is the message that
	// was reacted to, whose sender is usually the person being notified.
	Actor string
	// Detail carries what the reason needs — the emoji, for a reaction. Kept
	// on the notification rather than looked up, so a reaction that has since
	// been taken back still explains the notification it raised.
	Detail string

	/*
		Count is how many unread notifications this one stands for.

		One when a notification is its own entry, more when it summarises a
		conversation: ten messages in a busy channel is one thing worth knowing
		about, not ten, and listing each separately buries every other
		conversation under the noisiest one.
	*/
	Count int64
	// Actors is how many different people are behind that count, which is what
	// separates "five from Amina" from "five people replied".
	Actors int64
}

/*
Watching reports whether somebody has a conversation on screen right now.

An interface rather than a direct dependency on the live connection hub,
because notify has no business knowing what a WebSocket is — and because a
server without live connections should still notify rather than fail.
*/
type Watching interface {
	IsWatching(userID, roomID uuid.UUID) bool
}

/*
Silencing reports whether somebody's status says not to interrupt them.

Statuses are the operator's to define, and each carries a flag saying whether
it suppresses notifications — "in a meeting" does, "away" might not. Reading it
here is what makes that flag mean anything.
*/
type Silencing interface {
	SuppressesNotifications(ctx context.Context, userID uuid.UUID) bool
}

// Service raises and reads notifications.
type Service struct {
	queries *dbgen.Queries
	log     *slog.Logger

	// Both optional: a server with neither still notifies, which is what makes
	// them safe to leave unset in a test or a minimal deployment.
	watching  Watching
	silencing Silencing

	// Optional for the same reason: an operator who has generated no keys
	// still gets notifications, in the application, where they have always
	// been.
	pushing Pushing

	/*
		Both optional, and both about whether a notification reaches a device
		rather than whether it is raised: what is held back here is still
		stored, still counted and still in the list.

		batchWindow is how long after telling somebody about a conversation
		the server holds back from pushing about it again. digestChosen says
		whether somebody has asked for a summary instead of interruptions.
	*/
	batchWindow  func(ctx context.Context) time.Duration
	digestChosen func(ctx context.Context, userID uuid.UUID) bool
}

/*
Pushing tells a device about a notification that has just been raised.

An interface rather than the push service itself, so that deciding who hears
about a message stays independent of how they are reached. This package knows
who and why; it does not know what a VAPID key is.
*/
type Pushing interface {
	// Deliver reaches one person's subscribed devices. Called only for
	// notifications that survived every check here, so it makes no decisions
	// of its own about whether to send.
	Deliver(ctx context.Context, userID uuid.UUID, about Delivery)
}

/*
Delivery is what a raised notification amounts to, for a device.

Carries the message rather than an identifier because the decision about how
much of it to disclose is made where the notification is sent, and a
re-read there would be a second query per recipient for something already in
hand.
*/
type Delivery struct {
	// Sender is who sent it, as a display name.
	Sender string
	// Room is the conversation's name.
	Room string
	// Body is what was said.
	Body string
	// RoomID is where activating the notification should go.
	RoomID uuid.UUID
	// EventID names the message, so opening it can land on the right one.
	EventID string
	// Private marks a conversation whose contents are not already visible to
	// everybody on the server.
	Private bool
}

func NewService(pool *db.Pool, log *slog.Logger) *Service {
	return &Service{queries: dbgen.New(pool), log: log}
}

/*
WithSuppression adds the two reasons a notification is not raised despite being
wanted: the person is looking at the conversation, or their status says not to
interrupt them.

Separate from NewService so that a caller without a live connection hub — a
test, or a server built before this existed — still gets a working service.
*/
func (s *Service) WithSuppression(watching Watching, silencing Silencing) *Service {
	s.watching = watching
	s.silencing = silencing
	return s
}

/*
WithPush adds delivery to devices that are not currently looking at anything.

Separate for the same reason as WithSuppression: a server whose operator has
generated no keys still raises notifications, and a test that does not care
about delivery does not have to construct one.
*/
func (s *Service) WithPush(pushing Pushing) *Service {
	s.pushing = pushing
	return s
}

/*
WithBatching gathers a burst in one conversation into one push.

The window is read on every notification rather than once, because it is the
operator's to change while the server runs. Separate from NewService for the
reason the others are: a caller that does not care still gets a working
service, which pushes every notification it raises.
*/
func (s *Service) WithBatching(window func(ctx context.Context) time.Duration) *Service {
	s.batchWindow = window
	return s
}

/*
WithDigests holds back pushes for somebody who has chosen a summary instead.

The function answers for one person at the moment of asking, and should answer
no whenever a summary could not actually reach them — a server whose mail has
since been switched off must go back to interrupting people rather than
leaving them with neither.
*/
func (s *Service) WithDigests(chosen func(ctx context.Context, userID uuid.UUID) bool) *Service {
	s.digestChosen = chosen
	return s
}

/*
heldBack reports whether a notification that has been raised should stay in the
list rather than reaching somebody's devices.

Two reasons. They asked for a summary instead of interruptions, which is what
choosing one means. Or this is a burst: they were told about this conversation
moments ago and have not read it since, so the conversation's entry in their
list already stands for this message too, and a second push says nothing the
first did not.
*/
func (s *Service) heldBack(ctx context.Context, userID, roomID uuid.UUID, eventID string) bool {
	if s.digestChosen != nil && s.digestChosen(ctx, userID) {
		return true
	}

	if s.batchWindow == nil {
		return false
	}
	window := s.batchWindow(ctx)
	if window <= 0 {
		return false
	}

	recent, err := s.queries.RecentlyNotifiedInRoom(ctx, dbgen.RecentlyNotifiedInRoomParams{
		UserID:        userID,
		RoomID:        roomID,
		EventID:       eventID,
		WindowSeconds: window.Seconds(),
	})
	if err != nil {
		// Pushing when unsure. A second push about a busy channel is a
		// nuisance; a mention that never arrived is a failure.
		s.log.Debug("cannot tell whether a notification is part of a burst", "error", err)
		return false
	}
	return recent
}

/*
suppressed reports whether a notification should be withheld despite being
wanted.

Checked after the preference, not before: the preference decides whether
somebody wants to hear about this at all, and this decides whether now is the
moment. Both have to say yes.

Unread state is untouched either way. Somebody who was looking at a channel has
read what arrived; somebody whose status silenced it has not, and the badge is
what tells them so when they come back.
*/
func (s *Service) suppressed(ctx context.Context, userID, roomID uuid.UUID) bool {
	// Watching it. A notification for a message somebody just watched arrive
	// is the interruption that teaches people to switch notifications off.
	if s.watching != nil && s.watching.IsWatching(userID, roomID) {
		return true
	}

	// Their status says not to interrupt.
	if s.silencing != nil && s.silencing.SuppressesNotifications(ctx, userID) {
		return true
	}

	return false
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
	s.forMessage(ctx, event, body, recipients, directRoom, Delivery{})
}

/*
ForMessageIn is ForMessage for a caller that knows what the conversation is
called.

The names are what a notification says — "Amina in general" — and this package
has only identifiers. Kept as a second entry point rather than a wider one so
that a caller with nothing to add is unchanged.
*/
func (s *Service) ForMessageIn(
	ctx context.Context,
	event dbgen.Event,
	body string,
	recipients []dbgen.ListRoomMembersRow,
	directRoom bool,
	about Delivery,
) {
	s.forMessage(ctx, event, body, recipients, directRoom, about)
}

func (s *Service) forMessage(
	ctx context.Context,
	event dbgen.Event,
	body string,
	recipients []dbgen.ListRoomMembersRow,
	directRoom bool,
	about Delivery,
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

		// Wanted, but not now: they are looking at the conversation, or their
		// status says not to interrupt. Unread state is untouched either way.
		if s.suppressed(ctx, member.ID, event.RoomID) {
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
			// No push either. A device told about a message the application
			// has no record of would open onto nothing.
			continue
		}

		/*
			Reached only by a notification that survived every check above:
			the sender is somebody else, the preference wanted it, and they
			are neither watching the conversation nor silenced.

			Push follows that decision rather than making its own. Somebody
			looking at a channel should not have their phone light up about
			the message they are reading, and that rule lives here.
		*/
		if s.pushing != nil && !s.heldBack(ctx, member.ID, event.RoomID, event.ID) {
			delivery := about
			delivery.Body = body
			delivery.RoomID = event.RoomID
			delivery.EventID = event.ID
			delivery.Private = delivery.Private || directRoom

			s.pushing.Deliver(ctx, member.ID, delivery)
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
		// errors.Is rather than ==: a wrapped ErrNoRows would otherwise be
		// logged as a failure on every read of a preference nobody has set,
		// which is most of them.
		if !errors.Is(err, pgx.ErrNoRows) {
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
		notification := Notification{
			ID:      row.ID,
			RoomID:  row.RoomID,
			EventID: row.EventID,
			Reason:  Reason(row.Reason),
			Sender:  row.SenderAddress,
			Preview: previewOf(row.Content),
			Actor:   row.ActorAddress,
			Detail:  row.Detail,
		}
		// Older notifications predate the actor column, and for a message the
		// sender is the actor anyway.
		if notification.Actor == "" {
			notification.Actor = row.SenderAddress
		}
		notifications = append(notifications, notification)
	}

	return notifications, nil
}

/*
Grouped returns one entry per conversation rather than one per message.

What a notification list should show: a burst in a busy channel is one thing to
know about. Each underlying notification stays individually dismissible, so
reading one message withdraws its own — the grouping is how they are presented,
not how they are stored.
*/
func (s *Service) Grouped(ctx context.Context, userID uuid.UUID) ([]Notification, error) {
	rows, err := s.queries.ListUnreadNotificationsGrouped(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("notify: reading grouped notifications: %w", err)
	}

	notifications := make([]Notification, 0, len(rows))
	for _, row := range rows {
		notification := Notification{
			ID:      row.ID,
			RoomID:  row.RoomID,
			EventID: row.EventID,
			Reason:  Reason(row.Reason),
			Sender:  row.SenderAddress,
			Preview: previewOf(row.Content),
			Actor:   row.ActorAddress,
			Detail:  row.Detail,
			Count:   row.GroupCount,
			Actors:  row.ActorCount,
		}
		if notification.Actor == "" {
			notification.Actor = row.SenderAddress
		}
		notifications = append(notifications, notification)
	}

	// Newest first across conversations. The query orders by room so that
	// DISTINCT ON can pick each room's newest, which is not the order to show.
	sort.Slice(notifications, func(a, b int) bool {
		return rows[a].CreatedAt.After(rows[b].CreatedAt)
	})

	return notifications, nil
}

/*
DismissRoom withdraws everything unread in one conversation.

What dismissing a grouped entry means: the summary stands for several
notifications, and clearing it has to clear what it stood for or the group
reappears the moment the list is redrawn.
*/
func (s *Service) DismissRoom(ctx context.Context, userID, roomID uuid.UUID) error {
	err := s.queries.MarkRoomNotificationsReadForUser(ctx,
		dbgen.MarkRoomNotificationsReadForUserParams{UserID: userID, RoomID: roomID})
	if err != nil {
		return fmt.Errorf("notify: dismissing a conversation's notifications: %w", err)
	}
	return nil
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

/*
About tells one person that something happened to something of theirs.

Separate from ForMessage because the two answer different questions. ForMessage
asks who in a room should hear about a message that arrived, and consults each
person's level and mute. This is about a single person's own thing being acted
on — their message reacted to, their thread replied in, their post pinned — and
the only questions are whether they did it themselves and whether they have
muted that conversation.

Never notifies somebody about their own action. Reacting to your own message,
replying in your own thread and pinning your own post are all ordinary, and a
notification for any of them is noise.

Failures are logged rather than returned: whatever caused this has already
happened, and undoing a reaction because its notification could not be written
would be a much worse outcome than a missed notification.
*/
func (s *Service) About(
	ctx context.Context,
	recipientID uuid.UUID,
	actorID uuid.UUID,
	actorAddress string,
	roomID uuid.UUID,
	eventID string,
	reason Reason,
	detail string,
) {
	if recipientID == actorID {
		return
	}

	// Muting silences these too. Somebody who has muted a channel is asking
	// not to be interrupted by it, and a reaction is an interruption.
	if s.preferenceFor(ctx, recipientID, roomID).Muted {
		return
	}

	if s.suppressed(ctx, recipientID, roomID) {
		return
	}

	err := s.queries.CreateNotification(ctx, dbgen.CreateNotificationParams{
		ID:           uuid.New(),
		UserID:       recipientID,
		RoomID:       roomID,
		EventID:      eventID,
		Reason:       string(reason),
		ActorAddress: actorAddress,
		Detail:       detail,
	})
	if err != nil {
		s.log.Error("cannot raise a notification",
			"userId", recipientID, "eventId", eventID, "reason", reason, "error", err)
	}
}

/*
AboutRoom tells everybody in a conversation that something happened in it.

Used for joining, which is about the room rather than about any one message.
Whoever did it is skipped, and so is anybody who has the conversation muted or
set to hear nothing — a busy channel where people come and go would otherwise
be a stream of notifications about nothing anybody said.
*/
func (s *Service) AboutRoom(
	ctx context.Context,
	members []dbgen.ListRoomMembersRow,
	actorID uuid.UUID,
	actorAddress string,
	roomID uuid.UUID,
	eventID string,
	reason Reason,
) {
	for _, member := range members {
		if member.ID == actorID {
			continue
		}

		// Only for people who asked to hear everything. Somebody on the
		// default of mentions-only does not want to know that a person they
		// have never spoken to joined a channel.
		preference := s.preferenceFor(ctx, member.ID, roomID)
		if preference.Muted || preference.Level != LevelAll {
			continue
		}

		if s.suppressed(ctx, member.ID, roomID) {
			continue
		}

		err := s.queries.CreateNotification(ctx, dbgen.CreateNotificationParams{
			ID:           uuid.New(),
			UserID:       member.ID,
			RoomID:       roomID,
			EventID:      eventID,
			Reason:       string(reason),
			ActorAddress: actorAddress,
		})
		if err != nil {
			s.log.Error("cannot raise a notification",
				"userId", member.ID, "eventId", eventID, "reason", reason, "error", err)
		}
	}
}

// SuppressedForTest exposes the suppression decision to this package's tests.
//
// Exported for testing only. The decision is worth testing on its own: it is
// the difference between a notification somebody wanted and one that arrives
// while they are already reading the message.
func SuppressedForTest(s *Service, ctx context.Context, userID, roomID uuid.UUID) bool {
	return s.suppressed(ctx, userID, roomID)
}

/*
Muted reports whether somebody has silenced a conversation.

Exported for the summary, which asks at send time rather than relying on what
was true when each notification was raised: muting a channel on Tuesday should
keep it out of Wednesday's summary, including what arrived on Monday.
*/
func (s *Service) Muted(ctx context.Context, userID, roomID uuid.UUID) bool {
	return s.preferenceFor(ctx, userID, roomID).Muted
}
