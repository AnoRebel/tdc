package rooms_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

// Archiving preserves history and refuses new messages, and is reversible.
func TestArchivePreservesHistoryAndRefusesPosts(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	room := makeRoom(t, service, owner.ID, "general")

	if _, err := service.SendMessage(ctx, room.ID, owner.ID, "before archiving", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if err := service.Archive(ctx, room.ID, owner.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if _, err := service.SendMessage(ctx, room.ID, owner.ID, "after archiving", ""); !errors.Is(err, rooms.ErrArchived) {
		t.Errorf("posting to an archived room: error = %v, want ErrArchived", err)
	}

	// History must survive.
	timeline, err := service.Timeline(ctx, room.ID, owner.ID, 100)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	found := false
	for _, event := range timeline {
		if event.Type == string(events.TypeMessageSent) && bodyOf(t, event) == "before archiving" {
			found = true
		}
	}
	if !found {
		t.Error("archiving lost the room's history")
	}

	// And the decision must be reversible.
	if err := service.Restore(ctx, room.ID, owner.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := service.SendMessage(ctx, room.ID, owner.ID, "after restoring", ""); err != nil {
		t.Errorf("posting after restoring: %v", err)
	}
}

// A private room must not be distinguishable from one that does not exist.
func TestPrivateRoomIsNotDiscoverable(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	outsider := makeUser(t, queries, "juma")

	private, err := service.Create(ctx, rooms.NewRoom{
		Slug: "secret", Name: "Secret", Visibility: "private", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Fetching by identifier reports it missing, not forbidden.
	if _, err := service.Visible(ctx, private.ID, outsider.ID); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("error = %v, want ErrRoomNotFound", err)
	}

	// It must not appear in what the outsider can discover.
	discoverable, err := service.Discoverable(ctx, outsider.ID)
	if err != nil {
		t.Fatalf("Discoverable: %v", err)
	}
	for _, room := range discoverable {
		if room.ID == private.ID {
			t.Error("a private room appeared in an outsider's room list")
		}
	}

	// Joining must not confirm it either.
	if err := service.Join(ctx, private.ID, outsider.ID); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("joining a private room: error = %v, want ErrRoomNotFound", err)
	}

	// The owner still sees it.
	if _, err := service.Visible(ctx, private.ID, owner.ID); err != nil {
		t.Errorf("the owner cannot see their own private room: %v", err)
	}
}

// An invitation is what gets someone into a private room.
func TestInvitationGrantsAccessToAPrivateRoom(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	guest := makeUser(t, queries, "juma")

	private, err := service.Create(ctx, rooms.NewRoom{
		Slug: "secret", Name: "Secret", Visibility: "private", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := service.Invite(ctx, private.ID, owner.ID, guest.ID); err != nil {
		t.Fatalf("Invite: %v", err)
	}

	if _, err := service.Visible(ctx, private.ID, guest.ID); err != nil {
		t.Errorf("an invited guest cannot see the room: %v", err)
	}
	if _, err := service.SendMessage(ctx, private.ID, guest.ID, "thanks for the invitation", ""); err != nil {
		t.Errorf("an invited guest cannot post: %v", err)
	}
}

// Public rooms are joinable without an invitation.
func TestPublicRoomIsJoinable(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	newcomer := makeUser(t, queries, "juma")
	room := makeRoom(t, service, owner.ID, "general")

	// A public room is visible before joining, but posting requires membership.
	if _, err := service.Visible(ctx, room.ID, newcomer.ID); err != nil {
		t.Errorf("a public room is not visible to a non-member: %v", err)
	}
	if _, err := service.SendMessage(ctx, room.ID, newcomer.ID, "hello", ""); !errors.Is(err, rooms.ErrNotAMember) {
		t.Errorf("posting without joining: error = %v, want ErrNotAMember", err)
	}

	if err := service.Join(ctx, room.ID, newcomer.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := service.SendMessage(ctx, room.ID, newcomer.ID, "hello", ""); err != nil {
		t.Errorf("posting after joining: %v", err)
	}
}

// Removal takes effect immediately.
func TestRemovedMemberLosesAccess(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	member := makeUser(t, queries, "juma")

	private, err := service.Create(ctx, rooms.NewRoom{
		Slug: "team", Name: "Team", Visibility: "private", CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := service.Invite(ctx, private.ID, owner.ID, member.ID); err != nil {
		t.Fatalf("Invite: %v", err)
	}

	if err := service.Remove(ctx, private.ID, owner.ID, member.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := service.Visible(ctx, private.ID, member.ID); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("a removed member can still see the private room: %v", err)
	}
	if _, err := service.SendMessage(ctx, private.ID, member.ID, "still here?", ""); err == nil {
		t.Error("a removed member can still post")
	}
}

// Membership changes are recorded, so the timeline explains who is present.
func TestMembershipChangesAreRecorded(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	guest := makeUser(t, queries, "juma")
	room := makeRoom(t, service, owner.ID, "general")

	if err := service.Join(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err := service.Leave(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	timeline, err := service.Timeline(ctx, room.ID, owner.ID, 100)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}

	seen := map[string]bool{}
	for _, event := range timeline {
		seen[event.Type] = true
	}
	for _, wanted := range []events.Type{events.TypeMemberJoined, events.TypeMemberLeft} {
		if !seen[string(wanted)] {
			t.Errorf("%s was not recorded", wanted)
		}
	}
}

// An edit is a new event, so the original stays in place and verifiable.
func TestEditRecordsANewEvent(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	room := makeRoom(t, service, author.ID, "general")

	original, err := service.SendMessage(ctx, room.ID, author.ID, "frist post", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	edit, err := service.EditMessage(ctx, room.ID, author.ID, original.ID, "first post")
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}

	if edit.ID == original.ID {
		t.Error("the edit replaced the original event rather than adding one")
	}
	if edit.Type != string(events.TypeMessageEdited) {
		t.Errorf("edit type = %q, want %q", edit.Type, events.TypeMessageEdited)
	}

	// The edit names what it replaces, so a client can apply it.
	var content struct {
		Body     string `json:"body"`
		Replaces string `json:"replaces"`
	}
	if err := json.Unmarshal(edit.Content, &content); err != nil {
		t.Fatalf("decode edit: %v", err)
	}
	if content.Replaces != original.ID {
		t.Errorf("replaces = %q, want the original event", content.Replaces)
	}
	if content.Body != "first post" {
		t.Errorf("body = %q, want the corrected text", content.Body)
	}

	// The original is still there, unchanged.
	stored, err := queries.GetEventByID(ctx, original.ID)
	if err != nil {
		t.Fatalf("GetEventByID: %v", err)
	}
	if bodyOf(t, stored) != "frist post" {
		t.Error("editing altered the original event")
	}
}

// Only the author may edit their message.
func TestOnlyTheAuthorMayEdit(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	other := makeUser(t, queries, "juma")
	room := makeRoom(t, service, author.ID, "general")
	if err := service.Join(ctx, room.ID, other.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	message, err := service.SendMessage(ctx, room.ID, author.ID, "mine", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if _, err := service.EditMessage(ctx, room.ID, other.ID, message.ID, "not yours"); !errors.Is(err, rooms.ErrNotTheAuthor) {
		t.Errorf("error = %v, want ErrNotTheAuthor", err)
	}
}

// Deletion leaves evidence: the conversation must not appear never to have
// contained the message.
func TestDeletionLeavesEvidence(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	room := makeRoom(t, service, author.ID, "general")

	message, err := service.SendMessage(ctx, room.ID, author.ID, "something regrettable", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if _, err := service.DeleteMessage(ctx, room.ID, author.ID, message.ID, false); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	stored, err := queries.GetEventByID(ctx, message.ID)
	if err != nil {
		t.Fatalf("the deleted event is gone from the log entirely: %v", err)
	}
	if stored.RedactedAt == nil {
		t.Error("the event was not marked redacted")
	}
	if bodyOf(t, stored) == "something regrettable" {
		t.Error("the content survived deletion")
	}

	// The surrounding order is unaffected, so replay still works.
	timeline, err := service.Timeline(ctx, room.ID, author.ID, 100)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	for i := 1; i < len(timeline); i++ {
		if timeline[i].Sequence <= timeline[i-1].Sequence {
			t.Fatal("deletion disturbed the room's ordering")
		}
	}
}

// A moderator may delete someone else's message; an ordinary member may not.
func TestModeratorMayDeleteAnothersMessage(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	moderator := makeUser(t, queries, "juma")
	room := makeRoom(t, service, author.ID, "general")
	if err := service.Join(ctx, room.ID, moderator.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	message, err := service.SendMessage(ctx, room.ID, author.ID, "off topic", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Without the moderator flag the service refuses.
	if _, err := service.DeleteMessage(ctx, room.ID, moderator.ID, message.ID, false); !errors.Is(err, rooms.ErrNotTheAuthor) {
		t.Errorf("error = %v, want ErrNotTheAuthor", err)
	}

	// With it, the deletion proceeds and is marked as a moderator's act.
	deletion, err := service.DeleteMessage(ctx, room.ID, moderator.ID, message.ID, true)
	if err != nil {
		t.Fatalf("DeleteMessage as moderator: %v", err)
	}

	var content struct {
		ByModerator bool `json:"byModerator"`
	}
	if err := json.Unmarshal(deletion.Content, &content); err != nil {
		t.Fatalf("decode deletion: %v", err)
	}
	if !content.ByModerator {
		t.Error("a moderator's deletion was not recorded as one")
	}
}

// A message in another room must not be reachable by identifier.
func TestMessageFromAnotherRoomIsNotReachable(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	first := makeRoom(t, service, author.ID, "first")
	second := makeRoom(t, service, author.ID, "second")

	message, err := service.SendMessage(ctx, first.ID, author.ID, "in the first room", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if _, err := service.EditMessage(ctx, second.ID, author.ID, message.ID, "moved"); !errors.Is(err, rooms.ErrRoomNotFound) {
		t.Errorf("error = %v, want ErrRoomNotFound", err)
	}
}

// Replay must return exactly what a client missed.
func TestEventsSinceReturnsExactlyTheGap(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	author := makeUser(t, queries, "amina")
	room := makeRoom(t, service, author.ID, "general")

	var checkpoint int64
	for i := range 6 {
		event, err := service.SendMessage(ctx, room.ID, author.ID, "message", "")
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		if i == 2 {
			checkpoint = event.Sequence
		}
	}

	missed, err := service.EventsSince(ctx, room.ID, author.ID, checkpoint)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}

	// Three messages were sent after the checkpoint.
	if len(missed) != 3 {
		t.Fatalf("replayed %d events, want 3", len(missed))
	}
	for _, event := range missed {
		if event.Sequence <= checkpoint {
			t.Errorf("replay included sequence %d, at or before the checkpoint %d",
				event.Sequence, checkpoint)
		}
	}
	// In order, with no duplicates.
	for i := 1; i < len(missed); i++ {
		if missed[i].Sequence <= missed[i-1].Sequence {
			t.Error("replayed events are not in ascending order")
		}
	}
}

func TestInvalidSlugsAreRefused(t *testing.T) {
	service, queries, _ := newTestService(t)
	creator := makeUser(t, queries, "amina")

	for _, slug := range []string{"a", "-leading", "trailing-", "has spaces", "emoji🎉"} {
		_, err := service.Create(context.Background(), rooms.NewRoom{
			Slug: slug, Name: slug, CreatedBy: creator.ID,
		})
		if !errors.Is(err, rooms.ErrInvalidSlug) {
			t.Errorf("Create(%q) error = %v, want ErrInvalidSlug", slug, err)
		}
	}
}

func TestMembersAreListed(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	guest := makeUser(t, queries, "juma")
	room := makeRoom(t, service, owner.ID, "general")
	if err := service.Join(ctx, room.ID, guest.ID); err != nil {
		t.Fatalf("Join: %v", err)
	}

	members, err := service.Members(ctx, room.ID, owner.ID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("listed %d members, want 2", len(members))
	}

	ids, err := service.MemberIDs(ctx, room.ID)
	if err != nil {
		t.Fatalf("MemberIDs: %v", err)
	}
	present := map[uuid.UUID]bool{}
	for _, id := range ids {
		present[id] = true
	}
	if !present[owner.ID] || !present[guest.ID] {
		t.Error("MemberIDs did not return every member")
	}
	_ = dbgen.Room{}
}
