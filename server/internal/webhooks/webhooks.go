/*
Package webhooks tells somebody's endpoint about events as they happen.

A subscription names what it listens to — a channel, a project, or the whole
server — some event types and an endpoint. When a matching event is stored, a
delivery is queued in Postgres; a small pool of workers claims due deliveries,
signs each, sends it, and records what happened. A delivery that fails is
retried later with a growing delay, up to a documented limit; a subscription
that keeps failing is turned off and its owner told.

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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
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
	// ErrWrongScope reports an event type asked for where it cannot happen —
	// an issue's events on a channel — or a room that is not what the
	// subscription says it is.
	ErrWrongScope = errors.New("that event cannot happen there")
	// ErrNotPermitted reports a type whose events the owner does not hold the
	// permission to read.
	ErrNotPermitted = errors.New("you do not hold the permission those events need")
)

// MaxLabelLength bounds a subscription's label.
const MaxLabelLength = 80

// Access answers whether somebody can currently read a room.
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
	OwnerID uuid.UUID
	// Scope is ScopeChannel, ScopeProject or ScopeServer; empty is a channel.
	Scope string
	// RoomID is the channel, or the project's stream. Unset for the server.
	RoomID     uuid.UUID
	Label      string
	URL        string
	EventTypes []string
}

// Source is what a room belongs to, as far as a delivery needs to say.
type Source struct {
	// Kind is the room's kind: "channel", "project", "issue-discussion"...
	Kind    string
	Project *Ref
	Issue   *Ref
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

	scope := request.Scope
	if scope == "" {
		scope = ScopeChannel
	}
	if scope != ScopeChannel && scope != ScopeProject && scope != ScopeServer {
		return dbgen.WebhookSubscription{}, ErrWrongScope
	}

	types, err := checkTypes(request.EventTypes, scope)
	if err != nil {
		return dbgen.WebhookSubscription{}, err
	}
	for _, kind := range types {
		definition, _ := Lookup(kind)
		if definition.Permission != "" && !s.holds(ctx, request.OwnerID, definition.Permission) {
			return dbgen.WebhookSubscription{}, fmt.Errorf("%w: %s needs %s",
				ErrNotPermitted, kind, definition.Permission)
		}
	}

	var roomID *uuid.UUID
	if scope != ScopeServer {
		// A room the owner cannot read is refused, and refused as missing:
		// the answer to "can I subscribe to this" must not confirm it exists.
		if !s.config.CanRead(ctx, request.RoomID, request.OwnerID) {
			return dbgen.WebhookSubscription{}, ErrCannotRead
		}
		// Only then is what it is checked, which says nothing to somebody
		// who could not read it.
		if isProject := s.describe(ctx, request.RoomID).Kind == "project"; isProject != (scope == ScopeProject) {
			return dbgen.WebhookSubscription{}, ErrWrongScope
		}
		roomID = &request.RoomID
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
		RoomID:         roomID,
		Scope:          scope,
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

// checkTypes refuses unknown types and ones that cannot happen in the scope,
// and removes repeats.
func checkTypes(requested []string, scope string) ([]string, error) {
	if len(requested) == 0 {
		return nil, ErrNoEventTypes
	}
	chosen := make([]string, 0, len(requested))
	for _, kind := range requested {
		kind = strings.TrimSpace(kind)
		definition, known := Lookup(kind)
		if !known {
			return nil, fmt.Errorf("%w: %q", ErrUnknownEventType, kind)
		}
		if !slices.Contains(definition.Scopes, scope) {
			return nil, fmt.Errorf("%w: %s is not an event of a %s", ErrWrongScope, kind, scope)
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

Three kinds of subscription can want one event: those on its room, those on the
project the room belongs to (for a comment in an issue's discussion), and those
on the whole server. And one event can go by more than one name — a move into a
finishing state is issue.state_changed and issue.closed — each delivered to
whoever asked for that name.

A subscription whose owner cannot read the room the event happened in is
skipped here, and again at delivery, since access can be lost between the two.
*/
func (s *Service) Enqueue(ctx context.Context, event dbgen.Event) {
	source := s.describe(ctx, event.RoomID)
	kinds := append([]string{event.Type}, derived(event, source)...)

	candidates, err := s.store.ListActiveWebhookSubscriptionsForRoom(ctx, &event.RoomID)
	if err != nil {
		s.log.Error("cannot find webhooks for an event", "eventId", event.ID, "error", err)
		return
	}
	if source.Project != nil && slices.Contains(kinds, TypeIssueCommented) {
		if projectID, err := uuid.Parse(source.Project.ID); err == nil {
			onProject, err := s.store.ListActiveWebhookSubscriptionsForRoom(ctx, &projectID)
			if err != nil {
				s.log.Error("cannot find a project's webhooks", "eventId", event.ID, "error", err)
			}
			candidates = append(candidates, onProject...)
		}
	}
	for _, kind := range kinds {
		onServer, err := s.store.ListActiveServerWebhookSubscriptions(ctx, kind)
		if err != nil {
			s.log.Error("cannot find server webhooks for an event", "eventId", event.ID, "error", err)
			continue
		}
		for _, subscription := range onServer {
			if !slices.ContainsFunc(candidates, func(c dbgen.WebhookSubscription) bool { return c.ID == subscription.ID }) {
				candidates = append(candidates, subscription)
			}
		}
	}

	readers := map[uuid.UUID]bool{}
	queued := false
	for _, subscription := range candidates {
		wanted := slices.DeleteFunc(slices.Clone(kinds), func(kind string) bool {
			return !slices.Contains(subscription.EventTypes, kind)
		})
		if len(wanted) == 0 {
			continue
		}

		allowed, asked := readers[subscription.OwnerID]
		if !asked {
			allowed = s.config.CanRead(ctx, event.RoomID, subscription.OwnerID)
			readers[subscription.OwnerID] = allowed
		}
		if !allowed {
			// A server subscription hears the rooms its owner can read; one
			// it cannot read is somewhere else, not access lost.
			if subscription.Scope != ScopeServer {
				s.recordAccessLost(ctx, subscription.ID)
			}
			continue
		}

		eventID := event.ID
		for _, kind := range wanted {
			if s.queue(ctx, subscription, Input{
				Server: s.config.Server, Event: event, Kind: kind,
				Project: source.Project, Issue: source.Issue,
			}, &eventID) {
				queued = true
			}
		}
	}

	if queued {
		s.Wake()
	}
}

