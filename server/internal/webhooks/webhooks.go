/*
Package webhooks tells somebody's endpoint about events as they happen.

A subscription names a channel, some event types and an endpoint. When a
matching event is stored, a delivery is queued in Postgres; a small pool of
workers claims due deliveries, signs each, sends it, and records what happened.
A delivery that fails is retried later with a growing delay, up to a documented
limit; a subscription that keeps failing is turned off and its owner told.

Everything that could let one receiver affect anything else is bounded. Queuing
happens off the request that stored the event, so posting a message never waits
on a webhook. Each subscription has at most one delivery in flight, so a slow
receiver holds one worker and never the pool. The pool itself is the ceiling on
outbound connections. And the queue is a table an operator can read with psql,
because a second service to run and back up is exactly what this server
declines to require.
*/
package webhooks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/events"
)

var (
	// ErrNotFound reports a subscription that does not exist or is not the
	// caller's. One error for both, so a guess learns nothing.
	ErrNotFound = errors.New("no such webhook")
	// ErrUnknownEventType reports a type a subscription cannot ask for.
	ErrUnknownEventType = errors.New("that is not an event type a webhook can receive")
	// ErrNoEventTypes reports a subscription to nothing.
	ErrNoEventTypes = errors.New("a webhook needs at least one event type")
	// ErrLabelRequired reports a subscription nobody could tell apart from
	// another in a list.
	ErrLabelRequired = errors.New("a webhook needs a label saying what it is for")
	// ErrCannotRead reports a subscription to a channel its owner cannot
	// read. Reported as the channel being missing, as everywhere else.
	ErrCannotRead = errors.New("no such room")
)

/*
EventTypes are what a subscription may ask for: everything somebody able to read
a channel sees happen in it. Anything else — a type this server does not have,
or one only an administrator sees — is refused, so a subscription can never be a
way to learn what its owner could not already read.
*/
var EventTypes = []string{
	string(events.TypeMessageSent),
	string(events.TypeMessageEdited),
	string(events.TypeMessageDeleted),
	string(events.TypeMessageUpdated),
	string(events.TypeActionPressed),
	string(events.TypeMemberJoined),
	string(events.TypeMemberLeft),
	string(events.TypeMemberRemoved),
	string(events.TypeRoomRenamed),
	string(events.TypeRoomArchived),
	string(events.TypeRoomRestored),
}

// MaxLabelLength bounds a subscription's label.
const MaxLabelLength = 80

// Access answers whether somebody can currently read a channel.
type Access func(ctx context.Context, roomID, userID uuid.UUID) bool

// Store is the database surface this package needs.
type Store interface {
	dbgen.Querier
}

// Service manages subscriptions and their deliveries.
type Service struct {
	store  Store
	config Config
	log    *slog.Logger

	// wake nudges an idle worker when something is queued, so a delivery goes
	// out now rather than at the next poll.
	wake chan struct{}
}

// New builds the service. Nothing is delivered until Run is called.
func New(store Store, config Config, log *slog.Logger) *Service {
	return &Service{store: store, config: config, log: log, wake: make(chan struct{}, 1)}
}

// NewSubscription is what somebody asks for.
type NewSubscription struct {
	OwnerID    uuid.UUID
	RoomID     uuid.UUID
	Label      string
	URL        string
	EventTypes []string
}

/*
Subscribe creates a subscription, returning it with its secret.

The secret is generated here rather than chosen, so it always has the entropy a
signing key needs, and it is returned this once: the owner copies it into their
receiver now, and a lost one is replaced by making a new subscription.
*/
func (s *Service) Subscribe(ctx context.Context, request NewSubscription) (dbgen.WebhookSubscription, error) {
	label := strings.TrimSpace(request.Label)
	if label == "" {
		return dbgen.WebhookSubscription{}, ErrLabelRequired
	}
	if utf8.RuneCountInString(label) > MaxLabelLength {
		return dbgen.WebhookSubscription{}, fmt.Errorf("%w: at most %d characters", ErrLabelRequired, MaxLabelLength)
	}

	types, err := checkTypes(request.EventTypes)
	if err != nil {
		return dbgen.WebhookSubscription{}, err
	}

	// A channel the owner cannot read is refused, and refused as missing:
	// the answer to "can I subscribe to this" must not confirm it exists.
	if !s.config.CanRead(ctx, request.RoomID, request.OwnerID) {
		return dbgen.WebhookSubscription{}, ErrCannotRead
	}

	if err := delivery.CheckDestination(ctx, request.URL, s.config.AllowPrivate(ctx)); err != nil {
		return dbgen.WebhookSubscription{}, err
	}

	secret, err := newSecret()
	if err != nil {
		return dbgen.WebhookSubscription{}, err
	}

	subscription, err := s.store.CreateWebhookSubscription(ctx, dbgen.CreateWebhookSubscriptionParams{
		ID:             uuid.New(),
		OwnerID:        request.OwnerID,
		RoomID:         request.RoomID,
		Label:          label,
		Url:            strings.TrimSpace(request.URL),
		Secret:         secret,
		EventTypes:     types,
		PayloadVersion: CurrentVersion(),
	})
	if err != nil {
		return dbgen.WebhookSubscription{}, fmt.Errorf("create the subscription: %w", err)
	}
	return subscription, nil
}

