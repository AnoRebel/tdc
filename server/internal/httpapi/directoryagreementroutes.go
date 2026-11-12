package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/directory"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

/*
Proposals, the agreement and finishing, over HTTP.

Every response that shows an agreement carries the same disclosure, taken from
the one constant the export uses too, so the wording a client shows and the
wording in a file somebody keeps cannot differ. There is no route that changes a
sent version, a response or an agreement; the database would refuse one anyway.
*/

func init() {
	session := "session or token"
	documentRoutes(map[string]entry{
		"GET /api/directory/engagements/:id/proposals":                       {Summary: "Proposals, responses and agreement on an engagement", Authentication: session},
		"POST /api/directory/engagements/:id/proposals":                      {Summary: "Send a proposal, or a new version of it", Authentication: session},
		"POST /api/directory/engagements/:id/proposals/:versionId/responses": {Summary: "Accept, decline or ask for changes to the latest proposal", Authentication: session},
		"POST /api/directory/engagements/:id/start":                          {Summary: "Begin the agreed work", Authentication: session},
		"POST /api/directory/engagements/:id/complete":                       {Summary: "Say the work is complete", Authentication: session},
		"POST /api/directory/engagements/:id/confirm-completion":             {Summary: "Confirm the other party's completion", Authentication: session},
		"GET /api/directory/engagements/:id/agreement/export":                {Summary: "Export the agreement", Authentication: session},
		"POST /api/directory/engagements/:id/revisions":                      {Summary: "Propose a revision of the agreement in force", Authentication: session},
		"POST /api/directory/engagements/:id/revisions/:number/withdraw":     {Summary: "Withdraw a revision you opened", Authentication: session},
	})
}

func (api *directoryAPI) mountAgreements(directoryGroup *echo.Group) {
	engagements := directoryGroup.Group("/engagements", requireAuthentication)
	engagements.GET("/:id/proposals", api.handleNegotiation)
	engagements.POST("/:id/proposals", api.handlePropose)
	engagements.POST("/:id/proposals/:versionId/responses", api.handleRespond)
	engagements.POST("/:id/start", api.handleStartWork)
	engagements.POST("/:id/complete", api.handleRequestCompletion)
	engagements.POST("/:id/confirm-completion", api.handleConfirmCompletion)
	engagements.GET("/:id/agreement/export", api.handleExportAgreement)
	engagements.POST("/:id/revisions", api.handleOpenRevision)
	engagements.POST("/:id/revisions/:number/withdraw", api.handleWithdrawRevision)
}

type versionView struct {
	ID      string `json:"id"`
	Version int32  `json:"version"`
	// Revision is the revision the version was sent in; absent for the
	// negotiation before the first agreement.
	Revision   int32                `json:"revision,omitempty"`
	SentBy     string               `json:"sentBy"`
	Scope      string               `json:"scope"`
	Timeline   string               `json:"timeline"`
	LineItems  []directory.LineItem `json:"lineItems"`
	Currency   string               `json:"currency"`
	TotalMinor int64                `json:"totalMinor"`
	SentAt     string               `json:"sentAt"`
}

type responseView struct {
	Version   int32  `json:"version"`
	Responder string `json:"responder"`
	Kind      string `json:"kind"`
	Part      string `json:"part,omitempty"`
	Comment   string `json:"comment,omitempty"`
	At        string `json:"at"`
}

type agreementView struct {
	Version int32 `json:"version"`
	// Revision is the revision the agreement came from, 0 for the first.
	Revision            int32       `json:"revision"`
	ClientAcceptedAt    string      `json:"clientAcceptedAt"`
	DeveloperAcceptedAt string      `json:"developerAcceptedAt"`
	Terms               versionView `json:"terms"`
	RecordedAt          string      `json:"recordedAt"`
}

type revisionView struct {
	Number   int32  `json:"number"`
	OpenedBy string `json:"openedBy"`
	Reason   string `json:"reason"`
	OpenedAt string `json:"openedAt"`
	// Outcome is "open", "accepted", "declined" or "withdrawn".
	Outcome     string `json:"outcome"`
	OutcomeBy   string `json:"outcomeBy,omitempty"`
	OutcomeNote string `json:"outcomeNote,omitempty"`
	OutcomeAt   string `json:"outcomeAt,omitempty"`
}

type negotiationView struct {
	Versions  []versionView  `json:"versions"`
	Responses []responseView `json:"responses"`
	// Agreement is the agreement in force.
	Agreement *agreementView `json:"agreement,omitempty"`
	// Agreements is every agreement recorded, the first one first.
	Agreements []agreementView `json:"agreements"`
	// Revisions is every revision of the agreement, the first one first.
	Revisions []revisionView `json:"revisions"`
	// Disclosure is what the server says it is and is not, shown wherever an
	// agreement is.
	Disclosure string `json:"disclosure"`
}

func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }

