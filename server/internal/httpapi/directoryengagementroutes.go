package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/directory"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/notify"
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
		"POST /api/directory/enquiries/:id/accept":      {Summary: "Take up an enquiry you received", Authentication: "session or token"},
		"POST /api/directory/enquiries/:id/decline":     {Summary: "Decline an enquiry you received", Authentication: "session or token"},
		"POST /api/directory/enquiries/:id/withdraw":    {Summary: "Withdraw an enquiry you sent", Authentication: "session or token"},
		"GET /api/directory/me/engagements":             {Summary: "Your engagements", Authentication: "session or token"},
		"GET /api/directory/engagements/:id":            {Summary: "An engagement you are party to", Authentication: "session or token"},
		"PUT /api/directory/engagements/:id/project":    {Summary: "Link an engagement to a project you are in", Authentication: "session or token"},
		"DELETE /api/directory/engagements/:id/project": {Summary: "Unlink an engagement from its project", Authentication: "session or token"},
		"GET /api/directory/projects/:key/engagements":  {Summary: "The engagements you are party to that a project tracks", Authentication: "session or token"},
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
	engagements.PUT("/:id/project", api.handleLinkProject)
	engagements.DELETE("/:id/project", api.handleUnlinkProject)

	directoryGroup.GET("/projects/:key/engagements", api.handleProjectEngagements, requireAuthentication)
}

type partyView struct {
	Username string `json:"username"`
	// DisplayName is what to call them: their display name, or their username
	// without one, or the username kept on the engagement once the account is
	// gone.
	DisplayName string `json:"displayName"`
	Address     string `json:"address"`
}

// partyViewOf is one side of an engagement, called by the name they have now.
func (api *directoryAPI) partyViewOf(ctx context.Context, id *uuid.UUID, label string) partyView {
	party := partyView{Username: label, DisplayName: label, Address: api.rooms.UserAddress(label)}
	if id != nil {
		if user, err := api.queries.GetUserByID(ctx, *id); err == nil {
			party.DisplayName = identity.NameOf(user.Username, user.DisplayName)
		}
	}
	return party
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
	// Project is where the agreed work is tracked — present only for a viewer
	// who can see that project, so linking never tells the other party the
	// name of a private project they are not in.
	Project   *linkedProjectView `json:"project,omitempty"`
	CreatedAt string             `json:"createdAt"`
	UpdatedAt string             `json:"updatedAt"`
}

