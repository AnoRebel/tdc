package webhooks

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

// Settings is the policy surface the queue reads, on every decision, so an
// operator's change applies to the next delivery rather than the next start.
type Settings interface {
	Int(ctx context.Context, key settings.Key) int64
}

// Config is what the queue needs from the rest of the server.
type Config struct {
	// Server names this server in every payload.
	Server string
	// CanRead is asked when subscribing and before every delivery.
	CanRead Access
	// Holds says whether somebody holds a server permission now, for an event
	// that belongs to no room. Nil holds that nobody does.
	Holds func(ctx context.Context, userID uuid.UUID, permission string) bool
	// Describe says what a room belongs to. Nil treats every room as a
	// channel.
	Describe func(ctx context.Context, roomID uuid.UUID) Source
	/*
		OfferedIn says, for an event stored in a room nobody subscribes to, which
		room's subscribers it is delivered to instead — an event's calendar
		entries, to its channel's. False means its own room. Asked again before
		sending, so an entry whose event has since stopped being offered anywhere
		answers a room nobody can read, and is not sent. Nil is always false.
	*/
	OfferedIn func(ctx context.Context, event dbgen.Event) (uuid.UUID, bool)
	// AllowPrivate is the operator's decision about private destinations.
	AllowPrivate func(ctx context.Context) bool
	// Send signs a delivery and sends it, within the operator's timeout.
	Send func(ctx context.Context, endpoint, id, kind string, body, secret []byte) delivery.Outcome
	// Limiter holds each subscription to its rate.
	Limiter cache.Rates
	// Settings may be nil, which is every default.
	Settings Settings
	// Disabled tells a subscription's owner it was turned off.
	Disabled func(ctx context.Context, subscription dbgen.WebhookSubscription, eventID *string)
}

const (
	// pollInterval is how often an idle worker looks for due deliveries it
	// was not woken for — retries coming due, mostly.
	pollInterval = 250 * time.Millisecond

	// staleAfter is how long a claimed delivery may go unfinished before it
	// is put back, which only happens when a process ended mid-attempt.
	staleAfter = 5 * time.Minute

	// sweepInterval is how often stale claims are released and old history
	// pruned.
	sweepInterval = time.Minute
)

/*
Run delivers until ctx ends.

One dispatcher claims due deliveries and hands them to a fixed pool of workers,
sized by the operator. The pool is the whole of the outbound concurrency:
however many events arrive, no more deliveries are in flight than there are
workers, and each target is held to one of them.

The dispatcher claims for every idle worker in one statement rather than each
worker claiming for itself. Claiming is a commit, and a database under load
makes every committer wait its turn — workers claiming one row each spent more
time queueing for commits than sending anything.
*/
func (s *Service) Run(ctx context.Context) {
	workers := int(s.setting(ctx, settings.DeliveryWorkers, 4))

	jobs := make(chan dbgen.WebhookDelivery)
	finished := make(chan struct{}, workers)

	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for job := range jobs {
				/*
					Finished whatever happens to ctx now. A shutdown arriving
					mid-attempt still records how the attempt went, rather than
					leaving a claimed row for the sweep to find minutes later.
				*/
				s.attempt(context.WithoutCancel(ctx), job)
				finished <- struct{}{}
			}
		})
	}
	group.Go(func() { s.sweep(ctx) })

	s.dispatch(ctx, jobs, finished, workers)
	close(jobs)
	group.Wait()
}

