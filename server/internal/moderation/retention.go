package moderation

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Retention: removing content once it is older than a channel's policy allows.

Expiry redacts rather than deletes. The event stays, its content goes, and the
signed chain around it stays verifiable — a deleted row would leave a hole in a
log whose whole value is that it has no holes. What a reader sees afterwards is
the same placeholder a moderator's removal leaves, which is deliberate: the
mechanism for making a message unreadable should be one mechanism.

A channel's own period wins over the server's default, and no period anywhere
means content is kept. That last is what makes retention safe to ship switched
on: a server nobody has configured deletes nothing.
*/

// ExpiredMessage is a message that has passed its retention period.
type ExpiredMessage struct {
	EventID string
	RoomID  uuid.UUID
}

/*
AttachmentRemover deletes the files a message carried.

An interface rather than a dependency on the attachments package, which would
be a cycle: attachments already reaches for moderation to ask whether media is
marked. The caller wires the real one.
*/
type AttachmentRemover func(ctx context.Context, ids []uuid.UUID) error

/*
ExpireDue removes content past its retention period, in batches.

Batched because a server that has been running with no policy and then gets one
has every message it has ever held to expire at once, and a single statement
covering all of it would hold locks for as long as that takes. Successive
passes are how it drains.

defaultSeconds is the server-wide policy, zero when the operator has set none.
*/
func (s *Service) ExpireDue(
	ctx context.Context,
	defaultSeconds int32,
	batch int32,
	removeAttachments AttachmentRemover,
) (int, error) {
	if batch <= 0 {
		batch = 100
	}

	expired, err := s.queries.ExpireEvents(ctx, dbgen.ExpireEventsParams{
		DefaultSeconds: defaultSeconds,
		BatchSize:      batch,
	})
	if err != nil {
		return 0, fmt.Errorf("moderation: expiring content: %w", err)
	}
	if len(expired) == 0 {
		return 0, nil
	}

	eventIDs := make([]string, 0, len(expired))
	for _, event := range expired {
		eventIDs = append(eventIDs, event.ID)
	}

	/*
		The attachments go with the messages.

		Done after the redaction rather than before: a message whose text
		survived while its photographs went would be worse than the reverse,
		since the reader would see a message that appears intact and is not. If
		this fails the text is already gone and the files are reclaimed by the
		orphan sweep, which is the acceptable direction to fail in.
	*/
	if removeAttachments != nil {
		files, err := s.queries.ListAttachmentsForEvents(ctx, eventIDs)
		if err != nil {
			return len(expired), fmt.Errorf("moderation: listing expired attachments: %w", err)
		}

		if len(files) > 0 {
			ids := make([]uuid.UUID, 0, len(files))
			for _, file := range files {
				ids = append(ids, file.ID)
			}
			if err := removeAttachments(ctx, ids); err != nil {
				return len(expired), fmt.Errorf("moderation: removing expired attachments: %w", err)
			}
		}
	}

	s.log.Info("content expired", "messages", len(expired))
	return len(expired), nil
}

// RetentionPolicy is what applies to one channel, for disclosure to its
// members.
type RetentionPolicy struct {
	// Seconds is how long content is kept, or zero for indefinitely.
	Seconds int32
	// FromChannel says the channel sets its own, rather than inheriting the
	// server's. Members should be able to tell the two apart: one is a
	// decision about this conversation and the other is a decision about
	// everything.
	FromChannel bool
}

// PolicyFor reports the retention that applies to a channel.
func PolicyFor(channelSeconds *int32, defaultSeconds int32) RetentionPolicy {
	if channelSeconds != nil && *channelSeconds > 0 {
		return RetentionPolicy{Seconds: *channelSeconds, FromChannel: true}
	}
	return RetentionPolicy{Seconds: defaultSeconds}
}

// Keeps reports whether this policy removes anything at all.
func (p RetentionPolicy) Keeps() bool { return p.Seconds <= 0 }
