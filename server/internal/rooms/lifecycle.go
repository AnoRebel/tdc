package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

// Rename changes a room's name and topic.
//
// Recorded as an event rather than only updating the row, so the change
// appears in the timeline where members can see who made it.
func (s *Service) Rename(ctx context.Context, roomID, actorID uuid.UUID, name, topic string) (dbgen.Room, error) {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return dbgen.Room{}, err
	}
	if room.ArchivedAt != nil {
		return dbgen.Room{}, ErrArchived
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = room.Name
	}

	updated, err := s.store.RenameRoom(ctx, dbgen.RenameRoomParams{
		ID:    roomID,
		Name:  trimmedName,
		Topic: strings.TrimSpace(topic),
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("rename room: %w", err)
	}

	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("read actor: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"name":  updated.Name,
		"topic": updated.Topic,
		// The previous name, so the timeline reads as a change rather than a
		// bare assertion of the new value.
		"previousName": room.Name,
	})
	if err != nil {
		return dbgen.Room{}, fmt.Errorf("encode rename: %w", err)
	}

	if _, err := s.append(ctx, updated, actor, events.TypeRoomRenamed, content, ""); err != nil {
		return dbgen.Room{}, err
	}

	return updated, nil
}

// Archive stops new messages while preserving history.
func (s *Service) Archive(ctx context.Context, roomID, actorID uuid.UUID) error {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return err
	}
	if room.ArchivedAt != nil {
		// Already archived; nothing to do and nothing to record.
		return nil
	}

	if err := s.store.ArchiveRoom(ctx, dbgen.ArchiveRoomParams{
		ID:         roomID,
		ArchivedBy: &actorID,
	}); err != nil {
		return fmt.Errorf("archive room: %w", err)
	}

	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("read actor: %w", err)
	}

	// Recorded after the room is archived but before returning, so the archive
	// event is the last thing in the timeline.
	if _, err := s.append(ctx, room, actor, events.TypeRoomArchived,
		json.RawMessage(`{}`), ""); err != nil {
		return err
	}

	return nil
}

// Restore reverses archiving, so the decision is not permanent.
func (s *Service) Restore(ctx context.Context, roomID, actorID uuid.UUID) error {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return err
	}
	if room.ArchivedAt == nil {
		return nil
	}

	if err := s.store.RestoreRoom(ctx, roomID); err != nil {
		return fmt.Errorf("restore room: %w", err)
	}

	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("read actor: %w", err)
	}

	if _, err := s.append(ctx, room, actor, events.TypeRoomRestored,
		json.RawMessage(`{}`), ""); err != nil {
		return err
	}

	return nil
}

// Join adds a member to a public room.
//
// A private room cannot be joined this way: someone must invite you, which is
// what makes it private.
func (s *Service) Join(ctx context.Context, roomID, userID uuid.UUID) error {
	room, err := s.store.GetRoomByID(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRoomNotFound
	}
	if err != nil {
		return fmt.Errorf("read room: %w", err)
	}

	if room.Visibility != "public" {
		// Reported as missing rather than forbidden, so a private room's
		// existence cannot be probed by attempting to join it.
		return ErrRoomNotFound
	}
	if room.ArchivedAt != nil {
		return ErrArchived
	}

	already, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: roomID, UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if already {
		return nil
	}

	if err := s.store.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
		RoomID: roomID, UserID: userID,
	}); err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	/*
		On a replica, the local membership row is the whole of it.

		A replicated room's events are the home server's, numbered by it, and
		writing one here would take a position it is also going to use — after
		which its own event is rejected by the unique index and replication
		into this room stops for good. The membership that matters was already
		recorded by the home server when it admitted this person; this row is
		what makes the room appear in their list and what the ordinary read
		path checks.
	*/
	if room.HomeServer != "" && !strings.EqualFold(room.HomeServer, s.serverName) {
		return nil
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("read user: %w", err)
	}

	content, err := json.Marshal(map[string]any{"member": s.UserAddress(user.Username)})
	if err != nil {
		return fmt.Errorf("encode join: %w", err)
	}

	_, err = s.append(ctx, room, user, events.TypeMemberJoined, content, "")
	return err
}

// Invite adds someone to a room, which is how a private room gains members.
func (s *Service) Invite(ctx context.Context, roomID, actorID, inviteeID uuid.UUID) error {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return err
	}
	if room.ArchivedAt != nil {
		return ErrArchived
	}

	already, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: roomID, UserID: inviteeID,
	})
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if already {
		return nil
	}

	if err := s.store.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
		RoomID:    roomID,
		UserID:    inviteeID,
		InvitedBy: &actorID,
	}); err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	invitee, err := s.store.GetUserByID(ctx, inviteeID)
	if err != nil {
		return fmt.Errorf("read invitee: %w", err)
	}
	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("read actor: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"member":    s.UserAddress(invitee.Username),
		"invitedBy": s.UserAddress(actor.Username),
	})
	if err != nil {
		return fmt.Errorf("encode invitation: %w", err)
	}

	_, err = s.append(ctx, room, actor, events.TypeMemberJoined, content, "")
	return err
}

