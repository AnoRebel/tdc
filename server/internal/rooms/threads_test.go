package rooms_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// A thread inherits the channel's permissions: no separate membership, and no
// access for someone who cannot read the channel.
func TestThreadInheritsChannelPermissions(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	outsider := makeUser(t, queries, "juma")

	private, err := service.Create(ctx, rooms.NewRoom{
		Slug: "team", Name: "Team", Visibility: "private", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	root, err := service.SendMessage(ctx, private.ID, owner.ID, "what do we think?", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if _, err := service.ReplyInThread(ctx, private.ID, owner.ID, root.ID, "I think yes", ""); err != nil {
		t.Fatalf("ReplyInThread: %v", err)
	}

	// Someone outside the channel cannot read the thread, and is told the room
	// is missing rather than that they are forbidden.
	if _, _, err := service.Thread(ctx, private.ID, outsider.ID, root.ID); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("error = %v, want ErrRoomNotFound", err)
	}
	if _, err := service.ReplyInThread(ctx, private.ID, outsider.ID, root.ID, "let me in", ""); err == nil {
		t.Error("an outsider replied in a private thread")
	}
}

// Replies belong to the thread, not the channel timeline.
func TestRepliesStayOutOfTheMainTimeline(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	room := makeRoom(t, service, author.ID, "general")

	root, err := service.SendMessage(ctx, room.ID, author.ID, "a question", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	for range 3 {
		if _, err := service.ReplyInThread(ctx, room.ID, author.ID, root.ID, "an answer", ""); err != nil {
			t.Fatalf("ReplyInThread: %v", err)
		}
	}

	timeline, err := service.Timeline(ctx, room.ID, author.ID, 100)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	for _, event := range timeline {
		if event.ThreadRootID != nil {
			t.Error("a reply appeared in the main timeline")
		}
	}

	// The replies are in the thread.
	_, replies, err := service.Thread(ctx, room.ID, author.ID, root.ID)
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(replies) != 3 {
		t.Errorf("thread has %d replies, want 3", len(replies))
	}
}

// Replying to a reply joins the same thread rather than nesting.
func TestRepliesDoNotNest(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	room := makeRoom(t, service, author.ID, "general")

	root, err := service.SendMessage(ctx, room.ID, author.ID, "the question", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	first, err := service.ReplyInThread(ctx, room.ID, author.ID, root.ID, "first answer", "")
	if err != nil {
		t.Fatalf("ReplyInThread: %v", err)
	}

	// Replying to the reply must attach to the original root.
	second, err := service.ReplyInThread(ctx, room.ID, author.ID, first.ID, "building on that", "")
	if err != nil {
		t.Fatalf("ReplyInThread: %v", err)
	}
	if second.ThreadRootID == nil || *second.ThreadRootID != root.ID {
		t.Error("a reply to a reply started a nested thread")
	}
}

func TestThreadSummariesCountReplies(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	room := makeRoom(t, service, author.ID, "general")

	busy, err := service.SendMessage(ctx, room.ID, author.ID, "busy thread", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	quiet, err := service.SendMessage(ctx, room.ID, author.ID, "quiet message", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	for range 4 {
		if _, err := service.ReplyInThread(ctx, room.ID, author.ID, busy.ID, "reply", ""); err != nil {
			t.Fatalf("ReplyInThread: %v", err)
		}
	}

	summaries, err := service.ThreadSummaries(ctx, []string{busy.ID, quiet.ID})
	if err != nil {
		t.Fatalf("ThreadSummaries: %v", err)
	}
	if summaries[busy.ID].ReplyCount != 4 {
		t.Errorf("reply count = %d, want 4", summaries[busy.ID].ReplyCount)
	}
	// A message with no replies has no summary, rather than one saying zero.
	if _, present := summaries[quiet.ID]; present {
		t.Error("a message with no replies has a thread summary")
	}
}

// Opening the conversation with someone twice returns the same one.
func TestDirectConversationIsReused(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	first := makeUser(t, queries, "amina")
	second := makeUser(t, queries, "juma")

	opened, err := service.OpenDirect(ctx, []uuid.UUID{first.ID, second.ID})
	if err != nil {
		t.Fatalf("OpenDirect: %v", err)
	}

	// The order the participants are given must not matter: otherwise each
	// person would open a different conversation with the other.
	reopened, err := service.OpenDirect(ctx, []uuid.UUID{second.ID, first.ID})
	if err != nil {
		t.Fatalf("OpenDirect reversed: %v", err)
	}

	if opened.ID != reopened.ID {
		t.Error("opening the same conversation from the other side made a second one")
	}
	if opened.Kind != "direct" || opened.Visibility != "private" {
		t.Errorf("kind = %q, visibility = %q, want a private direct conversation",
			opened.Kind, opened.Visibility)
	}
	// No slug, so it is neither addressable nor discoverable.
	if opened.Slug != nil && *opened.Slug != "" {
		t.Error("a direct conversation has a slug, which would make it discoverable")
	}
}

// A direct conversation must not be readable by anyone outside it.
func TestDirectConversationIsPrivate(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	first := makeUser(t, queries, "amina")
	second := makeUser(t, queries, "juma")
	outsider := makeUser(t, queries, "stranger")

	direct, err := service.OpenDirect(ctx, []uuid.UUID{first.ID, second.ID})
	if err != nil {
		t.Fatalf("OpenDirect: %v", err)
	}
	if _, err := service.SendMessage(ctx, direct.ID, first.ID, "just between us", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Both participants can read it.
	for _, participant := range []uuid.UUID{first.ID, second.ID} {
		if _, err := service.Timeline(ctx, direct.ID, participant, 10); err != nil {
			t.Errorf("a participant cannot read the conversation: %v", err)
		}
	}

	// Nobody else can, and it reads as missing rather than forbidden.
	if _, err := service.Timeline(ctx, direct.ID, outsider.ID, 10); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("error = %v, want ErrRoomNotFound", err)
	}

	// It does not appear in an outsider's discoverable rooms.
	discoverable, err := service.Discoverable(ctx, outsider.ID)
	if err != nil {
		t.Fatalf("Discoverable: %v", err)
	}
	for _, room := range discoverable {
		if room.ID == direct.ID {
			t.Error("a direct conversation appeared in an outsider's room list")
		}
	}
}

// Unread counts exclude a person's own messages: they are not unread to them.
func TestUnreadCountsExcludeOwnMessages(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	reader := makeUser(t, queries, "amina")
	writer := makeUser(t, queries, "juma")
	room := makeRoom(t, service, reader.ID, "general")
	if err := service.Join(ctx, room.ID, writer.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	for range 3 {
		if _, err := service.SendMessage(ctx, room.ID, writer.ID, "from juma", ""); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	}
	if _, err := service.SendMessage(ctx, room.ID, reader.ID, "from amina", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	counts, err := service.UnreadCounts(ctx, reader.ID)
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts[room.ID] != 3 {
		t.Errorf("unread = %d, want 3 (the reader's own message must not count)", counts[room.ID])
	}
}

// Reading catches up, and read state does not go backwards.
func TestReadPositionAdvancesAndDoesNotRegress(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	reader := makeUser(t, queries, "amina")
	writer := makeUser(t, queries, "juma")
	room := makeRoom(t, service, reader.ID, "general")
	if err := service.Join(ctx, room.ID, writer.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	var latest int64
	for range 4 {
		event, err := service.SendMessage(ctx, room.ID, writer.ID, "a message", "")
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		latest = event.Sequence
	}

	if err := service.MarkRead(ctx, room.ID, reader.ID, latest); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	counts, err := service.UnreadCounts(ctx, reader.ID)
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts[room.ID] != 0 {
		t.Errorf("unread = %d after reading everything, want 0", counts[room.ID])
	}

	// An older position from a device that was behind must not resurface
	// messages the person has already read elsewhere.
	if err := service.MarkRead(ctx, room.ID, reader.ID, 1); err != nil {
		t.Fatalf("MarkRead with an older position: %v", err)
	}
	counts, err = service.UnreadCounts(ctx, reader.ID)
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts[room.ID] != 0 {
		t.Errorf("unread = %d after a stale device reported an older position, want 0", counts[room.ID])
	}
}
