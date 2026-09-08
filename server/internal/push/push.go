/*
Package push delivers notifications to a browser that is not open.

Web push, which is the open mechanism browsers already implement: an operator
generates a key pair once and needs no account with anybody. The push service
in the middle is whichever one the browser chose — Mozilla's, Google's,
Apple's — and none of them can read what is sent, because the payload is
encrypted to keys that never leave the device.

That last point is what makes this acceptable on a server whose premise is
self-hosting. The message passes through infrastructure the operator does not
control, and passes through it as ciphertext.
*/
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
failureLimit is how many consecutive failures retire a subscription.

Some push services fail for a while and recover, so one failure means nothing.
A long run of them means nothing is listening, and continuing to try costs a
request per notification forever.
*/
const failureLimit = 10

// deliveryTimeout bounds one attempt, so a push service that stops answering
// does not hold a notification worker open.
const deliveryTimeout = 10 * time.Second

// ErrNotConfigured is returned when push is asked for on a server with no keys.
var ErrNotConfigured = errors.New("this server has not configured push notifications")

// Keys are the operator's VAPID pair, which identifies this server to a push
// service.
type Keys struct {
	Public  string
	Private string
	// Subject is a mailto: or https: address a push service can use to reach
	// the operator about a problem. Required by the specification.
	Subject string
}

/*
GenerateKeys makes a VAPID pair for an operator who has none.

Offered so that enabling push needs nothing but this server: a feature
requiring somebody to visit a website and paste a key back is one that does not
work on a server with no outbound access.
*/
func GenerateKeys() (Keys, error) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return Keys{}, fmt.Errorf("push: generating keys: %w", err)
	}
	return Keys{Public: public, Private: private}, nil
}

/*
Payload is what a device is told.

Deliberately small. It travels through a service the operator does not run, and
everything in it is visible to whoever holds the device — on a lock screen, to
anybody who can see the screen.
*/
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Room is where activating the notification should go.
	Room string `json:"room,omitempty"`
	// Event names the message, so opening it can land on the right one.
	Event string `json:"event,omitempty"`
}

// Service sends push notifications.
type Service struct {
	queries *dbgen.Queries
	log     *slog.Logger
	keys    Keys
}

func NewService(pool *db.Pool, keys Keys, log *slog.Logger) *Service {
	return &Service{queries: dbgen.New(pool), log: log, keys: keys}
}

/*
Enabled reports whether an operator has configured push.

Off until they generate keys, and off is a working state: notifications are
stored and shown in the application regardless. Push is how somebody hears
about one while the application is closed, not how it exists.
*/
func (s *Service) Enabled() bool {
	return s != nil && s.keys.Public != "" && s.keys.Private != ""
}

// PublicKey is what a browser needs in order to subscribe.
func (s *Service) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.keys.Public
}

// Subscribe records where a device can be reached.
func (s *Service) Subscribe(
	ctx context.Context,
	userID uuid.UUID,
	endpoint, p256dh, auth, userAgent string,
) (dbgen.PushSubscription, error) {
	row, err := s.queries.SavePushSubscription(ctx, dbgen.SavePushSubscriptionParams{
		ID:        uuid.New(),
		UserID:    userID,
		Endpoint:  endpoint,
		P256dhKey: p256dh,
		AuthKey:   auth,
		UserAgent: userAgent,
	})
	if err != nil {
		return dbgen.PushSubscription{}, fmt.Errorf("push: saving a subscription: %w", err)
	}
	return row, nil
}

// Subscriptions lists a person's devices, so they can see and revoke them.
func (s *Service) Subscriptions(
	ctx context.Context,
	userID uuid.UUID,
) ([]dbgen.PushSubscription, error) {
	rows, err := s.queries.ListPushSubscriptions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("push: listing subscriptions: %w", err)
	}
	return rows, nil
}

// Unsubscribe removes one device, scoped to its owner.
func (s *Service) Unsubscribe(ctx context.Context, id, userID uuid.UUID) error {
	if _, err := s.queries.DeletePushSubscription(ctx, dbgen.DeletePushSubscriptionParams{
		ID:     id,
		UserID: userID,
	}); err != nil {
		return fmt.Errorf("push: removing a subscription: %w", err)
	}
	return nil
}

