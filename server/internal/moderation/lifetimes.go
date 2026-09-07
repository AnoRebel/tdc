package moderation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Lifetimes attached to individual messages.

Separate from a room's retention: somebody can give one message an hour in a
conversation that keeps everything, which is the case this exists for. A
photograph shared once, an address, a password read out to somebody — things
whose sender wants them gone without making the whole conversation
disposable.

Three ways to say when. A duration from sending is the simple one. Expiring
once everybody else has seen it suits something meant to be read rather than
kept. Expiring a while after each person first sees it gives every recipient
the same window, however long they took to look — which is the only one of the
three that is fair to somebody who was asleep.
*/

// LifetimeKind says how a message's lifetime is reckoned.
type LifetimeKind string

const (
	// LifetimeAfterSending expires a fixed time after the message was sent,
	// whether or not anybody read it.
	LifetimeAfterSending LifetimeKind = "after_sending"
	// LifetimeOnceSeen expires as soon as every other participant has read it.
	LifetimeOnceSeen LifetimeKind = "once_seen"
	// LifetimeAfterSeeing gives each recipient the same window from their own
	// first reading, and expires when the last of those has run out.
	LifetimeAfterSeeing LifetimeKind = "after_seeing"
)

var (
	// ErrUnknownLifetime is returned for a kind this server does not define.
	ErrUnknownLifetime = errors.New("that is not a way a message can expire")
	// ErrLifetimeNeedsDuration is returned when a kind that measures time is
	// given none.
	ErrLifetimeNeedsDuration = errors.New("that kind of lifetime needs a duration")
	/*
		ErrLifetimeExtended is returned for a change that would make a message
		last longer.

		Shortening is somebody deciding they want it gone sooner, which is
		theirs to decide. Lengthening is somebody reaching back into a
		conversation where everybody was told this would be gone — and the
		people who read it on that basis are not there to be asked again.
	*/
	ErrLifetimeExtended = errors.New("a lifetime can be shortened but not extended")
)

func knownLifetime(kind LifetimeKind) bool {
	switch kind {
	case LifetimeAfterSending, LifetimeOnceSeen, LifetimeAfterSeeing:
		return true
	}
	return false
}

// LifetimeKinds lists what a client may ask for, so it does not hard-code a
// list that can drift from what the server accepts.
func LifetimeKinds() []LifetimeKind {
	return []LifetimeKind{LifetimeAfterSending, LifetimeOnceSeen, LifetimeAfterSeeing}
}

// Lifetime is a message's expiry as it currently stands.
type Lifetime struct {
	EventID string
	Kind    LifetimeKind
	// Seconds is the duration for the kinds that use one, zero otherwise.
	Seconds int32
	// ExpiresAt is when the message becomes removable, zero while that is not
	// yet known — which is the ordinary state of a seen-based lifetime nobody
	// has read yet.
	ExpiresAt time.Time
}