/*
EnqueueServer queues an event that belongs to no room — a new account, a change
of role — for every server subscription that asked for it and whose owner holds
the permission its type needs.

The event is given an identifier of its own, the same for every subscription,
so a receiver hearing it through two of them can tell it is one event.
*/
func (s *Service) EnqueueServer(ctx context.Context, kind, sender string, content any) {
	definition, known := Lookup(kind)
	if !known || definition.Permission == "" {
		s.log.Error("not an event that belongs to no room", "type", kind)
		return
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		s.log.Error("cannot encode a server event", "type", kind, "error", err)
		return
	}
	subscriptions, err := s.store.ListActiveServerWebhookSubscriptions(ctx, kind)
	if err != nil {
		s.log.Error("cannot find server webhooks", "type", kind, "error", err)
		return
	}

	event := dbgen.Event{
		ID:            "$" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		Type:          kind,
		SenderAddress: sender,
		Content:       encoded,
		CreatedAt:     time.Now().UnixMilli(),
	}
	queued := false
	for _, subscription := range subscriptions {
		if !s.holds(ctx, subscription.OwnerID, definition.Permission) {
			s.recordAccessLost(ctx, subscription.ID)
			continue
		}
		if s.queue(ctx, subscription, Input{Server: s.config.Server, Event: event, Roomless: true}, nil) {
			queued = true
		}
	}
	if queued {
		s.Wake()
	}
}

// queue renders one delivery for one subscription and puts it in the queue,
// reporting whether it did.
func (s *Service) queue(ctx context.Context, subscription dbgen.WebhookSubscription, input Input, eventID *string) bool {
	id := uuid.New()
	input.Delivery = id.String()
	input.Subscription = subscription.ID.String()
	payload, err := Render(subscription.PayloadVersion, input)
	if err != nil {
		s.log.Error("cannot render a webhook payload",
			"subscription", subscription.ID, "version", subscription.PayloadVersion, "error", err)
		return false
	}

	kind := input.Kind
	if kind == "" {
		kind = input.Event.Type
	}
	if _, err := s.store.EnqueueWebhookDelivery(ctx, dbgen.EnqueueWebhookDeliveryParams{
		ID:             id,
		Target:         Target(subscription.ID),
		SubscriptionID: &subscription.ID,
		EventID:        eventID,
		Kind:           kind,
		Payload:        payload,
	}); err != nil {
		s.log.Error("cannot queue a webhook delivery", "subscription", subscription.ID, "error", err)
		return false
	}
	return true
}

/*
derived is the other names an event goes by.

A comment is a message in an issue's discussion. Closing an issue is moving it
into a finishing state from one that is not, and reopening the reverse — told
apart from a move between two open states by what the move recorded.
*/
func derived(event dbgen.Event, source Source) []string {
	switch {
	case event.Type == string(events.TypeMessageSent) && source.Issue != nil:
		return []string{TypeIssueCommented}
	case event.Type == string(events.TypeIssueStateChanged):
		var move struct {
			Completed    bool `json:"completed"`
			WasCompleted bool `json:"wasCompleted"`
		}
		if json.Unmarshal(event.Content, &move) != nil {
			return nil
		}
		switch {
		case move.Completed && !move.WasCompleted:
			return []string{TypeIssueClosed}
		case !move.Completed && move.WasCompleted:
			return []string{TypeIssueReopened}
		}
	}
	return nil
}

// describe asks what a room belongs to; a server that cannot say treats every
// room as a channel.
func (s *Service) describe(ctx context.Context, roomID uuid.UUID) Source {
	if s.config.Describe == nil {
		return Source{Kind: "channel"}
	}
	return s.config.Describe(ctx, roomID)
}

// holds asks whether somebody holds a server permission; a server that cannot
// say holds that nobody does.
func (s *Service) holds(ctx context.Context, userID uuid.UUID, permission string) bool {
	return s.config.Holds != nil && s.config.Holds(ctx, userID, permission)
}

/*
entitled decides, at the moment of sending, whether a delivery's owner may still
be told about it: they can read the room it happened in, or — for an event in
no room — they hold the permission its type needs.

Read from the payload's room rather than the subscription's, because they
differ for a comment, which happens in a discussion and is delivered to the
project's subscribers, and for a server subscription, which has no room. Every
version of the payload carries the room as room.id; one that did not would
fail here closed, delivering nothing.
*/
func (s *Service) entitled(ctx context.Context, subscription dbgen.WebhookSubscription, claimed dbgen.WebhookDelivery) bool {
	var payload struct {
		Room *struct {
			ID uuid.UUID `json:"id"`
		} `json:"room"`
	}
	if err := json.Unmarshal(claimed.Payload, &payload); err != nil {
		return false
	}
	if payload.Room != nil {
		return s.config.CanRead(ctx, payload.Room.ID, subscription.OwnerID)
	}
	definition, known := Lookup(claimed.Kind)
	return known && definition.Permission != "" && s.holds(ctx, subscription.OwnerID, definition.Permission)
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
