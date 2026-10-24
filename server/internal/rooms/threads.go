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

	"github.com/AnoRebel/tdc/server/internal/actions"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/identity"
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
	offered ...actions.Action,
) (dbgen.Event, error) {
	return s.ReplyInThreadWithFiles(ctx, roomID, senderID, rootEventID, body, clientToken, nil, offered...)
}

// ReplyInThreadWithFiles is ReplyInThread for a reply carrying files, whose
// text may then be empty.
func (s *Service) ReplyInThreadWithFiles(
	ctx context.Context,
	roomID, senderID uuid.UUID,
	rootEventID, body, clientToken string,
	files []FileRef,
	offered ...actions.Action,
) (dbgen.Event, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" && len(files) == 0 {
		return dbgen.Event{}, ErrEmptyMessage
	}
	// A reply is a message, and its buttons are checked as a message's are.
	if err := actions.Check(offered); err != nil {
		return dbgen.Event{}, err
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

	reply := map[string]any{
		"body":       trimmed,
		"threadRoot": rootID,
	}
	if len(offered) > 0 {
		reply["actions"] = offered
	}
	if len(files) > 0 {
		reply["attachments"] = files
	}
	if err := s.markIfProgram(ctx, sender, reply); err != nil {
		return dbgen.Event{}, err
	}
	content, err := json.Marshal(reply)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("encode reply: %w", err)
	}

	return s.appendIn(ctx, room, sender, events.TypeMessageSent, content, clientToken, &rootID)
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

	// Asked for by one of its replies — a search result, a pin, a
	// notification — the thread is the one that reply is in.
	if root.ThreadRootID != nil {
		root, err = s.store.GetEventByID(ctx, *root.ThreadRootID)
		if err != nil {
			return dbgen.Event{}, nil, fmt.Errorf("read thread root: %w", err)
		}
	}

	replies, err := s.store.ListThreadReplies(ctx, &root.ID)
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
	// Unread is replies by others the reader has not read, in a thread they
	// follow. Zero from ThreadSummaries, which has no reader.
	Unread int64
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
	roomID, readerID uuid.UUID,
) (map[string]ThreadSummary, error) {
	rows, err := s.store.ListRoomThreadSummaries(ctx, dbgen.ListRoomThreadSummariesParams{
		RoomID: roomID, UserID: readerID,
	})
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
			Unread:      row.Unread,
		}
	}

	return summaries, nil
}

// ThreadReading is where one person stands with one thread.
type ThreadReading struct {
	ReadSequence int64
	Following    bool
}

// ThreadReadingFor reads it, as nothing read and not following when the
// person has never touched the thread.
func (s *Service) ThreadReadingFor(ctx context.Context, userID uuid.UUID, rootID string) (ThreadReading, error) {
	row, err := s.store.GetThreadRead(ctx, dbgen.GetThreadReadParams{UserID: userID, RootEventID: rootID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ThreadReading{}, nil
	}
	if err != nil {
		return ThreadReading{}, fmt.Errorf("read thread position: %w", err)
	}
	return ThreadReading{ReadSequence: row.ReadSequence, Following: row.Following}, nil
}

// threadRootIn resolves a thread a member names, by its root or a reply.
func (s *Service) threadRootIn(ctx context.Context, roomID, userID uuid.UUID, eventID string) (string, error) {
	if _, err := s.RequireMembership(ctx, roomID, userID); err != nil {
		return "", err
	}
	event, err := s.EventInRoom(ctx, roomID, eventID)
	if err != nil {
		return "", err
	}
	if event.ThreadRootID != nil {
		return *event.ThreadRootID, nil
	}
	return event.ID, nil
}

// MarkThreadRead records how far a member has read a thread. Never backwards.
func (s *Service) MarkThreadRead(ctx context.Context, roomID, userID uuid.UUID, eventID string, sequence int64) error {
	rootID, err := s.threadRootIn(ctx, roomID, userID, eventID)
	if err != nil {
		return err
	}
	if err := s.store.MarkThreadRead(ctx, dbgen.MarkThreadReadParams{
		UserID: userID, RootEventID: rootID, ReadSequence: sequence,
	}); err != nil {
		return fmt.Errorf("record thread position: %w", err)
	}
	return nil
}

// SetThreadFollowing is a member's own decision to follow a thread or not,
// which taking part in it afterwards does not override.
func (s *Service) SetThreadFollowing(ctx context.Context, roomID, userID uuid.UUID, eventID string, following bool) error {
	rootID, err := s.threadRootIn(ctx, roomID, userID, eventID)
	if err != nil {
		return err
	}
	if err := s.store.ChooseThreadFollowing(ctx, dbgen.ChooseThreadFollowingParams{
		UserID: userID, RootEventID: rootID, Following: following,
	}); err != nil {
		return fmt.Errorf("record following: %w", err)
	}
	return nil
}

/*
TookPart records what a reply means for who follows its thread: whoever
replied, and whoever started it, follow it unless they decided otherwise, and
the replier has read up to their own reply.
*/
func (s *Service) TookPart(ctx context.Context, reply dbgen.Event) error {
	if reply.ThreadRootID == nil || reply.SenderID == nil {
		return nil
	}
	rootID := *reply.ThreadRootID

	followers := []uuid.UUID{*reply.SenderID}
	if root, err := s.store.GetEventByID(ctx, rootID); err == nil && root.SenderID != nil {
		followers = append(followers, *root.SenderID)
	}
	for _, id := range followers {
		if err := s.store.FollowThreadByTakingPart(ctx, dbgen.FollowThreadByTakingPartParams{
			UserID: id, RootEventID: rootID,
		}); err != nil {
			return fmt.Errorf("follow thread: %w", err)
		}
	}
	return s.store.MarkThreadRead(ctx, dbgen.MarkThreadReadParams{
		UserID: *reply.SenderID, RootEventID: rootID, ReadSequence: reply.Sequence,
	})
}

// ThreadFollowers is everybody following a thread.
func (s *Service) ThreadFollowers(ctx context.Context, rootID string) ([]uuid.UUID, error) {
	followers, err := s.store.ListThreadFollowers(ctx, rootID)
	if err != nil {
		return nil, fmt.Errorf("read thread followers: %w", err)
	}
	return followers, nil
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
		names = append(names, identity.NameOf(user.Username, user.DisplayName))
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
