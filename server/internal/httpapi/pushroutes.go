package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Subscribing a device to notifications it can receive while closed.

The browser does the hard part: it asks its own push service for an endpoint
and a key pair, keeps the private half, and hands the rest here. What this
server stores is enough to send to that device and not enough to read what it
sent.
*/

// pushConfigResponse is what a browser needs before it can subscribe.
type pushConfigResponse struct {
	// Enabled is false on a server whose operator has generated no keys, which
	// is an ordinary way to run: notifications still exist in the application.
	Enabled bool `json:"enabled"`
	// PublicKey identifies this server to the push service. Public by
	// definition — it is handed to every browser that subscribes.
	PublicKey string `json:"publicKey,omitempty"`
}

func (s *Server) handlePushConfig(c *echo.Context) error {
	if s.push == nil || !s.push.Enabled() {
		return c.JSON(http.StatusOK, pushConfigResponse{Enabled: false})
	}

	return c.JSON(http.StatusOK, pushConfigResponse{
		Enabled:   true,
		PublicKey: s.push.PublicKey(),
	})
}

type subscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type subscriptionResponse struct {
	ID string `json:"id"`
	// Device is what subscribed, so somebody reviewing their devices sees
	// something they recognise rather than an opaque endpoint.
	Device    string `json:"device"`
	CreatedAt string `json:"createdAt"`
	// LastUsedAt says when something last reached this device, so a
	// subscription that has quietly stopped working is visible as one.
	LastUsedAt string `json:"lastUsedAt,omitempty"`
	// Current marks the device making the request, so somebody can tell which
	// one they are about to remove.
	Current bool `json:"current"`
}

func (s *Server) handleSubscribeToPush(c *echo.Context) error {
	user, _ := currentUser(c)

	if s.push == nil || !s.push.Enabled() {
		return echo.NewHTTPError(http.StatusNotFound,
			"this server does not offer notifications to closed browsers")
	}

	var request subscribeRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	/*
		All three parts are required.

		A subscription missing its keys cannot be encrypted to, so storing one
		would mean a device that appears subscribed and never receives
		anything — which is worse than refusing, because nobody investigates a
		notification that simply did not arrive.
	*/
	if request.Endpoint == "" || request.Keys.P256dh == "" || request.Keys.Auth == "" {
		return echo.NewHTTPError(http.StatusBadRequest,
			"a subscription needs an endpoint and its keys")
	}

	subscription, err := s.push.Subscribe(c.Request().Context(), user.ID,
		request.Endpoint, request.Keys.P256dh, request.Keys.Auth,
		c.Request().UserAgent())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, s.subscriptionResponse(c, subscription))
}

func (s *Server) handleListPushSubscriptions(c *echo.Context) error {
	user, _ := currentUser(c)

	if s.push == nil {
		return c.JSON(http.StatusOK, []subscriptionResponse{})
	}

	subscriptions, err := s.push.Subscriptions(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	response := make([]subscriptionResponse, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		response = append(response, s.subscriptionResponse(c, subscription))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleUnsubscribeFromPush(c *echo.Context) error {
	user, _ := currentUser(c)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such subscription")
	}

	if s.push == nil {
		return c.NoContent(http.StatusNoContent)
	}

	// Scoped to the owner inside the service, so one person cannot unsubscribe
	// another's device by guessing an identifier.
	if err := s.push.Unsubscribe(c.Request().Context(), id, user.ID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) subscriptionResponse(
	c *echo.Context,
	subscription dbgen.PushSubscription,
) subscriptionResponse {
	response := subscriptionResponse{
		ID:        subscription.ID.String(),
		Device:    deviceName(subscription.UserAgent),
		CreatedAt: subscription.CreatedAt.UTC().Format(time.RFC3339),
		// Matched on the user agent rather than anything stronger: it is a
		// hint for somebody reading a list, not a security boundary, and
		// getting it wrong costs a mislabelled row.
		Current: subscription.UserAgent == c.Request().UserAgent(),
	}

	if subscription.LastUsedAt != nil {
		response.LastUsedAt = subscription.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return response
}

/*
deviceName describes a device the way its owner would recognise it.

A user-agent string is precise and unreadable. Somebody deciding whether to
remove a subscription needs "Chrome on Android", not a version tuple.
*/
func deviceName(userAgent string) string {
	if userAgent == "" {
		return "Unknown device"
	}

	browser := ""
	switch {
	case strings.Contains(userAgent, "Edg/"):
		browser = "Edge"
	case strings.Contains(userAgent, "OPR/"):
		browser = "Opera"
	case strings.Contains(userAgent, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(userAgent, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(userAgent, "Safari/"):
		browser = "Safari"
	}

	platform := ""
	switch {
	case strings.Contains(userAgent, "Android"):
		platform = "Android"
	case strings.Contains(userAgent, "iPhone"), strings.Contains(userAgent, "iPad"):
		platform = "iOS"
	case strings.Contains(userAgent, "Mac OS X"):
		platform = "macOS"
	case strings.Contains(userAgent, "Windows"):
		platform = "Windows"
	case strings.Contains(userAgent, "Linux"):
		platform = "Linux"
	}

	switch {
	case browser != "" && platform != "":
		return browser + " on " + platform
	case browser != "":
		return browser
	default:
		// Anything unrecognised keeps its own string rather than being
		// flattened: somebody may need to see exactly what subscribed.
		return userAgent
	}
}

type hidePushContentRequest struct {
	Hide bool `json:"hide"`
}

type hidePushContentResponse struct {
	Hide bool `json:"hide"`
}

/*
handleGetHidePushContent reports the setting back.

Offered because the control that sets it has to show its current state, and a
checkbox that always opens unchecked would tell somebody their messages are on
their lock screen when they are not — or, worse, the reverse.
*/
func (s *Server) handleGetHidePushContent(c *echo.Context) error {
	user, _ := currentUser(c)

	return c.JSON(http.StatusOK, hidePushContentResponse{Hide: user.HidePushContent})
}

/*
handleSetHidePushContent records whether somebody wants message content kept
off their lock screen.

On the account rather than per device: somebody who does not want their
messages on a lock screen does not want them on one of their lock screens.
*/
func (s *Server) handleSetHidePushContent(c *echo.Context) error {
	user, _ := currentUser(c)

	var request hidePushContentRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if err := s.queries.SetHidePushContent(c.Request().Context(), dbgen.SetHidePushContentParams{
		ID:              user.ID,
		HidePushContent: request.Hide,
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