/*
SetLifetime gives a message a lifetime, or replaces one it has.

Refuses anything that would make the message last longer than it already
would. Which of two lifetimes is longer is not always a number comparison —
see extends.
*/
func (s *Service) SetLifetime(
	ctx context.Context,
	eventID string,
	roomID uuid.UUID,
	sender uuid.UUID,
	kind LifetimeKind,
	seconds int32,
	sentAt time.Time,
) (Lifetime, error) {
	if !knownLifetime(kind) {
		return Lifetime{}, fmt.Errorf("%w: %q", ErrUnknownLifetime, kind)
	}
	if kind != LifetimeOnceSeen && seconds <= 0 {
		return Lifetime{}, ErrLifetimeNeedsDuration
	}

	existing, err := s.queries.GetMessageLifetime(ctx, eventID)
	switch {
	case err == nil:
		if extends(existing, kind, seconds, sentAt) {
			return Lifetime{}, ErrLifetimeExtended
		}
	case errors.Is(err, pgx.ErrNoRows):
		// No lifetime yet, so anything is a shortening of forever.
	default:
		return Lifetime{}, fmt.Errorf("moderation: reading a lifetime: %w", err)
	}

	var duration *int32
	if kind != LifetimeOnceSeen {
		value := seconds
		duration = &value
	}

	/*
		Only a duration from sending knows its expiry now.

		The seen-based kinds are left open until somebody reads, and the sweep
		resolves them. Guessing an expiry for them here would mean a message
		expiring on a schedule nobody agreed to.
	*/
	var expiresAt *time.Time
	if kind == LifetimeAfterSending {
		when := sentAt.Add(time.Duration(seconds) * time.Second)
		expiresAt = &when
	}

	row, err := s.queries.SetMessageLifetime(ctx, dbgen.SetMessageLifetimeParams{
		EventID:   eventID,
		RoomID:    roomID,
		SenderID:  &sender,
		Kind:      string(kind),
		Seconds:   duration,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return Lifetime{}, fmt.Errorf("moderation: setting a lifetime: %w", err)
	}

	return lifetimeOf(row), nil
}

/*
extends reports whether a proposed lifetime would make a message last longer
than the one it has.

Not a number comparison, because the kinds are not commensurable. What decides
it is when each would remove the message at the earliest, so that a change is
allowed exactly when it cannot delay the removal:

  - Once seen is the shortest thing there is. It removes the message as soon as
    everybody has read it, which is never later than any duration.
  - A duration from sending is fixed and known.
  - A duration after seeing is unbounded: somebody who never reads it never
    starts their clock, so it can outlast any fixed duration however small its
    own number looks.

So a five-minute after-seeing is not shorter than a one-hour after-sending, and
treating it as such would be the extension this refuses, dressed as a
reduction.
*/
func extends(existing dbgen.MessageLifetime, kind LifetimeKind, seconds int32, sentAt time.Time) bool {
	current := LifetimeKind(existing.Kind)

	// Once seen removes it at the first moment everybody has read it. Nothing
	// is sooner, so moving to it is always a shortening and moving away from
	// it always an extension.
	if kind == LifetimeOnceSeen {
		return false
	}
	if current == LifetimeOnceSeen {
		return true
	}

	/*
		After seeing is unbounded above but not below: somebody who never reads
		it never starts their clock, and somebody who reads it immediately
		starts it now. So it can neither replace nor be replaced by a fixed
		duration without one of the two directions being an extension.

		Moving to it from a fixed duration extends, because it may never
		expire. Moving away from it to a fixed duration also extends whenever
		that duration could outlast the window a reader who has already looked
		is on — which cannot be ruled out, since the reader who looked earliest
		may have looked a moment ago.

		The one comparison that holds is between two after-seeing windows,
		where the shorter window is unambiguously sooner for every reader.
	*/
	if kind == LifetimeAfterSeeing || current == LifetimeAfterSeeing {
		if kind == LifetimeAfterSeeing && current == LifetimeAfterSeeing {
			return seconds > durationOf(existing)
		}
		return true
	}

	// Both are durations from sending, and the later expiry is the longer one.
	return seconds > durationOf(existing)
}

func durationOf(row dbgen.MessageLifetime) int32 {
	if row.Seconds == nil {
		return 0
	}
	return *row.Seconds
}

func lifetimeOf(row dbgen.MessageLifetime) Lifetime {
	lifetime := Lifetime{
		EventID: row.EventID,
		Kind:    LifetimeKind(row.Kind),
		Seconds: durationOf(row),
	}
	if row.ExpiresAt != nil {
		lifetime.ExpiresAt = *row.ExpiresAt
	}
	return lifetime
}

// LifetimesFor reads the lifetimes carried by a page of messages, so a
// timeline can disclose them without a query per message.
func (s *Service) LifetimesFor(ctx context.Context, eventIDs []string) (map[string]Lifetime, error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListLifetimesForEvents(ctx, eventIDs)
	if err != nil {
		return nil, fmt.Errorf("moderation: reading lifetimes: %w", err)
	}

	lifetimes := make(map[string]Lifetime, len(rows))
	for _, row := range rows {
		lifetimes[row.EventID] = lifetimeOf(row)
	}
	return lifetimes, nil
}

/*
RecordViews notes that somebody has seen messages that expire after seeing.

Called when a timeline is read. Only the first view of each message counts, so
a second look does not restart anybody's clock.
*/
func (s *Service) RecordViews(ctx context.Context, viewer uuid.UUID, eventIDs []string) error {
	for _, eventID := range eventIDs {
		if err := s.queries.RecordMessageView(ctx, dbgen.RecordMessageViewParams{
			EventID:  eventID,
			ViewerID: viewer,
		}); err != nil {
			return fmt.Errorf("moderation: recording a view: %w", err)
		}
	}
	return nil
}

/*
ExpireLifetimes removes messages whose lifetime has run out.

Resolves the seen-based kinds first, so a message that became due since the
last pass is removed on this one rather than the next.
*/
func (s *Service) ExpireLifetimes(
	ctx context.Context,
	batch int32,
	removeAttachments AttachmentRemover,
) (int, error) {
	if batch <= 0 {
		batch = 100
	}

	if err := s.queries.ResolveOnceSeenLifetimes(ctx); err != nil {
		return 0, fmt.Errorf("moderation: resolving seen lifetimes: %w", err)
	}
	if err := s.queries.ResolveAfterSeeingLifetimes(ctx); err != nil {
		return 0, fmt.Errorf("moderation: resolving after-seeing lifetimes: %w", err)
	}

	expired, err := s.queries.ExpireMessagesByLifetime(ctx, batch)
	if err != nil {
		return 0, fmt.Errorf("moderation: expiring by lifetime: %w", err)
	}
	if len(expired) == 0 {
		return 0, nil
	}

	eventIDs := make([]string, 0, len(expired))
	for _, event := range expired {
		eventIDs = append(eventIDs, event.ID)
	}

	// The attachments go with the message, for the same reason retention takes
	// them: text that vanished while its photographs stayed would have expired
	// nothing that mattered.
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

	s.log.Info("messages expired by their own lifetime", "messages", len(expired))
	return len(expired), nil
}