/*
Send delivers a payload to every device a person has subscribed.

Failures are per device. One phone whose subscription has expired must not stop
the notification reaching a laptop that is still listening, so each is tried
and the outcome recorded against that subscription alone.
*/
func (s *Service) Send(ctx context.Context, userID uuid.UUID, payload Payload) error {
	if !s.Enabled() {
		return nil
	}

	subscriptions, err := s.Subscriptions(ctx, userID)
	if err != nil {
		return err
	}
	if len(subscriptions) == 0 {
		return nil
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("push: encoding a payload: %w", err)
	}

	for _, subscription := range subscriptions {
		s.deliver(ctx, subscription, encoded)
	}
	return nil
}

/*
deliver sends to one device and records what happened.

A subscription the push service reports as gone is removed at once rather than
counted as a failure: the service has said definitively that nothing is there,
and retrying it ten more times would be ignoring an answer.
*/
func (s *Service) deliver(
	ctx context.Context,
	subscription dbgen.PushSubscription,
	payload []byte,
) {
	attempt, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()

	response, err := webpush.SendNotificationWithContext(attempt, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys: webpush.Keys{
			P256dh: subscription.P256dhKey,
			Auth:   subscription.AuthKey,
		},
	}, &webpush.Options{
		Subscriber:      s.keys.Subject,
		VAPIDPublicKey:  s.keys.Public,
		VAPIDPrivateKey: s.keys.Private,
		// A notification nobody has read in a day is not worth waking a phone
		// for, and the application shows it whenever they next open it.
		TTL: int(24 * time.Hour / time.Second),
	})

	if err != nil {
		// The endpoint is not logged whole: anybody holding one can send to
		// that device, so it is a credential of sorts.
		s.log.Warn("push delivery failed",
			"service", endpointHost(subscription.Endpoint), "error", err)
		s.recordFailure(ctx, subscription.Endpoint)
		return
	}
	defer response.Body.Close()

	if Gone(response.StatusCode) {
		/*
			The push service says this subscription no longer exists — the
			browser was uninstalled, the permission revoked, the endpoint
			expired. Removed rather than retried: it will never work again, and
			keeping it means a request per notification forever.
		*/
		if err := s.queries.DeletePushSubscriptionByEndpoint(ctx, subscription.Endpoint); err != nil {
			s.log.Error("cannot remove an expired subscription", "error", err)
		}
		return
	}

	if response.StatusCode >= 400 {
		s.recordFailure(ctx, subscription.Endpoint)
		return
	}

	if err := s.queries.RecordPushSuccess(ctx, subscription.Endpoint); err != nil {
		s.log.Error("cannot record a push success", "error", err)
	}
}

func (s *Service) recordFailure(ctx context.Context, endpoint string) {
	if err := s.queries.RecordPushFailure(ctx, endpoint); err != nil {
		s.log.Error("cannot record a push failure", "error", err)
	}
}

/*
Gone reports whether a status means the subscription will never work again.

404 is the endpoint not existing and 410 is it having been withdrawn. Both are
final answers, distinct from a service that is merely unwell — which is why a
500 is counted as a failure and retried rather than treated as a removal.
*/
func Gone(status int) bool {
	return status == http.StatusNotFound || status == http.StatusGone
}

/*
Prune removes subscriptions that have failed too many times in a row.

The push service never told us they were gone; they simply stopped working.
Without this they are retried on every notification for as long as the account
exists.
*/
func (s *Service) Prune(ctx context.Context) (int64, error) {
	removed, err := s.queries.PrunePushSubscriptions(ctx, failureLimit)
	if err != nil {
		return 0, fmt.Errorf("push: pruning subscriptions: %w", err)
	}
	if removed > 0 {
		s.log.Info("removed push subscriptions that stopped working", "count", removed)
	}
	return removed, nil
}

/*
endpointHost is the push service's host, for logging.

The full endpoint is a credential of sorts — anybody holding it can send to
that device — so it is never written to a log whole.
*/
func endpointHost(endpoint string) string {
	for i := len("https://"); i < len(endpoint); i++ {
		if endpoint[i] == '/' {
			return endpoint[:i]
		}
	}
	return endpoint
}
