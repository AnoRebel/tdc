package rooms_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
Only a channel this server holds is administered.

A project's stream and an issue's discussion belong to the project, and a
replica belongs to the server that owns it. Each is a room its owner is a member
of, so membership alone would let every operation through; the kind and the
home server are what refuse.
*/
func TestOnlyAChannelHeldHereIsAdministered(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	other := makeUser(t, queries, "juma")

	roomOf := func(kind, homeServer string) dbgen.Room {
		t.Helper()
		room, err := queries.CreateRoom(ctx, dbgen.CreateRoomParams{
			ID: uuid.New(), Name: kind, Kind: kind, Visibility: "public",
			HomeServer: homeServer, CreatedBy: &owner.ID,
		})
		if err != nil {
			t.Fatalf("create a %s room: %v", kind, err)
		}
		for _, person := range []uuid.UUID{owner.ID, other.ID} {
			if err := queries.AddRoomMember(ctx, dbgen.AddRoomMemberParams{RoomID: room.ID, UserID: person}); err != nil {
				t.Fatalf("add member: %v", err)
			}
		}
		return room
	}

	cases := []struct {
		room dbgen.Room
		want error
	}{
		{roomOf("project", service.ServerName()), rooms.ErrNotAChannel},
		{roomOf("issue-discussion", service.ServerName()), rooms.ErrNotAChannel},
		{roomOf("direct", service.ServerName()), rooms.ErrNotAChannel},
		{roomOf("channel", "elsewhere.example"), rooms.ErrHostedElsewhere},
	}
	for _, c := range cases {
		if service.Administrable(c.room) {
			t.Errorf("a %s room on %s is reported administrable", c.room.Kind, c.room.HomeServer)
		}
		if _, _, err := service.Rename(ctx, c.room.ID, owner.ID, "renamed", ""); !errors.Is(err, c.want) {
			t.Errorf("renaming a %s room on %s: %v, want %v", c.room.Kind, c.room.HomeServer, err, c.want)
		}
		if _, _, err := service.SetVisibility(ctx, c.room.ID, owner.ID, "private"); !errors.Is(err, c.want) {
			t.Errorf("changing a %s room's visibility: %v, want %v", c.room.Kind, err, c.want)
		}
		if _, err := service.Archive(ctx, c.room.ID, owner.ID); !errors.Is(err, c.want) {
			t.Errorf("archiving a %s room: %v, want %v", c.room.Kind, err, c.want)
		}
		if _, err := service.Remove(ctx, c.room.ID, owner.ID, other.ID); !errors.Is(err, c.want) {
			t.Errorf("removing somebody from a %s room: %v, want %v", c.room.Kind, err, c.want)
		}
	}

	// A disposable chat is a channel with a timer, and administered as one.
	disposable := roomOf("disposable", service.ServerName())
	if _, _, err := service.Rename(ctx, disposable.ID, owner.ID, "renamed", ""); err != nil {
		t.Errorf("renaming a disposable chat: %v", err)
	}
}

// A channel other servers carry keeps its visibility: their replicas could
// not hear of a change.
func TestAFederatedChannelKeepsItsVisibility(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	room := makeRoom(t, service, owner.ID, "general")
	if err := queries.SetRoomFederated(ctx, dbgen.SetRoomFederatedParams{ID: room.ID, Federated: true}); err != nil {
		t.Fatalf("federate: %v", err)
	}

	if _, _, err := service.SetVisibility(ctx, room.ID, owner.ID, "private"); !errors.Is(err, rooms.ErrFederatedVisibility) {
		t.Errorf("making a federated channel private: %v, want ErrFederatedVisibility", err)
	}
	// Renaming is not refused: the rename is an event, and events travel.
	if _, _, err := service.Rename(ctx, room.ID, owner.ID, "renamed", ""); err != nil {
		t.Errorf("renaming a federated channel: %v", err)
	}
}

// Changing nothing records nothing.
func TestAnAdministrativeNoChangeRecordsNothing(t *testing.T) {
	service, queries, _ := newTestService(t)
	ctx := context.Background()

	owner := makeUser(t, queries, "amina")
	room := makeRoom(t, service, owner.ID, "general")

	if _, event, err := service.Rename(ctx, room.ID, owner.ID, room.Name, room.Topic); err != nil || event.ID != "" {
		t.Errorf("renaming to the same name: event %q, error %v; want nothing recorded", event.ID, err)
	}
	if _, event, err := service.SetVisibility(ctx, room.ID, owner.ID, room.Visibility); err != nil || event.ID != "" {
		t.Errorf("setting the same visibility: event %q, error %v; want nothing recorded", event.ID, err)
	}
	if event, err := service.Restore(ctx, room.ID, owner.ID); err != nil || event.ID != "" {
		t.Errorf("restoring a channel never archived: event %q, error %v; want nothing recorded", event.ID, err)
	}
}
