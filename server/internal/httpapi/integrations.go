package httpapi

import (
	"context"
	"sync"
	"time"

	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
What programs acting on this server need the HTTP layer to hold.

Built on first use rather than at construction, so a server nobody has pointed
a webhook at carries none of it, and so adding it did not change how every test
and the command build a server.
*/
type integrationsState struct {
	once   sync.Once
	signer *delivery.Signer
	client *delivery.Client
}

/*
maxDeliveryTimeout is the ceiling the outbound client is built with.

The operator's setting is applied per attempt, beneath it, so changing the
setting takes effect without a restart; this only stops a mistyped setting from
holding a worker for an hour.
*/
const maxDeliveryTimeout = 30 * time.Second

// deliveries returns the signer and the outbound client, building them once.
func (s *Server) deliveries() (*delivery.Signer, *delivery.Client) {
	s.integrations.once.Do(func() {
		if s.rooms != nil {
			s.integrations.signer = delivery.NewSigner(s.rooms.Keyring())
		}
		s.integrations.client = delivery.NewClient(maxDeliveryTimeout, func() bool {
			return s.privateDeliveriesAllowed(context.Background())
		})
	})
	return s.integrations.signer, s.integrations.client
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
