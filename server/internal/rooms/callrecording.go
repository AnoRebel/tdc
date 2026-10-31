package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Recording a call is the recorder's device's work; the server's part is saying
so. A recording is state on the recorder's place in the call, set here before
their client begins, and an event in the conversation's log at each end — so
whoever reads the conversation later sees who recorded and when.
*/

// StoppedRecording is a recording the server stopped because its recorder was
// no longer in the call.
type StoppedRecording struct {
	CallID uuid.UUID
	RoomID uuid.UUID
	UserID uuid.UUID
	Event  dbgen.Event
}

/*
StartRecording marks somebody in a call as recording it and writes that into
the conversation's log, returning the event; nil when they already were.

Only a member of the conversation records it: the recording is saved there,
and the log it is noted in is theirs. If the event cannot be written, the mark
is taken back, so nobody is shown recording what the log does not say.
*/
func (s *Service) StartRecording(ctx context.Context, callID, userID uuid.UUID) (*dbgen.Event, error) {
	roomID, in := s.CallRoomOf(ctx, callID, userID)
	if !in {
		return nil, ErrNoCall
	}
	room, err := s.RequireMembership(ctx, roomID, userID)
	if err != nil {
		return nil, err
	}
	marked, err := s.store.StartCallRecording(ctx, dbgen.StartCallRecordingParams{CallID: callID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("mark recording: %w", err)
	}
	if marked == 0 {
		return nil, nil
	}
	content, _ := json.Marshal(map[string]string{"call": callID.String()})
	event, err := s.appendBy(ctx, room, userID, events.TypeCallRecordingStarted, content)
	if err != nil {
		_ = s.store.UndoCallRecording(ctx, dbgen.UndoCallRecordingParams{CallID: callID, UserID: userID})
		return nil, err
	}
	return &event, nil
}

// StopRecording stops somebody's recording of a call and notes it in the
// conversation's log, returning the event; nil when they were not recording.
func (s *Service) StopRecording(ctx context.Context, callID, userID uuid.UUID) (*dbgen.Event, error) {
	began, err := s.store.StopCallRecording(ctx, dbgen.StopCallRecordingParams{CallID: callID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stop recording: %w", err)
	}
	record, err := s.CallRecord(ctx, callID)
	if err != nil {
		return nil, err
	}
	event, err := s.noteRecordingStopped(ctx, record.RoomID, callID, userID, began)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// StopAbsentRecordings stops the recordings of everybody no longer in their
// call — gone, dropped, or the call over — and notes each in its log.
func (s *Service) StopAbsentRecordings(ctx context.Context) ([]StoppedRecording, error) {
	rows, err := s.store.StopAbsentRecordings(ctx)
	if err != nil {
		return nil, fmt.Errorf("stop absent recordings: %w", err)
	}
	stopped := make([]StoppedRecording, 0, len(rows))
	for _, row := range rows {
		event, err := s.noteRecordingStopped(ctx, row.RoomID, row.CallID, row.UserID, row.Began)
		if err != nil {
			return stopped, err
		}
		stopped = append(stopped, StoppedRecording{CallID: row.CallID, RoomID: row.RoomID, UserID: row.UserID, Event: event})
	}
	return stopped, nil
}

func (s *Service) noteRecordingStopped(ctx context.Context, roomID, callID, userID uuid.UUID, began time.Time) (dbgen.Event, error) {
	room, err := s.store.GetRoomByID(ctx, roomID)
	if err != nil {
		return dbgen.Event{}, fmt.Errorf("read call's room: %w", err)
	}
	content, _ := json.Marshal(map[string]any{
		"call": callID.String(), "seconds": int64(time.Since(began).Seconds()),
	})
	return s.appendBy(ctx, room, userID, events.TypeCallRecordingStopped, content)
}
