package rooms

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/google/uuid"
)

var (
	// ErrScheduledInThePast is returned for a send time that has already been.
	ErrScheduledInThePast = errors.New("that time has already passed")
	// ErrScheduledTooFar is returned for a send time beyond the limit.
	ErrScheduledTooFar = errors.New("that is too far ahead to schedule")
	// ErrNoSuchScheduledMessage is returned when cancelling something that is
	// not there, or is not the caller's.
	ErrNoSuchScheduledMessage = errors.New("no such scheduled message")
)

const (
	// minLead keeps a scheduled message from racing the delivery pass. Below
	// this, sending it now is what somebody actually meant.
	minLead = 10 * time.Second
	// maxLead bounds how far ahead something may be scheduled. A year is far
	// past any real use and stops the table accumulating messages nobody
	// remembers writing.
	maxLead = 365 * 24 * time.Hour
)

// ScheduledMessage is something written now and sent later.
type ScheduledMessage struct {
	ID            uuid.UUID
	RoomID        uuid.UUID
	Body          string
	AttachmentIDs []uuid.UUID
	ThreadRootID  string
	SendAt        int64
}

/*
Schedule records a message to be sent later.

Held outside the event log until it is sent. An event is a fact about something
that happened, and a message that has not been sent has not happened — putting
it in the log early would mean either that it appears in timelines before its
time or that every reader has to know to filter it out.
*/
func (s *Service) Schedule(
	ctx context.Context,
	roomID, senderID uuid.UUID,
	body string,
	attachmentIDs []uuid.UUID,
	threadRootID string,
	sendAt time.Time,
) (ScheduledMessage, error) {
	if _, err := s.RequireMembership(ctx, roomID, senderID); err != nil {
		return ScheduledMessage{}, err
	}

	now := time.Now()
	if sendAt.Before(now.Add(minLead)) {
		return ScheduledMessage{}, ErrScheduledInThePast
	}
	if sendAt.After(now.Add(maxLead)) {
		return ScheduledMessage{}, ErrScheduledTooFar
	}

	sender, err := s.store.GetUserByID(ctx, senderID)
	if err != nil {
		return ScheduledMessage{}, fmt.Errorf("read sender: %w", err)
	}

	if threadRootID != "" {
		if _, err := s.EventInRoom(ctx, roomID, threadRootID); err != nil {
			return ScheduledMessage{}, err
		}
	}

	row, err := s.store.CreateScheduledMessage(ctx, dbgen.CreateScheduledMessageParams{
		ID:            uuid.New(),
		RoomID:        roomID,
		SenderID:      senderID,
		SenderAddress: s.UserAddress(sender.Username),
		Body:          body,
		AttachmentIds: attachmentIDs,
		ThreadRootID:  optionalString(threadRootID),
		SendAt:        sendAt,
	})
	if err != nil {
		return ScheduledMessage{}, fmt.Errorf("schedule a message: %w", err)
	}

	return scheduledOf(row), nil
}

// ScheduledFor lists somebody's own outstanding messages, soonest first.
func (s *Service) ScheduledFor(
	ctx context.Context,
	senderID uuid.UUID,
) ([]ScheduledMessage, error) {
	rows, err := s.store.ListScheduledMessagesForUser(ctx, senderID)
	if err != nil {
		return nil, fmt.Errorf("read scheduled messages: %w", err)
	}

	scheduled := make([]ScheduledMessage, 0, len(rows))
	for _, row := range rows {
		scheduled = append(scheduled, scheduledOf(row))
	}
	return scheduled, nil
}

// CancelScheduled withdraws a message that has not been sent.
//
// Scoped to the sender: cancelling is theirs to do, and a message already sent
// is a message rather than a plan.
func (s *Service) CancelScheduled(ctx context.Context, id, senderID uuid.UUID) error {
	affected, err := s.store.DeleteScheduledMessage(ctx, dbgen.DeleteScheduledMessageParams{
		ID:       id,
		SenderID: senderID,
	})
	if err != nil {
		return fmt.Errorf("cancel a scheduled message: %w", err)
	}
	if affected == 0 {
		return ErrNoSuchScheduledMessage
	}
	return nil
}

/*
ScheduledDelivery is what sending a due message needs from outside this package.

A scheduled message is an ordinary message sent late, so it passes the checks
an ordinary one does and is followed by what follows one — delivery to the
people reading, notifications, search, webhooks. Those live with the HTTP
server rather than here, so they are handed in.
*/
type ScheduledDelivery struct {
	// Bind ties the message's files to it once it exists.
	Bind AttachmentBinder
	// MayPost is asked at send time, as it is when somebody posts directly:
	// a ban, a restriction or a withdrawn permission made after scheduling
	// stops the message as it would have stopped them typing it.
	MayPost func(ctx context.Context, roomID, senderID uuid.UUID) error
	// Sent is told about each message once it is out.
	Sent func(ctx context.Context, event dbgen.Event, row dbgen.ScheduledMessage)
}