func versionViewOf(version directory.ProposalVersion) versionView {
	items := version.LineItems
	if items == nil {
		items = []directory.LineItem{}
	}
	return versionView{
		ID: version.ID.String(), Version: version.Version, Revision: version.Revision, SentBy: version.SentBy,
		Scope: version.Scope, Timeline: version.Timeline, LineItems: items,
		Currency: version.Currency, TotalMinor: version.TotalMinor, SentAt: stamp(version.SentAt),
	}
}

func agreementViewOf(agreement directory.Agreement) agreementView {
	return agreementView{
		Version: agreement.Version, Revision: agreement.Revision, ClientAcceptedAt: stamp(agreement.ClientAcceptedAt),
		DeveloperAcceptedAt: stamp(agreement.DeveloperAcceptedAt),
		Terms:               versionViewOf(agreement.Terms), RecordedAt: stamp(agreement.RecordedAt),
	}
}

func revisionViewOf(revision directory.Revision) revisionView {
	view := revisionView{
		Number: revision.Number, OpenedBy: revision.OpenedBy, Reason: revision.Reason,
		OpenedAt: stamp(revision.OpenedAt), Outcome: "open",
	}
	if !revision.Open() {
		view.Outcome, view.OutcomeBy, view.OutcomeNote = string(revision.Outcome), revision.OutcomeBy, revision.OutcomeNote
		if revision.OutcomeAt != nil {
			view.OutcomeAt = stamp(*revision.OutcomeAt)
		}
	}
	return view
}

func negotiationViewOf(negotiation directory.Negotiation) negotiationView {
	view := negotiationView{
		Versions: []versionView{}, Responses: []responseView{}, Agreements: []agreementView{},
		Revisions: []revisionView{}, Disclosure: directory.Disclosure,
	}
	for _, version := range negotiation.Versions {
		view.Versions = append(view.Versions, versionViewOf(version))
	}
	for _, response := range negotiation.Responses {
		view.Responses = append(view.Responses, responseView{
			Version: response.Version, Responder: response.Responder, Kind: string(response.Kind),
			Part: response.Part, Comment: response.Comment, At: stamp(response.At),
		})
	}
	if agreement := negotiation.Agreement; agreement != nil {
		inForce := agreementViewOf(*agreement)
		view.Agreement = &inForce
	}
	for _, agreement := range negotiation.Agreements {
		view.Agreements = append(view.Agreements, agreementViewOf(agreement))
	}
	for _, revision := range negotiation.Revisions {
		view.Revisions = append(view.Revisions, revisionViewOf(revision))
	}
	return view
}

func (api *directoryAPI) handleNegotiation(c *echo.Context) error {
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	negotiation, err := api.directory.Negotiation(c.Request().Context(), id, user.ID)
	if err != nil {
		return agreementError(err)
	}
	return c.JSON(http.StatusOK, negotiationViewOf(negotiation))
}

type proposalRequest struct {
	Scope     string               `json:"scope"`
	Timeline  string               `json:"timeline"`
	LineItems []directory.LineItem `json:"lineItems"`
	Currency  string               `json:"currency"`
}

func (api *directoryAPI) handlePropose(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	var request proposalRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	version, engagement, err := api.directory.Propose(ctx, id, user.ID, user.Username, directory.ProposalInput{
		Scope: request.Scope, Timeline: request.Timeline, LineItems: request.LineItems, Currency: request.Currency,
	})
	if err != nil {
		return agreementError(err)
	}
	// Announced in the conversation, where people read names; the agreement's
	// own record keeps the username as the identifier.
	who := identity.NameOf(user.Username, user.DisplayName)
	summary := fmt.Sprintf("%s sent version %d of the proposal", who, version.Version)
	if version.Revision > 0 {
		summary = fmt.Sprintf("%s sent version %d in revision %d of the agreement", who, version.Version, version.Revision)
	}
	api.announce(ctx, engagement, user.ID, summary)
	return c.JSON(http.StatusCreated, versionViewOf(version))
}

type respondRequest struct {
	Kind    string `json:"kind"`
	Part    string `json:"part"`
	Comment string `json:"comment"`
}

