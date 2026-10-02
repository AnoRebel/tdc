package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/directory"
	"github.com/AnoRebel/tdc/server/internal/rooms"
)

/*
Engagements over HTTP.

Every route answers only the engagement's two parties. Anybody else — another
member, a moderator, an administrator — is told there is no such engagement,
because the spec allows nobody outside an engagement to read it and a refusal
that said "forbidden" would confirm it exists.
*/

func init() {
	documentRoutes(map[string]entry{
		"POST /api/directory/enquiries/:id/accept":   {Summary: "Take up an enquiry you received", Authentication: "session or token"},
		"POST /api/directory/enquiries/:id/decline":  {Summary: "Decline an enquiry you received", Authentication: "session or token"},
		"POST /api/directory/enquiries/:id/withdraw": {Summary: "Withdraw an enquiry you sent", Authentication: "session or token"},
		"GET /api/directory/me/engagements":          {Summary: "Your engagements", Authentication: "session or token"},
		"GET /api/directory/engagements/:id":         {Summary: "An engagement you are party to", Authentication: "session or token"},
		"POST /api/directory/engagements/:id/withdraw": {
			Summary: "Withdraw from an engagement before an agreement", Authentication: "session or token",
		},
	})
}

func (api *directoryAPI) mountEngagements(directoryGroup, me *echo.Group) {
	enquiries := directoryGroup.Group("/enquiries", requireAuthentication)
	enquiries.POST("/:id/accept", api.handleAcceptEnquiry)
	enquiries.POST("/:id/decline", api.handleDeclineEnquiry)
	enquiries.POST("/:id/withdraw", api.handleWithdrawEnquiry)

	me.GET("/engagements", api.handleListEngagements)

	engagements := directoryGroup.Group("/engagements", requireAuthentication)
	engagements.GET("/:id", api.handleGetEngagement)
	engagements.POST("/:id/withdraw", api.handleWithdrawEngagement)
}

type partyView struct {
	Username string `json:"username"`
	Address  string `json:"address"`
}

type transitionView struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Actor string `json:"actor"`
	Note  string `json:"note,omitempty"`
	At    string `json:"at"`
}

type engagementView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Client    partyView `json:"client"`
	Developer partyView `json:"developer"`
	// You is which party the viewer is: "client" or "developer".
	You       string `json:"you"`
	RoomID    string `json:"roomId,omitempty"`
	EnquiryID string `json:"enquiryId,omitempty"`
	// Withdrawable says the engagement can still be withdrawn from: there is
	// no agreement yet.
	Withdrawable bool `json:"withdrawable"`
	// CompletionRequestedBy is who asked for the work to be recorded as
	// complete, while the other has not confirmed.
	CompletionRequestedBy string           `json:"completionRequestedBy,omitempty"`
	CompletionRequestedAt string           `json:"completionRequestedAt,omitempty"`
	Transitions           []transitionView `json:"transitions,omitempty"`
	CreatedAt             string           `json:"createdAt"`
	UpdatedAt             string           `json:"updatedAt"`
}

