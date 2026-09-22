package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// ErrNotAThreadRoot reports a reply to something that cannot start a thread.
var ErrNotAThreadRoot = errors.New("only a message can start a thread")

// ReplyInThread posts a reply under a message.
//
// A thread inherits the permissions and visibility of the channel holding its
// root: there is no separate membership to manage, and a person who cannot
// read the channel cannot read the thread.
func (s *Service) ReplyInThread(
	ctx context.Context,
	roomID, senderID uuid.UUID,
	rootEventID, body, clientToken string,
) (dbgen.Event, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return dbgen.Event{}, ErrEmptyMessage
	}
	if len(trimmed) > MaxMessageLength {
		return dbgen.Event{}, fmt.Errorf("%w: limit is %d characters", ErrMessageTooLong, MaxMessageLength)
	}

	if clientToken != "" {
		existing, err := s.store.FindIdempotentSubmission(ctx, dbgen.FindIdempotentSubmissionParams{
			SenderID: senderID, ClientToken: clientToken,
		})
		if err == nil {
			return s.store.GetEventByID(ctx, existing)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return dbgen.Event{}, fmt.Errorf("check submission: %w", err)
		}
	}

	room, err := s.RequireMembership(ctx, roomID, senderID)
	if err != nil {
		return dbgen.Event{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Event{}, ErrArchived
	}

	root, err := s.store.GetEventByID(ctx, rootEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read thread root: %w", err)
	}
	if root.RoomID != roomID {
		// Reported as missing rather than confirming the message exists in a
		// room the caller may not be able to see.
		return dbgen.Event{}, ErrRoomNotFound
	}
	if root.Type != string(events.TypeMessageSent) {
		return dbgen.Event{}, ErrNotAThreadRoot
	}

	// Replying to a reply joins the same thread rather than nesting: a
	// conversation people can follow has one level, not a tree.
	rootID := root.ID
	if root.ThreadRootID != nil {
		rootID = *root.ThreadRootID
	}

	sender, err := s.store.GetUserByID(ctx, senderID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read sender: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"body":       trimmed,
		"threadRoot": rootID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode reply: %w", err)
	}

	return s.appendThreaded(ctx, room, sender, events.TypeMessageSent, content, clientToken, &rootID)
}

// appendThreaded stores an event that belongs to a thread.
func (s *Service) appendThreaded(
	ctx context.Context,
	room dbgen.Room,
	sender dbgen.User,
	eventType events.Type,
	content json.RawMessage,
	clientToken string,
	threadRootID *string,
) (dbgen.Event, error) {
	sequence, err := s.nextSequence(ctx, room)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("allocate sequence: %w", err)
	}

	nonce, err := events.NewNonce()
	if err != nil {
		return dbgen.Event{}, err
	}

	event := events.Event{
		Room:      s.RoomAddress(room),
		Sender:    s.UserAddress(sender.Username),
		Origin:    s.serverName,
		Type:      eventType,
		CreatedAt: events.Now(),
		Nonce:     nonce,
		Content:   content,
	}
	if err := s.keyring.Sign(&event); err != nil {
		return dbgen.Event{}, fmt.Errorf("sign event: %w", err)
	}

	stored, err := s.store.AppendThreadedEvent(ctx, dbgen.AppendThreadedEventParams{
		ID:            event.ID,
		RoomID:        room.ID,
		Sequence:      sequence,
		SenderID:      &sender.ID,
		SenderAddress: event.Sender,
		RoomAddress:   event.Room,
		OriginServer:  event.Origin,
		Type:          string(event.Type),
		Content:       content,
		CreatedAt:     event.CreatedAt,
		Nonce:         event.Nonce,
		Signature:     event.Signature,
		KeyID:         event.KeyID,
		ThreadRootID:  threadRootID,
	})
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("append event: %w", err)
	}

	if clientToken != "" {
		if err := s.store.RecordIdempotentSubmission(ctx, dbgen.RecordIdempotentSubmissionParams{
			SenderID: sender.ID, ClientToken: clientToken, EventID: stored.ID,
		}); err != nil {
			return dbgen.Event{}, fmt.Errorf("record submission: %w", err)
		}
	}

	return stored, nil
}

// Thread returns a thread's root and its replies.
func (s *Service) Thread(
	ctx context.Context,
	roomID, userID uuid.UUID,
	rootEventID string,
) (dbgen.Event, []dbgen.Event, error) {
	if _, err := s.Visible(ctx, roomID, userID); err != nil {
		return dbgen.Event{}, nil, err
	}

	root, err := s.store.GetEventByID(ctx, rootEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Event{}, nil, ErrRoomNotFound
	}
	if err != nil {
		return dbgen.Event{}, nil, fmt.Errorf("read thread root: %w", err)
	}
	if root.RoomID != roomID {
		return dbgen.Event{}, nil, ErrRoomNotFound
	}

	replies, err := s.store.ListThreadReplies(ctx, &rootEventID)
	if err != nil {
		return dbgen.Event{}, nil, fmt.Errorf("read thread replies: %w", err)
	}

	return root, replies, nil
}

// ThreadSummary says how many replies a message has.
type ThreadSummary struct {
	RootID      string
	ReplyCount  int64
	LastReplyAt int64
}

