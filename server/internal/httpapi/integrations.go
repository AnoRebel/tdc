package httpapi

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/applications"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/settings"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
)

/*
What programs acting on this server need the HTTP layer to hold.

Built on first use rather than at construction, so a server nobody has pointed
a webhook at carries none of it, and so adding it did not change how every test
and the command build a server.
*/
type integrationsState struct {
	once         sync.Once
	signer       *delivery.Signer
	client       *delivery.Client
	webhooks     *webhooks.Service
	applications *applications.Service
}

/*
maxDeliveryTimeout is the ceiling the outbound client is built with.

The operator's setting is applied per attempt, beneath it, so changing the
setting takes effect without a restart; this only stops a mistyped setting from
holding a worker for an hour.
*/
const maxDeliveryTimeout = 30 * time.Second

// buildIntegrations constructs everything once.
func (s *Server) buildIntegrations() {
	s.integrations.once.Do(func() {
		if s.rooms != nil {
			s.integrations.signer = delivery.NewSigner(s.rooms.Keyring())
		}
		s.integrations.client = delivery.NewClient(maxDeliveryTimeout, func() bool {
			return s.privateDeliveriesAllowed(context.Background())
		})

		// The queue needs the database it lives in and the rooms it reads
		// access from; a server built without either has no webhooks.
		if s.db == nil || s.rooms == nil {
			return
		}
		var policy webhooks.Settings
		if s.settings != nil {
			policy = s.settings
		}
		s.integrations.webhooks = webhooks.New(s.queries, webhooks.Config{
			Server:       s.serverName(),
			CanRead:      s.canRead,
			AllowPrivate: s.privateDeliveriesAllowed,
			Send: func(ctx context.Context, endpoint, id, kind string, body, secret []byte) delivery.Outcome {
				outcome, err := s.sendSigned(ctx, endpoint, id, kind, body, secret)
				if err != nil {
					return delivery.Outcome{Error: err.Error()}
				}
				return outcome
			},
			Limiter:  s.limiter,
			Settings: policy,
			Disabled: s.tellOwnerWebhookDisabled,
		}, s.log)

		if s.identity != nil && s.apiTokens != nil {
			s.integrations.applications = applications.NewService(s.queries, s.identity, s.rooms,
				s.apiTokens, s.log)
		}
	})
}

// applicationService returns the application registry, or nil on a server
// without what it needs.
func (s *Server) applicationService() *applications.Service {
	s.buildIntegrations()
	return s.integrations.applications
}

// deliveries returns the signer and the outbound client.
func (s *Server) deliveries() (*delivery.Signer, *delivery.Client) {
	s.buildIntegrations()
	return s.integrations.signer, s.integrations.client
}

// webhookService returns the queue, or nil on a server without a database.
func (s *Server) webhookService() *webhooks.Service {
	s.buildIntegrations()
	return s.integrations.webhooks
}

/*
RunDeliveries delivers queued webhooks until ctx ends.

Started by Run alongside the listener. Exported so a test can run the same
workers against a handler it built without a listener, rather than a copy of
them that could drift.
*/
func (s *Server) RunDeliveries(ctx context.Context) {
	if service := s.webhookService(); service != nil {
		service.Run(ctx)
	}
}

/*
queueWebhooks hands a stored event to the subscriptions that want it.

Off the request, like search indexing and notifications: a message is posted
whether or not any webhook could be queued, and the person posting never waits
on anybody's endpoint.
*/
func (s *Server) queueWebhooks(ctx context.Context, event dbgen.Event) {
	service := s.webhookService()
	if service == nil {
		return
	}
	s.inBackground(ctx, "queue webhooks", func(ctx context.Context) {
		service.Enqueue(ctx, event)
	})
}

// canRead is whether somebody can read a channel right now, which is what
// every webhook decision about a channel turns on.
func (s *Server) canRead(ctx context.Context, roomID, userID uuid.UUID) bool {
	_, err := s.rooms.Visible(ctx, roomID, userID)
	return err == nil
}

// tellOwnerWebhookDisabled raises the notification that a webhook was turned
// off, attributed to the server rather than to any person.
func (s *Server) tellOwnerWebhookDisabled(ctx context.Context, subscription dbgen.WebhookSubscription, eventID *string) {
	if s.notify == nil || eventID == nil {
		return
	}
	s.notify.About(ctx, subscription.OwnerID, uuid.Nil, s.rooms.UserAddress("webhooks"),
		subscription.RoomID, *eventID, notify.ReasonWebhookDisabled, subscription.Label)
}

// privateDeliveriesAllowed reads the operator's decision, defaulting to no.
func (s *Server) privateDeliveriesAllowed(ctx context.Context) bool {
	return s.settings != nil && s.settings.Get(ctx, settings.DeliveriesAllowPrivateAddresses) == "true"
}

// deliveryTimeout is how long one attempt may take, as the operator set it.
func (s *Server) deliveryTimeout(ctx context.Context) time.Duration {
	seconds := int64(5)
	if s.settings != nil {
		if configured := s.settings.Int(ctx, settings.DeliveryTimeoutSeconds); configured > 0 {
			seconds = configured
		}
	}
	if timeout := time.Duration(seconds) * time.Second; timeout < maxDeliveryTimeout {
		return timeout
	}
	return maxDeliveryTimeout
}
