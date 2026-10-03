package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/delivery"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
Signed deliveries: the keys that verify them, and a way to send one on demand.

Everything a program receives from this server — webhook events, presses on its
messages — is signed the same way, so a receiver writes one check. What is here
is the part a receiver needs before it has received anything: where the keys
are, and a delivery it can point its check at to see that it works.
*/

// DeliveryKeysPath is where the keys verifying a delivery are published.
const DeliveryKeysPath = "/.well-known/tdc-delivery-keys"

func init() {
	registerRoutes(func(s *Server) {
		/*
			Unauthenticated, because a receiver holds no credential here — it
			is somebody else's server, checking that a request came from this
			one. Nothing in it is secret: public halves, and the rules for
			checking them.
		*/
		s.echo.GET(DeliveryKeysPath, s.handleDeliveryKeys)

		s.echo.POST("/api/integrations/test-delivery", s.handleTestDelivery, requireAuthentication)
	})

	documentRoutes(map[string]entry{
		"POST /api/integrations/test-delivery": {
			Summary: "Send a signed test delivery", Authentication: "session or token",
		},
	})
}

// deliveryKeys is what a receiver fetches to verify deliveries.
type deliveryKeys struct {
	// Algorithm is how the signatures are made. One, stated so a receiver
	// that assumed otherwise finds out from the document rather than from a
	// verification that always fails.
	Algorithm string `json:"algorithm"`
	// ReplayWindowSeconds is how far from now a signed timestamp may be.
	ReplayWindowSeconds int `json:"replayWindowSeconds"`
	// Keys includes retired ones, which still verify what they signed.
	Keys []events.PublishedKey `json:"keys"`
}

func (s *Server) handleDeliveryKeys(c *echo.Context) error {
	document := deliveryKeys{
		Algorithm:           "ed25519",
		ReplayWindowSeconds: int(delivery.ReplayWindow / time.Second),
		Keys:                []events.PublishedKey{},
	}
	if s.rooms != nil {
		if keyring := s.rooms.Keyring(); keyring != nil {
			document.Keys = keyring.Published()
		}
	}

	// Cacheable for a while: keys change on rotation, rarely, and a receiver
	// that refetches on an unknown key identifier picks the new one up anyway.
	c.Response().Header().Set("Cache-Control", "public, max-age=300")
	return c.JSON(http.StatusOK, document)
}

type testDeliveryRequest struct {
	URL string `json:"url"`
	// Secret, when given, is used for the HMAC exactly as a webhook's would
	// be, so both checks can be tried against one delivery.
	Secret string `json:"secret"`
}

type testDeliveryResponse struct {
	ID        string `json:"id"`
	Delivered bool   `json:"delivered"`
	Status    int    `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
	Response  string `json:"response,omitempty"`
}

// testDeliveriesPerMinute bounds how often one person can make this server
// send a request somewhere.
var testDeliveriesPerMinute = cache.RateLimit{Attempts: 10, Window: time.Minute}

/*
handleTestDelivery sends one signed delivery to an endpoint, now.

For somebody writing a receiver: a real delivery, signed exactly as every
other, to check their verification against before anything depends on it.

Subject to everything a real delivery is — the same destination check, the same
guard at connection time, the same bound on how long it may take — and limited
per person, because an endpoint that makes this server send a request anywhere
on demand is one somebody would otherwise use to make it send a great many.
*/
func (s *Server) handleTestDelivery(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	var request testDeliveryRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if s.limiter != nil {
		decision, err := s.limiter.Allow(ctx, "test-delivery:"+user.ID.String(), testDeliveriesPerMinute)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return retryAfter(c, decision.RetryAfter)
		}
	}

	if err := delivery.CheckDestination(ctx, request.URL, s.privateDeliveriesAllowed(ctx)); err != nil {
		return destinationError(err)
	}

	id := uuid.NewString()
	body, err := json.Marshal(map[string]any{
		"version":     1,
		"id":          id,
		"type":        "delivery.test",
		"occurredAt":  time.Now().UTC().Format(time.RFC3339),
		"server":      s.serverName(),
		"requestedBy": s.rooms.UserAddress(user.Username),
	})
	if err != nil {
		return err
	}

	outcome, err := s.sendSigned(ctx, request.URL, id, "delivery.test", body, []byte(request.Secret))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, testDeliveryResponse{
		ID:        id,
		Delivered: outcome.Delivered,
		Status:    outcome.Status,
		Error:     outcome.Error,
		Response:  outcome.Response,
	})
}

// sendSigned signs a delivery and sends it, bounded by the operator's timeout.
func (s *Server) sendSigned(
	ctx context.Context,
	endpoint, id, kind string,
	body, secret []byte,
) (delivery.Outcome, error) {
	signer, client := s.deliveries()
	if signer == nil {
		return delivery.Outcome{}, errors.New("this server has no signing key")
	}

	headers, err := signer.Sign(id, kind, body, secret)
	if err != nil {
		return delivery.Outcome{}, err
	}

	attempt, cancel := context.WithTimeout(ctx, s.deliveryTimeout(ctx))
	defer cancel()
	return client.Send(attempt, endpoint, headers, body), nil
}

// destinationError reports an endpoint that will never be delivered to.
func destinationError(err error) error {
	switch {
	case errors.Is(err, delivery.ErrPrivateDestination):
		// Forbidden rather than bad request: the address is well formed, and
		// the refusal is this server's policy about where it will send.
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, delivery.ErrUnusableURL):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	default:
		return err
	}
}