func (api *directoryAPI) engagementViewOf(ctx context.Context, engagement directory.Engagement, viewer uuid.UUID) engagementView {
	view := engagementView{
		ID: engagement.ID.String(), Title: engagement.Title, State: string(engagement.State),
		Client:       api.partyViewOf(ctx, engagement.ClientID, engagement.ClientLabel),
		Developer:    api.partyViewOf(ctx, engagement.DeveloperID, engagement.DeveloperLabel),
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
func (api *directoryAPI) announce(ctx context.Context, engagement directory.Engagement, actor uuid.UUID, summary string) {
	if engagement.RoomID == nil {
		return
	}
	event, err := api.rooms.RecordNotice(ctx, *engagement.RoomID, actor, rooms.TypeEngagementChanged,
		map[string]string{
			"engagementId": engagement.ID.String(), "state": string(engagement.State), "body": summary,
		})
	if err != nil {
		api.log.Warn("cannot announce an engagement's change", "engagementId", engagement.ID, "error", err)
		return
	}
	api.publishEvent(ctx, event)

	/*
		And the other party is notified, the way a direct message notifies
		them: what changed is addressed to them, and they may not have the
		engagement open. The person who acted is not told about their own
		step.
	*/
	other := engagement.Other(actor)
	if api.notify == nil || other == nil || summary == "" {
		return
	}
	actorUser, err := api.queries.GetUserByID(ctx, actor)
	if err != nil {
		return
	}
	api.notify.About(ctx, *other, actor, api.rooms.UserAddress(actorUser.Username),
		*engagement.RoomID, event.ID, notify.ReasonDirect, "")
}

func engagementIDParam(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil(), echo.NewHTTPError(http.StatusNotFound, directory.ErrNoEngagement.Error())
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
	api.announce(ctx, engagement, user.ID, identity.NameOf(user.Username, user.DisplayName)+" took up the enquiry")
	return c.JSON(http.StatusCreated, api.engagementViewOf(ctx, engagement, user.ID))
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
		views = append(views, api.engagementViewOf(c.Request().Context(), engagement, user.ID))
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
	return c.JSON(http.StatusOK, api.engagementWithProject(c, engagement))
}

type linkedProjectView struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// engagementWithProject is the engagement as the requester sees it, with the
// project it is linked to if the requester may see that project.
func (api *directoryAPI) engagementWithProject(c *echo.Context, engagement directory.Engagement) engagementView {
	user, _ := currentUser(c)
	view := api.engagementViewOf(c.Request().Context(), engagement, user.ID)
	if engagement.ProjectID != nil && api.projects != nil {
		access, err := api.projects.ProjectByID(c.Request().Context(), actorOf(c), *engagement.ProjectID)
		if err == nil {
			view.Project = &linkedProjectView{Key: access.Project.Key, Name: access.Project.Name}
		}
	}
	return view
}

type linkProjectRequest struct {
	// Project is the project's key.
	Project string `json:"project"`
}

/*
handleLinkProject links an engagement to the project its agreed work is tracked
in.

Either party may, to a project they are in: linking says "this work is tracked
there", which is a claim about the project only somebody in it can make. A
project somebody can merely see, being public, is refused the same as one they
cannot.
*/
func (api *directoryAPI) handleLinkProject(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	var request linkProjectRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if api.projects == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server keeps no projects")
	}

	// A party first, so somebody who is not one learns nothing about the
	// engagement from how the project check answers.
	if _, err := api.directory.Engagement(ctx, id, user.ID); err != nil {
		return engagementError(err)
	}
	access, err := api.projects.ProjectByKey(ctx, actorOf(c), request.Project)
	if err != nil {
		return projectError(err)
	}
	if access.Role == "" {
		return echo.NewHTTPError(http.StatusForbidden, "only somebody in a project can link work to it")
	}

	engagement, err := api.directory.LinkProject(ctx, id, user.ID, &access.Project.ID)
	if err != nil {
		return engagementError(err)
	}
	api.announce(ctx, engagement, user.ID, identity.NameOf(user.Username, user.DisplayName)+" linked the engagement to a project")
	return c.JSON(http.StatusOK, api.engagementWithProject(c, engagement))
}

// handleUnlinkProject undoes a link. Either party may, whatever their place in
// the project: taking work out of a project claims nothing about it.
func (api *directoryAPI) handleUnlinkProject(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	engagement, err := api.directory.LinkProject(ctx, id, user.ID, nil)
	if err != nil {
		return engagementError(err)
	}
	api.announce(ctx, engagement, user.ID, identity.NameOf(user.Username, user.DisplayName)+" unlinked the engagement from its project")
	return c.JSON(http.StatusOK, api.engagementWithProject(c, engagement))
}

type linkedEngagementView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

/*
handleProjectEngagements lists, for a project, the engagements linked to it
that the requester is a party to — so a party can go from the work to the
agreement. Anybody else gets an empty list: a project member who is not a party
learns neither the terms nor that there are any.
*/
func (api *directoryAPI) handleProjectEngagements(c *echo.Context) error {
	user, _ := currentUser(c)
	if api.projects == nil {
		return echo.NewHTTPError(http.StatusNotFound, "this server keeps no projects")
	}
	access, err := api.projectFrom(c)
	if err != nil {
		return err
	}
	linked, err := api.directory.LinkedTo(c.Request().Context(), access.Project.ID, user.ID)
	if err != nil {
		return err
	}
	views := make([]linkedEngagementView, 0, len(linked))
	for _, engagement := range linked {
		views = append(views, linkedEngagementView{
			ID: engagement.ID.String(), Title: engagement.Title, State: string(engagement.State),
		})
	}
	return c.JSON(http.StatusOK, views)
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
	api.announce(ctx, engagement, user.ID, identity.NameOf(user.Username, user.DisplayName)+" withdrew from the engagement")
	return c.JSON(http.StatusOK, api.engagementViewOf(ctx, engagement, user.ID))
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
