package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

var (
	// ErrNoCall is returned for a call that is not open, or a room with none.
	ErrNoCall = errors.New("there is no call here")
	// ErrCallFull is returned when a call already has as many people as allowed.
	ErrCallFull = errors.New("the call is full")
	// ErrNotCallable is returned for a room calls do not happen in.
	ErrNotCallable = errors.New("calls happen in channels and direct conversations")
)

// Call is an open call and who is in it.
type Call struct {
	dbgen.Call
	Participants []dbgen.ListCallParticipantsRow
}

/*
callable is whether calls happen in a room here.

Channels, direct conversations and disposable chats — the places people talk.
Not a project's stream or an issue's discussion, and not a room another server
orders: its members on other servers could not be reached by anything this
server relays.
*/
func (s *Service) callable(room dbgen.Room) error {
	switch room.Kind {
	case "channel", "direct", "disposable":
	default:
		return ErrNotCallable
	}
	if !s.HostedHere(room) {
		return ErrHostedElsewhere
	}
	if room.ArchivedAt != nil {
		return ErrArchived
	}
	return nil
}

// OpenCall returns the call in progress in a room, for a member of it.
func (s *Service) OpenCall(ctx context.Context, roomID, userID uuid.UUID) (Call, error) {
	if _, err := s.RequireMembership(ctx, roomID, userID); err != nil {
		return Call{}, err
	}
	return s.openCall(ctx, roomID)
}

// OpenCallIn returns the call in progress in a room, without asking who wants
// to know; callers decide what of it to show.
func (s *Service) OpenCallIn(ctx context.Context, roomID uuid.UUID) (Call, error) {
	return s.openCall(ctx, roomID)
}

func (s *Service) openCall(ctx context.Context, roomID uuid.UUID) (Call, error) {
	call, err := s.store.OpenCallInRoom(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Call{}, ErrNoCall
	}
	if err != nil {
		return Call{}, fmt.Errorf("read call: %w", err)
	}
	participants, err := s.store.ListCallParticipants(ctx, call.ID)
	if err != nil {
		return Call{}, fmt.Errorf("read call participants: %w", err)
	}
	return Call{Call: call, Participants: participants}, nil
}