// checkTypes refuses unknown types and removes repeats.
func checkTypes(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return nil, ErrNoEventTypes
	}
	chosen := make([]string, 0, len(requested))
	for _, kind := range requested {
		kind = strings.TrimSpace(kind)
		if !slices.Contains(EventTypes, kind) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownEventType, kind)
		}
		if !slices.Contains(chosen, kind) {
			chosen = append(chosen, kind)
		}
	}
	return chosen, nil
}

// List returns somebody's subscriptions.
func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]dbgen.WebhookSubscription, error) {
	return s.store.ListWebhookSubscriptionsForOwner(ctx, ownerID)
}

// Owned returns one subscription, if it is the caller's.
func (s *Service) Owned(ctx context.Context, ownerID, id uuid.UUID) (dbgen.WebhookSubscription, error) {
	subscription, err := s.store.GetWebhookSubscription(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && subscription.OwnerID != ownerID) {
		return dbgen.WebhookSubscription{}, ErrNotFound
	}
	return subscription, err
}

// History is a subscription's recent deliveries and attempts.
type History struct {
	Deliveries []dbgen.WebhookDelivery
	Attempts   []dbgen.ListWebhookHistoryRow
}

// History returns what has been tried recently for a subscription.
func (s *Service) History(ctx context.Context, subscriptionID uuid.UUID, limit int32) (History, error) {
	deliveries, err := s.store.ListWebhookDeliveries(ctx, dbgen.ListWebhookDeliveriesParams{
		SubscriptionID: &subscriptionID,
		Limit:          limit,
	})
	if err != nil {
		return History{}, fmt.Errorf("list deliveries: %w", err)
	}
	attempts, err := s.store.ListWebhookHistory(ctx, dbgen.ListWebhookHistoryParams{
		SubscriptionID: &subscriptionID,
		Limit:          limit,
	})
	if err != nil {
		return History{}, fmt.Errorf("list attempts: %w", err)
	}
	return History{Deliveries: deliveries, Attempts: attempts}, nil
}

// Unsubscribe removes a subscription and its history.
func (s *Service) Unsubscribe(ctx context.Context, ownerID, id uuid.UUID) (dbgen.WebhookSubscription, error) {
	removed, err := s.store.DeleteWebhookSubscription(ctx, dbgen.DeleteWebhookSubscriptionParams{
		ID:      id,
		OwnerID: ownerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.WebhookSubscription{}, ErrNotFound
	}
	return removed, err
}

// Enable turns a disabled subscription back on.
func (s *Service) Enable(ctx context.Context, ownerID, id uuid.UUID) (dbgen.WebhookSubscription, error) {
	enabled, err := s.store.EnableWebhookSubscription(ctx, dbgen.EnableWebhookSubscriptionParams{
		ID:      id,
		OwnerID: ownerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.WebhookSubscription{}, ErrNotFound
	}
	return enabled, err
}

/*
Enqueue queues an event for every subscription that asked for it.

Called after the event is stored and off the request that stored it: a message
is posted whether or not a single webhook could be queued, because failing a
post over a webhook would make every receiver a way to break the channel.

A subscription whose owner can no longer read the channel is skipped here, and
again at delivery, since access can be lost between the two.
*/
func (s *Service) Enqueue(ctx context.Context, event dbgen.Event) {
	subscriptions, err := s.store.ListActiveWebhookSubscriptionsForRoom(ctx, event.RoomID)
	if err != nil {
		s.log.Error("cannot find webhooks for an event", "eventId", event.ID, "error", err)
		return
	}

	queued := false
	for _, subscription := range subscriptions {
		if !slices.Contains(subscription.EventTypes, event.Type) {
			continue
		}
		if !s.config.CanRead(ctx, subscription.RoomID, subscription.OwnerID) {
			s.recordAccessLost(ctx, subscription.ID)
			continue
		}

		id := uuid.New()
		payload, err := Render(subscription.PayloadVersion, Input{
			Delivery:     id.String(),
			Subscription: subscription.ID.String(),
			Server:       s.config.Server,
			Event:        event,
		})
		if err != nil {
			s.log.Error("cannot render a webhook payload",
				"subscription", subscription.ID, "version", subscription.PayloadVersion, "error", err)
			continue
		}

		eventID := event.ID
		if _, err := s.store.EnqueueWebhookDelivery(ctx, dbgen.EnqueueWebhookDeliveryParams{
			ID:             id,
			Target:         Target(subscription.ID),
			SubscriptionID: &subscription.ID,
			EventID:        &eventID,
			Kind:           event.Type,
			Payload:        payload,
		}); err != nil {
			s.log.Error("cannot queue a webhook delivery", "subscription", subscription.ID, "error", err)
			continue
		}
		queued = true
	}

	if queued {
		s.Wake()
	}
}

// Target names a subscription as a destination, which is what the queue holds
// to one delivery in flight at a time.
func Target(subscriptionID uuid.UUID) string {
	return "webhook:" + subscriptionID.String()
}

// Wake tells an idle worker there is something to do.
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) recordAccessLost(ctx context.Context, id uuid.UUID) {
	if err := s.store.RecordWebhookAccessLost(ctx, id); err != nil {
		s.log.Warn("cannot record a webhook's lost access", "subscription", id, "error", err)
	}
}

// secretPrefix marks a webhook secret, as tdc_ marks a token.
const secretPrefix = "whsec_"

func newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate a secret: %w", err)
	}
	return secretPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