// ThreadSummaries returns reply counts for a set of messages, so a timeline
// can show "3 replies" without fetching them.
func (s *Service) ThreadSummaries(ctx context.Context, rootIDs []string) (map[string]ThreadSummary, error) {
	if len(rootIDs) == 0 {
		return map[string]ThreadSummary{}, nil
	}

	rows, err := s.store.ListThreadSummaries(ctx, rootIDs)
	if err != nil {
		return nil, fmt.Errorf("read thread summaries: %w", err)
	}

	summaries := make(map[string]ThreadSummary, len(rows))
	for _, row := range rows {
		if row.ThreadRootID == nil {
			continue
		}
		summaries[*row.ThreadRootID] = ThreadSummary{
			RootID:      *row.ThreadRootID,
			ReplyCount:  row.ReplyCount,
			LastReplyAt: row.LastReplyAt,
		}
	}

	return summaries, nil
}

/*
RoomThreadSummaries returns reply counts for every thread in a room.

By room rather than by a list of identifiers, because that is what a timeline
actually needs: one call to draw the whole view, instead of one per message.
Authorisation is the caller's to make — it has the room and the person.
*/
func (s *Service) RoomThreadSummaries(
	ctx context.Context,
	roomID uuid.UUID,
) (map[string]ThreadSummary, error) {
	rows, err := s.store.ListRoomThreadSummaries(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("read room thread summaries: %w", err)
	}

	summaries := make(map[string]ThreadSummary, len(rows))
	for _, row := range rows {
		if row.ThreadRootID == nil {
			continue
		}
		summaries[*row.ThreadRootID] = ThreadSummary{
			RootID:      *row.ThreadRootID,
			ReplyCount:  row.ReplyCount,
			LastReplyAt: row.LastReplyAt,
		}
	}

	return summaries, nil
}

// participantKey identifies a direct conversation by who is in it.
//
// Sorted, so the key does not depend on which person opened the conversation:
// asking for the conversation with someone must return the existing one rather
// than making a second.
func participantKey(userIDs []uuid.UUID) string {
	parts := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		parts = append(parts, id.String())
	}
	sort.Strings(parts)
	return strings.Join(parts, ":")
}

// OpenDirect returns the conversation between these people, creating it if it
// does not exist.
//
// A direct conversation has no slug, so it is neither addressable nor
// discoverable: it is found only by its participants.
func (s *Service) OpenDirect(ctx context.Context, participants []uuid.UUID) (dbgen.Room, error) {
	if len(participants) < 2 {
		return dbgen.Room{}, errors.New("a direct conversation needs at least two people")
	}

	// Deduplicate, so opening a conversation with oneself twice does not make
	// a distinct key.
	unique := make(map[uuid.UUID]bool, len(participants))
	ordered := make([]uuid.UUID, 0, len(participants))
	for _, id := range participants {
		if !unique[id] {
			unique[id] = true
			ordered = append(ordered, id)
		}
	}

	key := participantKey(ordered)

	existing, err := s.store.FindDirectRoom(ctx, &key)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Room{}, fmt.Errorf("find direct conversation: %w", err)
	}

	// The name is for display only; the conversation is identified by its
	// participants.
	names := make([]string, 0, len(ordered))
	for _, id := range ordered {
		user, err := s.store.GetUserByID(ctx, id)
		if err != nil {
			return dbgen.Room{}, fmt.Errorf("read participant: %w", err)
		}
		names = append(names, user.DisplayName)
	}

	room, err := s.store.CreateDirectRoom(ctx, dbgen.CreateDirectRoomParams{
		ID:             uuid.New(),
		Name:           strings.Join(names, ", "),
		HomeServer:     s.serverName,
		CreatedBy:      &ordered[0],
		ParticipantKey: &key,
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("create direct conversation: %w", err)
	}

	for _, id := range ordered {
		if err := s.store.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
			RoomID: room.ID, UserID: id,
		}); err != nil {
			return dbgen.Room{}, fmt.Errorf("add participant: %w", err)
		}
	}

	return room, nil
}

// DirectRooms returns a person's direct conversations.
func (s *Service) DirectRooms(ctx context.Context, userID uuid.UUID) ([]dbgen.Room, error) {
	direct, err := s.store.ListDirectRoomsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list direct conversations: %w", err)
	}
	return direct, nil
}

// MarkRead records how far a person has read.
func (s *Service) MarkRead(ctx context.Context, roomID, userID uuid.UUID, sequence int64) error {
	if err := s.store.SetReadPosition(ctx, dbgen.SetReadPositionParams{
		RoomID:       roomID,
		UserID:       userID,
		ReadSequence: &sequence,
	}); err != nil {
		return fmt.Errorf("record read position: %w", err)
	}
	return nil
}

// UnreadCounts returns how many unread messages each of a person's rooms holds.
func (s *Service) UnreadCounts(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := s.store.ListUnreadCounts(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read unread counts: %w", err)
	}

	counts := make(map[uuid.UUID]int64, len(rows))
	for _, row := range rows {
		counts[row.RoomID] = row.Unread
	}
	return counts, nil
}