/*
DeliverDue sends whatever is ready, returning how many it sent.

Called on a timer, on every server. Each pass claims the rows it will send in
the statement that finds them, so several servers can run this at once without
any message being sent twice — which is the failure that matters here, since an
unsent message can be sent on the next pass but a message sent twice cannot be
unsent.

A message whose room, membership or permission has changed since it was
scheduled is recorded as failed rather than retried: somebody who has left a
channel should not have a message appear in it a week later.
*/
func (s *Service) DeliverDue(
	ctx context.Context,
	limit int32,
	delivery ScheduledDelivery,
	log *slog.Logger,
) (int, error) {
	if limit <= 0 {
		limit = 50
	}

	due, err := s.store.ClaimDueScheduledMessages(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("claim due messages: %w", err)
	}
	// RETURNING keeps no order, and two messages due in the same pass should
	// arrive in the order they were meant to.
	slices.SortFunc(due, func(a, b dbgen.ScheduledMessage) int { return a.SendAt.Compare(b.SendAt) })

	sent := 0
	for _, row := range due {
		event, err := s.deliver(ctx, row, delivery)
		if err != nil {
			// Recorded rather than retried forever. The sender can see why.
			if markErr := s.store.MarkScheduledMessageFailed(ctx,
				dbgen.MarkScheduledMessageFailedParams{
					ID:           row.ID,
					FailedReason: err.Error(),
				}); markErr != nil {
				log.Error("cannot record a failed scheduled message",
					"id", row.ID, "error", markErr)
			}
			log.Warn("a scheduled message could not be sent", "id", row.ID, "error", err)
			continue
		}

		if err := s.store.MarkScheduledMessageSent(ctx, dbgen.MarkScheduledMessageSentParams{
			ID:          row.ID,
			SentEventID: &event.ID,
		}); err != nil {
			// The message is out. Failing to record that is worth logging
			// loudly, since a pass after the claim expires would send it again.
			log.Error("a scheduled message was sent but not recorded as sent",
				"id", row.ID, "eventId", event.ID, "error", err)
		}
		if delivery.Sent != nil {
			delivery.Sent(ctx, event, row)
		}
		sent++
	}

	return sent, nil
}

/*
AttachmentBinder ties uploaded files to the message that carries them.

An interface rather than a direct call, because binding lives in the
attachments package and reaching into it from here would make rooms depend on
storage. The scheduler holds the files by identifier until send time, which is
the only moment an event identifier exists to bind them to.
*/
type AttachmentBinder func(
	ctx context.Context,
	eventID string,
	roomID, uploaderID uuid.UUID,
	ids []uuid.UUID,
) (int64, error)

// deliver sends one scheduled message as an ordinary one.
func (s *Service) deliver(
	ctx context.Context,
	row dbgen.ScheduledMessage,
	delivery ScheduledDelivery,
) (dbgen.Event, error) {
	// Membership is checked again at send time, not only when scheduling: a
	// week is long enough to leave a channel, be removed from one, or have the
	// room archived.
	if _, err := s.RequireMembership(ctx, row.RoomID, row.SenderID); err != nil {
		return dbgen.Event{}, err
	}
	if delivery.MayPost != nil {
		if err := delivery.MayPost(ctx, row.RoomID, row.SenderID); err != nil {
			return dbgen.Event{}, err
		}
	}

	threadRootID := ""
	if row.ThreadRootID != nil {
		threadRootID = *row.ThreadRootID
	}

	var event dbgen.Event
	var err error

	// A fresh token each time: the idempotency key exists to collapse a retried
	// request from a client, and this is not one.
	token := uuid.New().String()

	if threadRootID != "" {
		event, err = s.ReplyInThread(ctx, row.RoomID, row.SenderID, threadRootID, row.Body, token)
	} else {
		event, err = s.SendMessage(ctx, row.RoomID, row.SenderID, row.Body, token)
	}
	if err != nil {
		return dbgen.Event{}, err
	}

	// Bound after the event exists, since that is what they attach to. A
	// failure here leaves the message sent without its files rather than
	// unsent, which is the better of the two: the words arrived.
	if delivery.Bind != nil && len(row.AttachmentIds) > 0 {
		if _, bindErr := delivery.Bind(ctx, event.ID, row.RoomID, row.SenderID, row.AttachmentIds); bindErr != nil {
			return event, nil
		}
	}

	return event, nil
}

func scheduledOf(row dbgen.ScheduledMessage) ScheduledMessage {
	message := ScheduledMessage{
		ID:            row.ID,
		RoomID:        row.RoomID,
		Body:          row.Body,
		AttachmentIDs: row.AttachmentIds,
		SendAt:        row.SendAt.UnixMilli(),
	}
	if row.ThreadRootID != nil {
		message.ThreadRootID = *row.ThreadRootID
	}
	return message
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