func (api *directoryAPI) handleRespond(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	versionID, err := uuid.Parse(c.Param("versionId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, directory.ErrNotLatest.Error())
	}
	var request respondRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	answer, engagement, err := api.directory.Respond(ctx, id, user.ID, user.Username, versionID,
		directory.ResponseKind(request.Kind), request.Part, request.Comment)
	if err != nil {
		return agreementError(err)
	}

	what := "the proposal"
	if answer.Revision > 0 {
		what = fmt.Sprintf("revision %d of the agreement", answer.Revision)
	}
	who := identity.NameOf(user.Username, user.DisplayName)
	summary := map[directory.ResponseKind]string{
		directory.Accept:         who + " accepted " + what,
		directory.Decline:        who + " declined " + what,
		directory.RequestChanges: who + " asked for changes to " + request.Part + " in " + what + ": " + request.Comment,
	}[directory.ResponseKind(request.Kind)]
	switch {
	case answer.Agreed && answer.Revision > 0:
		summary = fmt.Sprintf("Both parties accepted revision %d. The revised agreement is now in force.", answer.Revision)
	case answer.Agreed:
		summary = "Both parties accepted the same proposal. The agreement is recorded."
	case answer.Closed:
		summary += ". The agreement in force is unchanged."
	}
	api.announce(ctx, engagement, user.ID, summary)
	return c.JSON(http.StatusCreated, api.engagementViewOf(ctx, engagement, user.ID))
}

func (api *directoryAPI) handleStartWork(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	engagement, err := api.directory.StartWork(ctx, id, user.ID, user.Username)
	if err != nil {
		return agreementError(err)
	}
	api.announce(ctx, engagement, user.ID, identity.NameOf(user.Username, user.DisplayName)+" started the agreed work")
	return c.JSON(http.StatusOK, api.engagementViewOf(ctx, engagement, user.ID))
}

type completeRequest struct {
	Note string `json:"note"`
}

func (api *directoryAPI) handleRequestCompletion(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	var request completeRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	engagement, err := api.directory.RequestCompletion(ctx, id, user.ID, user.Username, request.Note)
	if err != nil {
		return agreementError(err)
	}
	api.announce(ctx, engagement, user.ID, identity.NameOf(user.Username, user.DisplayName)+" says the work is complete, and asks you to confirm")
	return c.JSON(http.StatusOK, api.engagementViewOf(ctx, engagement, user.ID))
}

func (api *directoryAPI) handleConfirmCompletion(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	engagement, err := api.directory.ConfirmCompletion(ctx, id, user.ID, user.Username)
	if err != nil {
		return agreementError(err)
	}
	api.announce(ctx, engagement, user.ID, identity.NameOf(user.Username, user.DisplayName)+" confirmed the work is complete")
	return c.JSON(http.StatusOK, api.engagementViewOf(ctx, engagement, user.ID))
}

func (api *directoryAPI) handleExportAgreement(c *echo.Context) error {
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	export, err := api.directory.ExportAgreement(c.Request().Context(), id, user.ID, api.config.CanonicalAddress)
	if err != nil {
		return agreementError(err)
	}
	c.Response().Header().Set("Content-Disposition", `attachment; filename="agreement-`+id.String()+`.json"`)
	return c.JSON(http.StatusOK, export)
}

type openRevisionRequest struct {
	Reason string `json:"reason"`
	proposalRequest
}

/*
handleOpenRevision proposes a revision of the agreement in force: why, and the
amended terms, which become the revision's first version. The agreement in
force stays in force until both parties accept one version of the revision.
*/
func (api *directoryAPI) handleOpenRevision(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	var request openRevisionRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	revision, version, engagement, err := api.directory.OpenRevision(ctx, id, user.ID, user.Username, request.Reason,
		directory.ProposalInput{
			Scope: request.Scope, Timeline: request.Timeline, LineItems: request.LineItems, Currency: request.Currency,
		})
	if err != nil {
		return agreementError(err)
	}
	api.announce(ctx, engagement, user.ID, fmt.Sprintf(
		"%s proposed revision %d of the agreement (version %d): %s. The agreement in force stands until you both accept.",
		identity.NameOf(user.Username, user.DisplayName), revision.Number, version.Version, revision.Reason))
	return c.JSON(http.StatusCreated, map[string]any{
		"revision": revisionViewOf(revision), "version": versionViewOf(version),
	})
}

type withdrawRevisionRequest struct {
	Reason string `json:"reason"`
}

func (api *directoryAPI) handleWithdrawRevision(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	id, err := engagementIDParam(c)
	if err != nil {
		return err
	}
	number, err := strconv.ParseInt(c.Param("number"), 10, 32)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, directory.ErrNoRevision.Error())
	}
	var request withdrawRevisionRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	revision, engagement, err := api.directory.WithdrawRevision(ctx, id, user.ID, user.Username, int32(number), request.Reason)
	if err != nil {
		return agreementError(err)
	}
	api.announce(ctx, engagement, user.ID, fmt.Sprintf(
		"%s withdrew revision %d of the agreement. The agreement in force is unchanged.", identity.NameOf(user.Username, user.DisplayName), revision.Number))
	return c.JSON(http.StatusOK, revisionViewOf(revision))
}

func agreementError(err error) error {
	switch {
	case errors.Is(err, directory.ErrNoAgreement), errors.Is(err, directory.ErrNoProposal),
		errors.Is(err, directory.ErrNoRevision):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, directory.ErrNotLatest), errors.Is(err, directory.ErrAlreadyAccepted),
		errors.Is(err, directory.ErrRevisionOpen):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, directory.ErrOwnCompletion), errors.Is(err, directory.ErrNotOpener):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	return engagementError(err)
}
