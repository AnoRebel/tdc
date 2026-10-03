package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
scheduleInterval is how often due messages are looked for.

A minute rather than a second: somebody scheduling a message for tomorrow
morning does not care about the difference, and the query runs against an index
of exactly what is due, so a pass that finds nothing costs almost nothing.
*/
const scheduleInterval = time.Minute

/*
RunScheduled sends scheduled messages as they come due, until ctx ends.

Here rather than in the command, because a scheduled message is an ordinary
message sent late and what follows an ordinary message — the live frame,
notifications, search, webhooks, the other nodes — belongs to the server. Run
from the command, a scheduled message was stored and nothing else: nobody saw it
arrive, and nothing watching the channel heard of it.
*/
func (s *Server) RunScheduled(ctx context.Context) {
	ticker := time.NewTicker(scheduleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.DeliverScheduled(ctx)
		}
	}
}

/*
DeliverScheduled makes one delivery pass, returning how many messages it sent.

Exported so a test can make a pass when it chooses rather than wait out the
interval.
*/
func (s *Server) DeliverScheduled(ctx context.Context) int {
	delivery := rooms.ScheduledDelivery{
		MayPost: s.mayStillPost,
		Sent: func(ctx context.Context, event dbgen.Event, row dbgen.ScheduledMessage) {
			if event.ThreadRootID != nil {
				s.afterReply(ctx, event, row.SenderID, row.SenderAddress)
				return
			}
			s.afterMessage(ctx, event, row.Body)
		},
	}
	if s.attachments != nil {
		delivery.Bind = s.attachments.Bind
	}

	sent, err := s.rooms.DeliverDue(ctx, 50, delivery, s.log)
	if err != nil && ctx.Err() == nil {
		// Logged rather than fatal: a failed pass means a message goes out on
		// the next one, not that the server cannot serve.
		s.log.Error("scheduled delivery failed", slog.String("error", err.Error()))
	}
	if sent > 0 {
		s.log.Info("sent scheduled messages", "count", sent)
	}
	return sent
}

/*
mayStillPost asks, at send time, what posting directly would have asked.

What it says becomes the reason the sender sees beside the message that did
not go, so it is written for them rather than as a status code.
*/
func (s *Server) mayStillPost(ctx context.Context, roomID, senderID uuid.UUID) error {
	if s.authz != nil {
		permissions, err := s.authz.PermissionsFor(ctx, senderID)
		if err != nil {
			return err
		}
		if !permissions.AllowsChannel(authz.PermChannelPost, roomID).Allowed {
			return errors.New("you can no longer post in this channel")
		}
	}

	if s.moderation != nil {
		if s.moderation.IsBanned(ctx, roomID, senderID) {
			return errors.New("you are banned from this channel")
		}
		if s.moderation.IsPostingRestricted(ctx, senderID, roomID) {
			return errors.New("you could not post in this channel when it was due")
		}
	}

	return nil
}