/*
dispatch claims work for idle workers until ctx ends.

Paced by DeliveriesPerSecond as well as bounded by the workers: the pace is
what keeps the pool from taking the database's commits away from people
posting, since prompt receivers free a worker almost as soon as it starts.
Read on every claim, so a change takes effect without a restart.
*/
func (s *Service) dispatch(ctx context.Context, jobs chan<- dbgen.WebhookDelivery, finished <-chan struct{}, workers int) {
	idle := workers
	pace := rate.NewLimiter(rate.Inf, workers)
	for {
		// Workers that finished since the last look are idle again.
		for drained := false; !drained; {
			select {
			case <-finished:
				idle++
			default:
				drained = true
			}
		}

		if idle > 0 && ctx.Err() == nil {
			want := idle
			perSecond := s.deliveriesPerSecond(ctx)
			if perSecond > 0 {
				pace.SetLimit(rate.Limit(perSecond))
				// Only as many as the pace has already earned, and none until
				// it has earned one.
				earned := pace.Tokens()
				if earned < 1 {
					wait := time.Duration((1 - earned) / perSecond * float64(time.Second))
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
					continue
				}
				want = min(idle, int(earned))
			}

			claimed, err := s.store.ClaimWebhookDeliveries(ctx, int32(want))
			if err != nil && ctx.Err() == nil {
				s.log.Error("cannot claim webhook deliveries", "error", err)
			}
			if perSecond > 0 {
				pace.AllowN(time.Now(), len(claimed))
			}
			for _, job := range claimed {
				jobs <- job
				idle--
			}
			if len(claimed) > 0 {
				// There may be more, now that these are off the queue.
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-finished:
			idle++
		case <-s.wake:
		case <-time.After(pollInterval):
		}
	}
}

func (s *Service) attempt(ctx context.Context, claimed dbgen.WebhookDelivery) {
	if claimed.ApplicationID != nil {
		s.attemptInvocation(ctx, claimed)
		return
	}
	if claimed.SubscriptionID == nil {
		// Neither a webhook's nor an application's: nothing here knows where
		// to send it.
		s.complete(ctx, claimed.ID, "cancelled")
		return
	}

	subscription, err := s.store.GetWebhookSubscription(ctx, *claimed.SubscriptionID)
	if err != nil {
		s.complete(ctx, claimed.ID, "cancelled")
		return
	}
	if subscription.State != "active" {
		s.complete(ctx, claimed.ID, "cancelled")
		return
	}

	/*
		The owner's access, checked now rather than trusted from when the event
		was queued. Somebody removed from a channel stops being told about it
		from that moment, including about events that happened before they were
		removed and were still waiting to be sent. The subscription and its
		history stay; only delivery stops.
	*/
	if !s.entitled(ctx, subscription, claimed) {
		s.recordAccessLost(ctx, subscription.ID)
		s.complete(ctx, claimed.ID, "cancelled")
		return
	}

	if s.throttled(ctx, subscription, claimed) {
		return
	}

	outcome := s.config.Send(ctx, subscription.Url, claimed.ID.String(), claimed.Kind,
		claimed.Payload, []byte(subscription.Secret))

	/*
		What happens next, decided before anything is written so it can all be
		written at once.

		The run of failures is read from the subscription rather than returned
		by the write: a target has one delivery in flight, so nothing else
		changes the count between the read at the start of this attempt and the
		write at its end.
	*/
	limit := int32(s.setting(ctx, settings.WebhookMaxAttempts, 12))
	threshold := int32(s.setting(ctx, settings.WebhookDisableAfterFailures, 24))
	failures := subscription.ConsecutiveFailures + 1

	state, reason := "delivered", ""
	var next *time.Time
	switch {
	case outcome.Delivered:
	case claimed.Attempts >= limit:
		state, reason = "abandoned", fmt.Sprintf("a delivery failed all %d attempts", limit)
	case failures >= threshold:
		state, reason = "abandoned", fmt.Sprintf("%d attempts in a row failed", failures)
	default:
		state = "pending"
		retry := time.Now().Add(s.backoff(ctx, claimed.Attempts))
		next = &retry
	}

	if err := s.store.FinishWebhookAttempt(ctx, dbgen.FinishWebhookAttemptParams{
		Delivered:      outcome.Delivered,
		SubscriptionID: &subscription.ID,
		AttemptID:      uuid.New(),
		DeliveryID:     claimed.ID,
		Attempt:        claimed.Attempts,
		Outcome:        outcomeOf(outcome),
		StatusCode:     statusOf(outcome),
		Error:          optional(outcome.Error),
		Response:       optional(outcome.Response),
		DurationMs:     int32(outcome.Duration / time.Millisecond),
		State:          state,
		NextAttemptAt:  next,
	}); err != nil {
		s.log.Error("cannot record a webhook attempt", "delivery", claimed.ID, "error", err)
	}

	if state == "abandoned" {
		s.disable(ctx, subscription, claimed.EventID, reason)
	}
}

func outcomeOf(outcome delivery.Outcome) string {
	if outcome.Delivered {
		return "delivered"
	}
	return "failed"
}

func statusOf(outcome delivery.Outcome) *int32 {
	if outcome.Status == 0 {
		return nil
	}
	code := int32(outcome.Status)
	return &code
}

/*
throttled holds a delivery back when its subscription is over its rate, and
reports whether it did.

Held back, never dropped: the delivery goes back in the queue for when the
limit allows it, the attempt it was about to be is given back, and the
subscription records that it happened so its owner sees a limit rather than a
mysterious delay.
*/
func (s *Service) throttled(ctx context.Context, subscription dbgen.WebhookSubscription, claimed dbgen.WebhookDelivery) bool {
	// Read directly rather than through setting(): here zero is meaningful —
	// the operator's "no limit" — rather than a value to replace.
	perMinute := int64(120)
	if s.config.Settings != nil {
		perMinute = s.config.Settings.Int(ctx, settings.WebhookDeliveriesPerMinute)
	}
	if perMinute <= 0 || s.config.Limiter == nil {
		return false
	}

	decision, err := s.config.Limiter.Allow(ctx, "webhook:"+subscription.ID.String(),
		cache.RateLimit{Attempts: int(perMinute), Window: time.Minute})
	if err != nil || decision.Allowed {
		// A limiter that cannot count lets the delivery through: delaying
		// every webhook because a cache is unwell is the worse outcome.
		return false
	}

	if err := s.store.RecordWebhookRateLimited(ctx, subscription.ID); err != nil {
		s.log.Warn("cannot record a webhook being held back", "error", err)
	}
	message := fmt.Sprintf("held back: over %d deliveries a minute", perMinute)
	s.record(ctx, claimed, delivery.Outcome{Error: message}, "rate_limited")

	if err := s.store.PostponeWebhookDelivery(ctx, dbgen.PostponeWebhookDeliveryParams{
		ID:            claimed.ID,
		NextAttemptAt: time.Now().Add(decision.RetryAfter),
	}); err != nil {
		s.log.Error("cannot postpone a webhook delivery", "delivery", claimed.ID, "error", err)
	}
	return true
}

/*
backoff is the delay before a delivery's next attempt: the base doubled for each
attempt already made, up to the cap, with jitter.

The jitter takes up to a quarter off, never adds: receivers that failed together
— one outage, many subscriptions — should not all be retried in the same
instant, and a delay that only ever shrinks from its nominal value keeps each
retry's delay longer than the one before it.
*/
func (s *Service) backoff(ctx context.Context, attempts int32) time.Duration {
	base := time.Duration(s.setting(ctx, settings.WebhookRetryBaseSeconds, 60)) * time.Second
	ceiling := time.Duration(s.setting(ctx, settings.WebhookRetryCapSeconds, 21600)) * time.Second

	delay := base
	for range attempts - 1 {
		delay *= 2
		if delay >= ceiling {
			delay = ceiling
			break
		}
	}
	if delay > ceiling {
		delay = ceiling
	}

	jitter := time.Duration(rand.Int64N(int64(delay)/4 + 1))
	return delay - jitter
}

/*
disable turns a subscription off and tells its owner, once.

Only an active subscription is turned off, so the owner is told once rather
than at every failure queued behind the one that decided it. Everything still
waiting is cancelled: it would only fail too, and the history already says why.
*/
func (s *Service) disable(ctx context.Context, subscription dbgen.WebhookSubscription, eventID *string, reason string) {
	disabled, err := s.store.DisableWebhookSubscription(ctx, dbgen.DisableWebhookSubscriptionParams{
		ID:             subscription.ID,
		DisabledReason: &reason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		s.log.Error("cannot turn off a failing webhook", "subscription", subscription.ID, "error", err)
		return
	}

	if _, err := s.store.CancelPendingWebhookDeliveries(ctx, &subscription.ID); err != nil {
		s.log.Warn("cannot cancel a disabled webhook's queue", "subscription", subscription.ID, "error", err)
	}

	s.log.Warn("turned off a failing webhook",
		"subscription", subscription.ID, "owner", subscription.OwnerID, "reason", reason)
	if s.config.Disabled != nil {
		s.config.Disabled(ctx, disabled, eventID)
	}
}

// record writes an attempt to the history.
func (s *Service) record(ctx context.Context, claimed dbgen.WebhookDelivery, outcome delivery.Outcome, override ...string) {
	result := "failed"
	if outcome.Delivered {
		result = "delivered"
	}
	if len(override) > 0 {
		result = override[0]
	}

	var status *int32
	if outcome.Status != 0 {
		code := int32(outcome.Status)
		status = &code
	}

	if err := s.store.RecordWebhookAttempt(ctx, dbgen.RecordWebhookAttemptParams{
		ID:         uuid.New(),
		DeliveryID: claimed.ID,
		Attempt:    claimed.Attempts,
		Outcome:    result,
		StatusCode: status,
		Error:      optional(outcome.Error),
		Response:   optional(outcome.Response),
		DurationMs: int32(outcome.Duration / time.Millisecond),
	}); err != nil {
		s.log.Warn("cannot record a webhook attempt", "delivery", claimed.ID, "error", err)
	}
}

func (s *Service) complete(ctx context.Context, id uuid.UUID, state string) {
	if err := s.store.CompleteWebhookDelivery(ctx, dbgen.CompleteWebhookDeliveryParams{
		ID:    id,
		State: state,
	}); err != nil {
		s.log.Error("cannot record a webhook delivery's end", "delivery", id, "state", state, "error", err)
	}
}

/*
sweep puts back deliveries a process claimed and never finished, and removes
finished history older than the operator keeps it.

Bounded retention is what keeps a table written on every event from growing for
ever, the same way the audit log's own retention does.
*/
func (s *Service) sweep(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		cutoff := time.Now().Add(-staleAfter)
		if released, err := s.store.ReleaseStaleWebhookDeliveries(ctx, &cutoff); err == nil && released > 0 {
			s.log.Warn("put back webhook deliveries a worker never finished", "count", released)
			s.Wake()
		}

		days := s.setting(ctx, settings.DeliveryHistoryDays, 30)
		if pruned, err := s.store.PruneWebhookDeliveries(ctx, time.Now().AddDate(0, 0, -int(days))); err == nil && pruned > 0 {
			s.log.Info("pruned webhook history", "deliveries", pruned, "olderThanDays", days)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) setting(ctx context.Context, key settings.Key, fallback int64) int64 {
	if s.config.Settings == nil {
		return fallback
	}
	if value := s.config.Settings.Int(ctx, key); value > 0 {
		return value
	}
	return fallback
}

// deliveriesPerSecond is the pace deliveries are started at, or 0 for none.
func (s *Service) deliveriesPerSecond(ctx context.Context) float64 {
	if s.config.Settings == nil {
		return 100
	}
	return float64(s.config.Settings.Int(ctx, settings.DeliveriesPerSecond))
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