func (api *directoryAPI) engagementViewOf(engagement directory.Engagement, viewer uuid.UUID) engagementView {
	view := engagementView{
		ID: engagement.ID.String(), Title: engagement.Title, State: string(engagement.State),
		Client:       partyView{Username: engagement.ClientLabel, Address: api.rooms.UserAddress(engagement.ClientLabel)},
		Developer:    partyView{Username: engagement.DeveloperLabel, Address: api.rooms.UserAddress(engagement.DeveloperLabel)},
		You:          "client",
		Withdrawable: engagement.State.Withdrawable(),
		CreatedAt:    engagement.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    engagement.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if engagement.IsDeveloper(viewer) {
		view.You = "developer"
	}
	if engagement.RoomID != nil {
		view.RoomID = engagement.RoomID.String()
	}
	if engagement.EnquiryID != nil {
		view.EnquiryID = engagement.EnquiryID.String()
	}
	if engagement.CompletionRequestedBy != nil && engagement.CompletionRequestedAt != nil {
		view.CompletionRequestedBy = engagement.ClientLabel
		if engagement.DeveloperID != nil && *engagement.CompletionRequestedBy == *engagement.DeveloperID {
			view.CompletionRequestedBy = engagement.DeveloperLabel
		}
		view.CompletionRequestedAt = engagement.CompletionRequestedAt.UTC().Format(time.RFC3339)
	}
	for _, transition := range engagement.Transitions {
		view.Transitions = append(view.Transitions, transitionView{
			From: string(transition.From), To: string(transition.To), Actor: transition.Actor,
			Note: transition.Note, At: transition.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	return view
}

/*
announce tells both parties an engagement changed, over the connection they
already hold.

A notice in the engagement's conversation rather than a frame of its own, so it
is delivered, replayed on reconnection and ordered exactly like the messages
around it. Failure is logged rather than returned: the change is recorded, and
somebody who missed the notice sees the new state when they next read it.
*/
func (api *directoryAPI) announce(ctx context.Context, engagement directory.Engagement, actor uuid.UUID) {
	if engagement.RoomID == nil {
		return
	}
	event, err := api.rooms.RecordNotice(ctx, *engagement.RoomID, actor, rooms.TypeEngagementChanged,
		map[string]string{"engagementId": engagement.ID.String(), "state": string(engagement.State)})
	if err != nil {
		api.log.Warn("cannot announce an engagement's change", "engagementId", engagement.ID, "error", err)
		return
	}
	api.publishEvent(ctx, event)
}

func engagementIDParam(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, echo.NewHTTPError(http.StatusNotFound, directory.ErrNoEngagement.Error())
	}
	return id, nil
}

func (api *directoryAPI) handleAcceptEnquiry(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return enquiryError(directory.ErrNoEnquiry)
	}

	engagement, err := api.directory.AcceptEnquiry(ctx, id, user.ID)
	if err != nil {
		return enquiryError(err)
	}
	api.announce(ctx, engagement, user.ID)
	return c.JSON(http.StatusCreated, api.engagementViewOf(engagement, user.ID))
}

func (api *directoryAPI) closeEnquiry(c *echo.Context, status directory.EnquiryStatus) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return enquiryError(directory.ErrNoEnquiry)
	}
	enquiry, err := api.directory.CloseEnquiry(ctx, id, user.ID, status)
	if err != nil {
		return enquiryError(err)
	}
	return c.JSON(http.StatusOK, api.enquiryViewOf(enquiry, status == directory.EnquiryDeclined))
}

func (api *directoryAPI) handleDeclineEnquiry(c *echo.Context) error {
	return api.closeEnquiry(c, directory.EnquiryDeclined)
}

func (api *directoryAPI) handleWithdrawEnquiry(c *echo.Context) error {
	return api.closeEnquiry(c, directory.EnquiryWithdrawn)
}

func (api *directoryAPI) handleListEngagements(c *echo.Context) error {
	user, _ := currentUser(c)
	engagements, err := api.directory.Engagements(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	views := make([]engagementView, 0, len(engagements))
	for _, engagement := range engagements {
		views = append(views, api.engagementViewOf(engagement, user.ID))
	}
	return c.JSON(http.StatusOK, views)
}

func (api *directoryAPI) handleGetEngagement(c *echo.Context) error {
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	engagement, err := api.directory.Engagement(c.Request().Context(), id, user.ID)
	if err != nil {
		return engagementError(err)
	}
	return c.JSON(http.StatusOK, api.engagementViewOf(engagement, user.ID))
}

type withdrawRequest struct {
	Reason string `json:"reason"`
}

func (api *directoryAPI) handleWithdrawEngagement(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	var request withdrawRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	engagement, err := api.directory.Withdraw(ctx, id, user.ID, user.Username, request.Reason)
	if err != nil {
		return engagementError(err)
	}
	api.announce(ctx, engagement, user.ID)
	return c.JSON(http.StatusOK, api.engagementViewOf(engagement, user.ID))
}

func engagementError(err error) error {
	switch {
	case errors.Is(err, directory.ErrNoEngagement):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, directory.ErrNotNow), errors.Is(err, directory.ErrChanged):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	return directoryError(err)
}
