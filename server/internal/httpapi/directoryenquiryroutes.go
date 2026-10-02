package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/directory"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Enquiries over HTTP: sending one from a profile, and reading one's own.

Sending runs through the same machinery as any message — the content filter,
the event log, search indexing for the people in the conversation,
notifications, live delivery — because an enquiry is a message, just one
addressed through a profile rather than a conversation. What is different is
who decides it may be sent, which is the recipient's profile, never their
direct-message setting.
*/

func init() {
	documentRoutes(map[string]entry{
		"POST /api/directory/profiles/:username/enquiries": {
			Summary: "Send an enquiry through somebody's profile", Authentication: "session or token",
		},
		"GET /api/directory/me/enquiries": {
			Summary: "Enquiries you received, or with ?box=sent those you sent", Authentication: "session or token",
		},
	})
}

func (api *directoryAPI) mountEnquiries(directoryGroup, me *echo.Group) {
	directoryGroup.POST("/profiles/:username/enquiries", api.handleSendEnquiry, requireAuthentication)
	me.GET("/enquiries", api.handleListEnquiries)
}

type enquiryRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type enquiryView struct {
	ID       string `json:"id"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Status   string `json:"status"`
	RoomID   string `json:"roomId,omitempty"`
	Received bool   `json:"received"`
	// With is the other person.
	With        string `json:"with"`
	WithName    string `json:"withName"`
	CreatedAt   string `json:"createdAt"`
	RespondedAt string `json:"respondedAt,omitempty"`
	// EngagementID is present once the enquiry was taken up.
	EngagementID string `json:"engagementId,omitempty"`
}

func (api *directoryAPI) enquiryViewOf(enquiry directory.Enquiry, received bool) enquiryView {
	view := enquiryView{
		ID: enquiry.ID.String(), Subject: enquiry.Subject, Body: enquiry.Body, Status: string(enquiry.Status),
		Received: received, With: enquiry.OtherUsername, WithName: enquiry.OtherDisplayName,
		CreatedAt: enquiry.CreatedAt.UTC().Format(time.RFC3339),
	}
	if enquiry.RoomID != nil {
		view.RoomID = enquiry.RoomID.String()
	}
	if enquiry.RespondedAt != nil {
		view.RespondedAt = enquiry.RespondedAt.UTC().Format(time.RFC3339)
	}
	if enquiry.EngagementID != nil {
		view.EngagementID = enquiry.EngagementID.String()
	}
	return view
}

/*
handleSendEnquiry sends an enquiry through somebody's profile.

The order is the point. Everything that can refuse — whether they take
enquiries from this sender, the rate, an unanswered enquiry already waiting,
the content filter — refuses before anything is made, so a refused enquiry
leaves nothing behind and is not counted against anybody but the sender.
*/
func (api *directoryAPI) handleSendEnquiry(c *echo.Context) error {
	ctx := c.Request().Context()
	sender, _ := currentUser(c)

	var request enquiryRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	input := directory.EnquiryInput{Subject: request.Subject, Body: request.Body}
	if err := input.Normalise(); err != nil {
		return directoryError(err)
	}

	recipient, err := api.directory.RecipientFor(ctx, c.Param("username"), sender.ID, directory.Member)
	if err != nil {
		return enquiryError(err)
	}

	// Before the rate is counted: asking again before an answer is refused,
	// and a refusal costs the sender nothing.
	if pending, err := api.directory.PendingBetween(ctx, sender.ID, recipient.ID); err != nil {
		return err
	} else if pending {
		return enquiryError(directory.ErrAlreadyAsked)
	}

	/*
		Counted per sender, and only for enquiries that could be delivered.

		A refusal above spends nothing, so somebody probing which profiles
		take enquiries learns no more from the limit than from the refusal
		itself.
	*/
	perHour := int(api.limitFor(ctx, settings.DirectoryEnquiriesPerHour))
	if perHour < 1 {
		perHour = 1
	}
	decision, err := api.limiter.Allow(ctx, "directory:enquiry:"+sender.ID.String(),
		cache.RateLimit{Attempts: perHour, Window: time.Hour})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}

	// The operator's content filter applies to an enquiry exactly as to any
	// message: it is one.
	filtered, err := api.applyContentFilter(ctx, input.Body)
	if err != nil {
		return err
	}
	input.Body = filtered.Body

	room, err := api.directory.OpenEnquiryRoom(ctx, sender.ID, recipient.ID, input.Subject, api.homeServer())
	if err != nil {
		return err
	}
	event, err := api.rooms.SendMessage(ctx, room.ID, sender.ID, input.Body, "")
	if err != nil {
		api.directory.AbandonEnquiryRoom(ctx, room.ID)
		return roomError(err)
	}
	enquiry, err := api.directory.RecordEnquiry(ctx, sender.ID, recipient.ID, room.ID, input)
	if err != nil {
		api.directory.AbandonEnquiryRoom(ctx, room.ID)
		return enquiryError(err)
	}

	if filtered.Flagged {
		api.flagForReview(c, room.ID, event.ID,
			api.rooms.UserAddress(sender.Username), filtered.Matched, filtered.Body)
	}

	// The same after-work as any message: indexed for the people in the
	// conversation, the recipient notified — the sender is excluded, as the
	// author of any message is — and delivered live.
	body := input.Body
	api.inBackground(ctx, "index", func(ctx context.Context) {
		api.indexForSearch(ctx, event, body)
	})
	api.inBackground(ctx, "notify", func(ctx context.Context) {
		api.notifyRecipients(ctx, event, body, room.ID)
	})
	api.publishEvent(ctx, event)

	enquiry.OtherUsername, enquiry.OtherDisplayName = recipient.Username, recipient.DisplayName
	return c.JSON(http.StatusCreated, api.enquiryViewOf(enquiry, false))
}

// homeServer is this server's own name, which a room it makes carries.
func (api *directoryAPI) homeServer() string {
	return strings.TrimPrefix(api.rooms.UserAddress(""), "@")
}

func (api *directoryAPI) handleListEnquiries(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	sent := c.QueryParam("box") == "sent"
	var (
		enquiries []directory.Enquiry
		err       error
	)
	if sent {
		enquiries, err = api.directory.Sent(ctx, user.ID)
	} else {
		enquiries, err = api.directory.Received(ctx, user.ID)
	}
	if err != nil {
		return err
	}

	views := make([]enquiryView, 0, len(enquiries))
	for _, enquiry := range enquiries {
		views = append(views, api.enquiryViewOf(enquiry, !sent))
	}
	return c.JSON(http.StatusOK, views)
}

func enquiryError(err error) error {
	switch {
	case errors.Is(err, directory.ErrNotAccepting):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, directory.ErrAlreadyAsked), errors.Is(err, directory.ErrAnswered):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, directory.ErrNoEnquiry):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return directoryError(err)
}
