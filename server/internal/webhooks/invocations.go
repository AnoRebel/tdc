package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Invocations: a press on an application's message, sent to the application.

An application that holds a live connection hears a press as it happens. One
that cannot — a function that runs only when called, a service behind a load
balancer that drops long connections — registers an address instead, and each
press on one of its messages is delivered there: signed as a webhook's delivery
is, retried as one is, and held to one in flight as one is.

Who pressed was already checked against what the control requires before the
press was recorded at all. Nothing here decides who may cause an invocation;
by the time one is queued, that question has been answered.
*/

// ErrNoSuchApplication reports an invocation address set on an application
// that is not approved.
var ErrNoSuchApplication = errors.New("no such application")

// ApplicationTarget names an application as a destination.
func ApplicationTarget(applicationID uuid.UUID) string {
	return "application:" + applicationID.String()
}

/*
SetInvocation sets where an application is told about presses, returning the
secret its deliveries will be signed with — generated here, shown this once.

An empty address stops invocations; anything already queued is then cancelled
at its next attempt rather than sent to an address that was withdrawn.
*/
func (s *Service) SetInvocation(ctx context.Context, applicationID uuid.UUID, url string) (dbgen.Application, error) {
	secret := ""
	if url != "" {
		if err := delivery.CheckDestination(ctx, url, s.config.AllowPrivate(ctx)); err != nil {
			return dbgen.Application{}, err
		}
		generated, err := newSecret()
		if err != nil {
			return dbgen.Application{}, err
		}
		secret = generated
	}

	application, err := s.store.SetApplicationInvocation(ctx, dbgen.SetApplicationInvocationParams{
		ID: applicationID, InvocationUrl: url, InvocationSecret: secret,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Application{}, ErrNoSuchApplication
	}
	if err != nil {
		return dbgen.Application{}, fmt.Errorf("set invocation address: %w", err)
	}
	return application, nil
}

// invocationV1 is what an application's address receives, as documented in
// docs/webhooks-and-apps.md. The same shape as a webhook's delivery, naming the
// application where a delivery names its subscription.
type invocationV1 struct {
	Version     int     `json:"version"`
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	OccurredAt  string  `json:"occurredAt"`
	Application string  `json:"application"`
	Server      string  `json:"server"`
	Room        roomV1  `json:"room"`
	Event       eventV1 `json:"event"`
}

/*
Invoke queues a press for the application whose message was pressed.

Nothing is queued for an application with no address; it hears presses on its
live connection, as it always could.
*/
func (s *Service) Invoke(ctx context.Context, application dbgen.Application, press dbgen.Event) error {
	if application.State != "approved" || application.InvocationUrl == "" {
		return nil
	}

	id := uuid.New()
	payload, err := json.Marshal(invocationV1{
		Version:     1,
		ID:          id.String(),
		Type:        press.Type,
		OccurredAt:  time.UnixMilli(press.CreatedAt).UTC().Format(time.RFC3339Nano),
		Application: application.ID.String(),
		Server:      s.config.Server,
		Room:        roomV1{ID: press.RoomID.String(), Address: press.RoomAddress},
		Event: eventV1{
			ID:        press.ID,
			Type:      press.Type,
			Sequence:  press.Sequence,
			Sender:    press.SenderAddress,
			Content:   json.RawMessage(press.Content),
			CreatedAt: press.CreatedAt,
		},
	})
	if err != nil {
		return fmt.Errorf("render an invocation: %w", err)
	}

	eventID := press.ID
	if _, err := s.store.EnqueueInvocation(ctx, dbgen.EnqueueInvocationParams{
		ID:            id,
		Target:        ApplicationTarget(application.ID),
		ApplicationID: &application.ID,
		EventID:       &eventID,
		Kind:          press.Type,
		Payload:       payload,
	}); err != nil {
		return fmt.Errorf("queue an invocation: %w", err)
	}
	s.Wake()
	return nil
}

/*
attemptInvocation sends one invocation, deciding afresh whether it still should.

An application removed, restricted out of the channel, or with its address
withdrawn since the press was queued is not sent anything: the invocation is
cancelled, which its history shows, and nothing reaches an address that no
longer speaks for it.

A failure is retried exactly as a webhook's is. What does not happen is the
application being switched off after a run of failures: it is installed by an
administrator's decision, and only an administrator's decision removes it.
*/
func (s *Service) attemptInvocation(ctx context.Context, claimed dbgen.WebhookDelivery) {
	application, err := s.store.GetApplication(ctx, *claimed.ApplicationID)
	if err != nil || application.State != "approved" || application.InvocationUrl == "" ||
		!s.actsIn(ctx, application.ID, claimed.Payload) {
		s.complete(ctx, claimed.ID, "cancelled")
		return
	}

	outcome := s.config.Send(ctx, application.InvocationUrl, claimed.ID.String(), claimed.Kind,
		claimed.Payload, []byte(application.InvocationSecret))

	limit := int32(s.setting(ctx, settings.WebhookMaxAttempts, 12))
	state := "delivered"
	var next *time.Time
	switch {
	case outcome.Delivered:
	case claimed.Attempts >= limit:
		state = "abandoned"
	default:
		state = "pending"
		retry := time.Now().Add(s.backoff(ctx, claimed.Attempts))
		next = &retry
	}

	if err := s.store.FinishWebhookAttempt(ctx, dbgen.FinishWebhookAttemptParams{
		Delivered:     outcome.Delivered,
		AttemptID:     uuid.New(),
		DeliveryID:    claimed.ID,
		Attempt:       claimed.Attempts,
		Outcome:       outcomeOf(outcome),
		StatusCode:    statusOf(outcome),
		Error:         optional(outcome.Error),
		Response:      optional(outcome.Response),
		DurationMs:    int32(outcome.Duration / time.Millisecond),
		State:         state,
		NextAttemptAt: next,
	}); err != nil {
		s.log.Error("cannot record an invocation attempt", "delivery", claimed.ID, "error", err)
	}
	if state == "abandoned" {
		s.log.Warn("gave up on an invocation", "application", application.ID, "delivery", claimed.ID,
			"attempts", claimed.Attempts)
	}
}

// actsIn is whether the channel an invocation came from is still one of the
// application's.
func (s *Service) actsIn(ctx context.Context, applicationID uuid.UUID, payload []byte) bool {
	var invocation struct {
		Room struct {
			ID uuid.UUID `json:"id"`
		} `json:"room"`
	}
	if err := json.Unmarshal(payload, &invocation); err != nil {
		return false
	}
	channels, err := s.store.ListApplicationChannels(ctx, applicationID)
	if err != nil {
		return false
	}
	return slices.Contains(channels, invocation.Room.ID)
}