/*
JoinCall puts somebody in a room's call, starting one if none is open, and
returns the call-started event when this started it.

Whether they may — calling permission, a ban, a restriction — is the HTTP
layer's to have checked; this checks only what is about the room and the call.
*/
func (s *Service) JoinCall(
	ctx context.Context,
	roomID, userID uuid.UUID,
	audio, video bool,
	maxParticipants int,
) (Call, *dbgen.Event, error) {
	room, err := s.RequireMembership(ctx, roomID, userID)
	if err != nil {
		return Call{}, nil, err
	}
	if err := s.callable(room); err != nil {
		return Call{}, nil, err
	}

	started := false
	current, err := s.store.OpenCallInRoom(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		current, err = s.store.StartCall(ctx, dbgen.StartCallParams{
			ID: uuid.New(), RoomID: roomID, StartedBy: &userID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Somebody else started one in the same moment: join theirs.
			current, err = s.store.OpenCallInRoom(ctx, roomID)
		} else if err == nil {
			started = true
		}
	}
	if err != nil {
		return Call{}, nil, fmt.Errorf("open call: %w", err)
	}

	if err := s.enter(ctx, current.ID, userID, audio, video, maxParticipants); err != nil {
		return Call{}, nil, err
	}

	var startedEvent *dbgen.Event
	if started {
		content, _ := json.Marshal(map[string]string{"call": current.ID.String()})
		event, err := s.appendBy(ctx, room, userID, events.TypeCallStarted, content)
		if err != nil {
			return Call{}, nil, err
		}
		if err := s.store.SetCallStartedEvent(ctx, dbgen.SetCallStartedEventParams{
			ID: current.ID, StartedEvent: &event.ID,
		}); err != nil {
			return Call{}, nil, fmt.Errorf("record call start: %w", err)
		}
		startedEvent = &event
	}

	joined, err := s.openCall(ctx, roomID)
	return joined, startedEvent, err
}

/*
LeaveCall takes somebody out of a call, ending it if they were the last, and
returns the call-ended event when it did.
*/
func (s *Service) LeaveCall(ctx context.Context, callID, userID uuid.UUID) (*dbgen.Event, error) {
	left, err := s.store.LeaveCall(ctx, dbgen.LeaveCallParams{CallID: callID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("leave call: %w", err)
	}
	if left == 0 {
		return nil, ErrNoCall
	}
	return s.endIfEmpty(ctx, callID, userID)
}

func (s *Service) endIfEmpty(ctx context.Context, callID, actorID uuid.UUID) (*dbgen.Event, error) {
	ended, err := s.store.EndCallIfEmpty(ctx, callID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("end call: %w", err)
	}
	return s.noteCallEnded(ctx, ended, actorID)
}

// noteCallEnded writes a call's end into its conversation, with how long it lasted.
func (s *Service) noteCallEnded(ctx context.Context, ended dbgen.Call, actorID uuid.UUID) (*dbgen.Event, error) {
	room, err := s.store.GetRoomByID(ctx, ended.RoomID)
	if err != nil {
		return nil, fmt.Errorf("read call's room: %w", err)
	}
	duration := time.Duration(0)
	if ended.EndedAt != nil {
		duration = ended.EndedAt.Sub(ended.StartedAt)
	}
	content, _ := json.Marshal(map[string]any{
		"call": ended.ID.String(), "seconds": int64(duration.Seconds()),
	})
	event, err := s.appendBy(ctx, room, actorID, events.TypeCallEnded, content)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// SetCallMedia records what somebody in a call is sending, and whether they
// are holding it while in another.
func (s *Service) SetCallMedia(ctx context.Context, callID, userID uuid.UUID, audio, video, held bool) error {
	changed, err := s.store.SetCallMedia(ctx, dbgen.SetCallMediaParams{
		CallID: callID, UserID: userID, Audio: audio, Video: video, Held: held,
	})
	if err != nil {
		return fmt.Errorf("set call media: %w", err)
	}
	if changed == 0 {
		return ErrNoCall
	}
	return nil
}

// CallRoomOf returns the room of an open call somebody is in now, and false if
// they are not in it.
func (s *Service) CallRoomOf(ctx context.Context, callID, userID uuid.UUID) (uuid.UUID, bool) {
	roomID, err := s.store.CallRoomOfParticipant(ctx, dbgen.CallRoomOfParticipantParams{CallID: callID, UserID: userID})
	return roomID, err == nil
}

// CallParticipants lists who is in a call now.
func (s *Service) CallParticipants(ctx context.Context, callID uuid.UUID) ([]dbgen.ListCallParticipantsRow, error) {
	participants, err := s.store.ListCallParticipants(ctx, callID)
	if err != nil {
		return nil, fmt.Errorf("read call participants: %w", err)
	}
	return participants, nil
}

/*
SweepCalls takes everybody with no live connection out of their calls and ends
the calls that leaves empty, returning the calls whose membership changed and
the events of those that ended.
*/
func (s *Service) SweepCalls(ctx context.Context) ([]SweptCall, []dbgen.Event, error) {
	gone, err := s.store.LeaveCallsOfDisconnected(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("sweep calls: %w", err)
	}
	seen := map[uuid.UUID]bool{}
	var changed []SweptCall
	var ended []dbgen.Event
	for _, row := range gone {
		if seen[row.CallID] {
			continue
		}
		seen[row.CallID] = true
		changed = append(changed, SweptCall{CallID: row.CallID, RoomID: row.RoomID})
		event, err := s.endIfEmpty(ctx, row.CallID, row.UserID)
		if err != nil {
			return changed, ended, err
		}
		if event != nil {
			ended = append(ended, *event)
		}
	}
	abandoned, err := s.store.EndAbandonedCalls(ctx)
	if err != nil {
		return changed, ended, fmt.Errorf("end abandoned calls: %w", err)
	}
	for _, call := range abandoned {
		if call.StartedBy == nil {
			continue
		}
		event, err := s.noteCallEnded(ctx, call, *call.StartedBy)
		if err != nil {
			return changed, ended, err
		}
		ended = append(ended, *event)
	}
	return changed, ended, nil
}

// SweptCall is a call somebody was taken out of by the sweep.
type SweptCall struct {
	CallID uuid.UUID
	RoomID uuid.UUID
}

// CallRecord returns a call, open or not, by its identifier.
func (s *Service) CallRecord(ctx context.Context, callID uuid.UUID) (dbgen.Call, error) {
	call, err := s.store.CallByID(ctx, callID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Call{}, ErrNoCall
	}
	return call, err
}

// enter puts somebody in an open call, if it has room for them.
func (s *Service) enter(ctx context.Context, callID, userID uuid.UUID, audio, video bool, maxParticipants int) error {
	present, err := s.store.ListCallParticipants(ctx, callID)
	if err != nil {
		return fmt.Errorf("read call participants: %w", err)
	}
	others := 0
	for _, participant := range present {
		if participant.UserID != userID {
			others++
		}
	}
	// ponytail: checked then joined without a lock, so two people joining at
	// once can take a call one over its limit; a row lock on the call when
	// that ever matters.
	if others >= maxParticipants {
		return ErrCallFull
	}
	if err := s.store.JoinCall(ctx, dbgen.JoinCallParams{
		CallID: callID, UserID: userID, Audio: audio, Video: video,
	}); err != nil {
		return fmt.Errorf("join call: %w", err)
	}
	return nil
}

/*
InviteToCall lets people into one call who are not in its conversation.

Only somebody in the call may invite, and only people they are in another call
with now — the people of a call being merged into this one. It is a merge, not
a way to reach anybody at all; who may be rung is still each invitee's to say,
and the HTTP layer checks that.
*/
func (s *Service) InviteToCall(ctx context.Context, callID, inviter uuid.UUID, invitees []uuid.UUID) ([]uuid.UUID, error) {
	if _, in := s.CallRoomOf(ctx, callID, inviter); !in {
		return nil, ErrNoCall
	}
	var invited []uuid.UUID
	for _, invitee := range invitees {
		if invitee == inviter {
			continue
		}
		shared, err := s.store.ShareAnOpenCall(ctx, dbgen.ShareAnOpenCallParams{UserID: inviter, UserID_2: invitee})
		if err != nil {
			return invited, fmt.Errorf("check shared call: %w", err)
		}
		if !shared {
			continue
		}
		if err := s.store.InviteToCall(ctx, dbgen.InviteToCallParams{
			CallID: callID, UserID: invitee, InvitedBy: &inviter,
		}); err != nil {
			return invited, fmt.Errorf("invite to call: %w", err)
		}
		invited = append(invited, invitee)
	}
	return invited, nil
}

/*
CallFor returns an open call by its identifier to somebody entitled to it: a
member of its conversation, somebody in it, or somebody invited into it.
*/
func (s *Service) CallFor(ctx context.Context, callID, userID uuid.UUID) (Call, bool, error) {
	record, err := s.CallRecord(ctx, callID)
	if err != nil {
		return Call{}, false, err
	}
	if record.EndedAt != nil {
		return Call{}, false, ErrNoCall
	}
	guest := false
	if _, err := s.RequireMembership(ctx, record.RoomID, userID); err != nil {
		_, in := s.CallRoomOf(ctx, callID, userID)
		invited, inviteErr := s.store.IsInvitedToCall(ctx, dbgen.IsInvitedToCallParams{CallID: callID, UserID: userID})
		if inviteErr != nil {
			return Call{}, false, fmt.Errorf("check call invite: %w", inviteErr)
		}
		if !in && !invited {
			return Call{}, false, ErrNoCall
		}
		guest = true
	}
	call, err := s.openCall(ctx, record.RoomID)
	if err != nil || call.ID != callID {
		return Call{}, false, ErrNoCall
	}
	return call, guest, nil
}

// JoinInvited puts somebody invited into a call into it.
func (s *Service) JoinInvited(ctx context.Context, callID, userID uuid.UUID, audio, video bool, maxParticipants int) (Call, error) {
	call, _, err := s.CallFor(ctx, callID, userID)
	if err != nil {
		return Call{}, err
	}
	if err := s.enter(ctx, callID, userID, audio, video, maxParticipants); err != nil {
		return Call{}, err
	}
	return s.openCall(ctx, call.RoomID)
}