// Leave removes the caller from a room.
func (s *Service) Leave(ctx context.Context, roomID, userID uuid.UUID) error {
	room, err := s.RequireMembership(ctx, roomID, userID)
	if err != nil {
		if errors.Is(err, ErrNotAMember) {
			return nil
		}
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("read user: %w", err)
	}

	content, err := json.Marshal(map[string]any{"member": s.UserAddress(user.Username)})
	if err != nil {
		return fmt.Errorf("encode departure: %w", err)
	}

	// Recorded while still a member, so the event is written by someone the
	// room can attribute it to.
	if _, err := s.append(ctx, room, user, events.TypeMemberLeft, content, ""); err != nil {
		return err
	}

	if err := s.store.RemoveRoomMember(ctx, dbgen.RemoveRoomMemberParams{
		RoomID: roomID, UserID: userID,
	}); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	return nil
}

// Remove takes someone out of a room. The caller must hold the permission to
// do so, which the HTTP layer checks.
func (s *Service) Remove(ctx context.Context, roomID, actorID, memberID uuid.UUID) error {
	room, err := s.RequireMembership(ctx, roomID, actorID)
	if err != nil {
		return err
	}

	member, err := s.store.GetUserByID(ctx, memberID)
	if err != nil {
		return fmt.Errorf("read member: %w", err)
	}
	actor, err := s.store.GetUserByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("read actor: %w", err)
	}

	content, err := json.Marshal(map[string]any{
		"member":    s.UserAddress(member.Username),
		"removedBy": s.UserAddress(actor.Username),
	})
	if err != nil {
		return fmt.Errorf("encode removal: %w", err)
	}

	if _, err := s.append(ctx, room, actor, events.TypeMemberRemoved, content, ""); err != nil {
		return err
	}

	if err := s.store.RemoveRoomMember(ctx, dbgen.RemoveRoomMemberParams{
		RoomID: roomID, UserID: memberID,
	}); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	return nil
}

// Discoverable returns the rooms a user may see: those they are in, plus
// public rooms they could join. A private room they are not in never appears.
func (s *Service) Discoverable(ctx context.Context, userID uuid.UUID) ([]dbgen.Room, error) {
	discoverable, err := s.store.ListRoomsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	return discoverable, nil
}

// Members returns who is in a room, for a caller permitted to see it.
func (s *Service) Members(ctx context.Context, roomID, userID uuid.UUID) ([]dbgen.ListRoomMembersRow, error) {
	if _, err := s.Visible(ctx, roomID, userID); err != nil {
		return nil, err
	}

	members, err := s.store.ListRoomMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return members, nil
}

/*
MembersWithDisclosure returns who is in a room, carrying only the details each
person allows to be shown.

asModerator opens the withheld details. It is the caller's job to have
established that the caller may moderate and to record the access — this takes
the decision rather than making it, because the permission and the audit log
both live above this package. Passing true without both is the bug this
signature cannot prevent, so the only caller that does is the one that checks.

The withholding itself happens in the query. A detail somebody has withheld is
never read out of the database, so there is no value in memory for a later
change to send by accident.
*/
func (s *Service) MembersWithDisclosure(
	ctx context.Context,
	roomID, userID uuid.UUID,
	asModerator bool,
) ([]dbgen.ListRoomMembersWithDisclosureRow, error) {
	if _, err := s.Visible(ctx, roomID, userID); err != nil {
		return nil, err
	}

	members, err := s.store.ListRoomMembersWithDisclosure(ctx,
		dbgen.ListRoomMembersWithDisclosureParams{
			RoomID:      roomID,
			AsModerator: asModerator,
		})
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return members, nil
}

// MemberIDs returns who should receive a room's events.
func (s *Service) MemberIDs(ctx context.Context, roomID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.store.ListRoomMemberIDs(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("list member ids: %w", err)
	}
	return ids, nil
}

// RoomsUserIsIn returns only the rooms the user has joined, as distinct from
// those they can merely discover.
func (s *Service) RoomsUserIsIn(ctx context.Context, userID uuid.UUID) ([]dbgen.Room, error) {
	joined, err := s.store.ListRoomsUserIsIn(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list joined rooms: %w", err)
	}
	return joined, nil
}

/*
AddMemberByInvitation admits somebody who holds a valid invitation.

Distinct from Join, which admits anybody to a public room and refuses a private
one — an invitation is the authority a private channel is joined by, so the
visibility check that Join makes would refuse exactly the case this exists for.

The caller has already established that the invitation is valid and names this
room. This does not re-check it: an authority checked in two places is an
authority two places can disagree about.
*/
func (s *Service) AddMemberByInvitation(ctx context.Context, roomID, userID uuid.UUID) error {
	room, err := s.store.GetRoomByID(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRoomNotFound
	}
	if err != nil {
		return fmt.Errorf("read room: %w", err)
	}

	// An archived channel is not joined, however somebody was invited to it:
	// the invitation may predate the archiving.
	if room.ArchivedAt != nil {
		return ErrArchived
	}

	already, err := s.store.IsRoomMember(ctx, dbgen.IsRoomMemberParams{
		RoomID: roomID, UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if already {
		return nil
	}

	if err := s.store.AddRoomMember(ctx, dbgen.AddRoomMemberParams{
		RoomID: roomID, UserID: userID,
	}); err != nil {
		return fmt.Errorf("add member: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("read user: %w", err)
	}

	content, err := json.Marshal(map[string]any{"member": s.UserAddress(user.Username)})
	if err != nil {
		return fmt.Errorf("encode join: %w", err)
	}

	_, err = s.append(ctx, room, user, events.TypeMemberJoined, content, "")
	return err
}
