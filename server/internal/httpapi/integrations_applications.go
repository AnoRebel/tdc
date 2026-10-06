package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/applications"
	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Applications: programs installed on the server as themselves.

Two halves. Managing them is an administrator's business — registering,
approving with the exact permissions shown, restricting to channels, issuing and
revoking credentials, removing. Knowing about them is everybody's: which are
installed and what each may do, and in any channel, which can act there.

And one gate every request made with an application's credential passes
through, which is where an application's authority is decided: its grant, in its
channels, and nothing else — whatever its account's roles say and whoever
approved it.
*/

// contextKeyApplication marks a request made by an application.
const contextKeyApplication = "tdc.application"

func init() {
	registerRoutes(func(s *Server) {
		// Every request, after authentication: a request made with an
		// application's credential is held to the application's grant.
		s.echo.Use(s.applicationGate)

		manage := s.echo.Group("/api/admin/applications", requireAuthentication,
			requirePermission(authz.PermApplicationsManage))
		manage.GET("", s.handleListApplications)
		manage.POST("", s.handleRegisterApplication)
		// What an application can be granted, for whoever registers one —
		// who need not be able to manage roles to read the list.
		manage.GET("/permissions", s.handleApplicationPermissions)
		manage.GET("/:appId", s.handleGetApplication)
		manage.POST("/:appId/approve", s.handleApproveApplication)
		manage.PUT("/:appId/channels", s.handleRestrictApplication)
		manage.POST("/:appId/credentials", s.handleIssueApplicationCredential)
		manage.DELETE("/:appId/credentials/:tokenId", s.handleRevokeApplicationCredential)
		manage.POST("/:appId/remove", s.handleRemoveApplication)

		s.echo.GET("/api/applications", s.handleInstalledApplications, requireAuthentication)
		s.echo.GET("/api/applications/me", s.handleApplicationItself, requireAuthentication)
		s.echo.GET("/api/rooms/:id/applications", s.handleApplicationsInRoom, requireAuthentication)
	})

	manage := "server.applications.manage"
	documentRoutes(map[string]entry{
		"GET /api/admin/applications":                                {Summary: "List applications", Authentication: "session or token", Permission: manage},
		"POST /api/admin/applications":                               {Summary: "Register an application", Authentication: "session or token", Permission: manage},
		"GET /api/admin/applications/permissions":                    {Summary: "Permissions an application can be granted", Authentication: "session or token", Permission: manage},
		"GET /api/admin/applications/:appId":                         {Summary: "An application, its channels and credentials", Authentication: "session or token", Permission: manage},
		"POST /api/admin/applications/:appId/approve":                {Summary: "Approve an application", Authentication: "session or token", Permission: manage},
		"PUT /api/admin/applications/:appId/channels":                {Summary: "Restrict an application to channels", Authentication: "session or token", Permission: manage},
		"POST /api/admin/applications/:appId/credentials":            {Summary: "Issue an application another credential", Authentication: "session or token", Permission: manage},
		"DELETE /api/admin/applications/:appId/credentials/:tokenId": {Summary: "Revoke an application's credential", Authentication: "session or token", Permission: manage},
		"POST /api/admin/applications/:appId/remove":                 {Summary: "Remove an application", Authentication: "session or token", Permission: manage},
		"GET /api/applications":                                      {Summary: "Applications installed on this server", Authentication: "session or token"},
		"GET /api/applications/me":                                   {Summary: "The application making the request", Authentication: "session or token"},
		"GET /api/rooms/:id/applications":                            {Summary: "Applications that can act in a channel", Authentication: "session or token"},
	})
}

/*
applicationRoutes are the routes outside a channel an application may call.

An allowlist, so a route added later is closed to applications until somebody
decides it should be open — the opposite default would make every new endpoint a
thing applications could do the day it shipped.
*/
var applicationRoutes = map[string]bool{
	"GET /api/me":                      true,
	"GET /api/me/permissions":          true,
	"GET /api/applications/me":         true,
	"GET /api/live":                    true,
	"GET /api/attachments/:id":         true,
	"GET /api/attachments/:id/preview": true,
	"GET /api/server":                  true,
	"GET /api/version":                 true,
	"GET /api/health":                  true,
	"GET /api/description":             true,
	"GET /api/openapi.json":            true,
}

/*
applicationGate holds a request made with an application's credential to the
application's grant.

The permissions the request carries are replaced with the application's, so
every check downstream — an administrative route's own permission, a restricted
control — is asked of the application rather than of its account or anybody
else. Then the route itself is checked: in a channel, the channel must be one
of the application's and the action one its grant allows there; elsewhere, the
route must be one applications may use at all.
*/
func (s *Server) applicationGate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		token, ok := currentAPIToken(c)
		if !ok || token.ApplicationID == nil {
			return next(c)
		}
		ctx := c.Request().Context()

		service := s.applicationService()
		if service == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "authentication is required")
		}
		application, err := service.Get(ctx, *token.ApplicationID)
		if err != nil || application.State != "approved" {
			// Removed between the credential being checked and now. Its
			// credentials were revoked with it, so this is a race of
			// milliseconds — refused all the same.
			return echo.NewHTTPError(http.StatusUnauthorized, "authentication is required")
		}

		permissions, err := service.Permissions(ctx, application)
		if err != nil {
			return err
		}
		c.Set(contextKeyPermissions, permissions)
		c.Set(contextKeyApplication, application)

		if refusal := s.throttleApplication(c, application); refusal != nil {
			return refusal
		}
		return s.applicationMayUse(c, application, permissions, next)
	}
}

func (s *Server) applicationMayUse(
	c *echo.Context,
	application dbgen.Application,
	permissions authz.Permissions,
	next echo.HandlerFunc,
) error {
	route := c.RouteInfo().Path
	method := c.Request().Method

	switch {
	case strings.HasPrefix(route, "/api/admin/"):
		// Each administrative route checks its own permission, now against
		// the application's grant — which is all the gate needs to decide.
		return next(c)

	case route == "/api/rooms/:id/join" || route == "/api/rooms/:id/leave" ||
		route == "/api/rooms/:id/remote/join":
		return echo.NewHTTPError(http.StatusForbidden,
			"an application's channels are set by whoever approved it")

	case strings.HasPrefix(route, "/api/rooms/:id"):
		roomID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "no such room")
		}
		channels, err := s.applicationService().Channels(c.Request().Context(), application.ID)
		if err != nil {
			return err
		}
		if !slices.Contains(channels, roomID) {
			return echo.NewHTTPError(http.StatusForbidden, "this application may not act in that channel")
		}

		needed := authz.PermChannelPost
		switch {
		case method == http.MethodGet:
			needed = authz.PermChannelRead
		case route == "/api/rooms/:id/attachments":
			needed = authz.PermAttachmentUpload
		}
		if decision := permissions.AllowsChannel(needed, roomID); !decision.Allowed {
			return echo.NewHTTPError(http.StatusForbidden, decision.Error())
		}
		return next(c)

	case applicationRoutes[method+" "+route]:
		return next(c)

	default:
		return echo.NewHTTPError(http.StatusForbidden, "applications cannot use that")
	}
}

/*
throttleApplication holds one application to its rate, across all its
credentials.

Counted per application rather than per credential, so issuing itself more
credentials does not buy an application more requests, and recorded on it so
whoever manages it sees that it was throttled.
*/
func (s *Server) throttleApplication(c *echo.Context, application dbgen.Application) error {
	if s.limiter == nil || s.settings == nil {
		return nil
	}
	ctx := c.Request().Context()
	perMinute := s.settings.Int(ctx, settings.ApplicationRequestsPerMinute)
	if perMinute <= 0 {
		return nil
	}
	decision, err := s.limiter.Allow(ctx, "application:"+application.ID.String(),
		cache.RateLimit{Attempts: int(perMinute), Window: time.Minute})
	if err != nil || decision.Allowed {
		return nil
	}
	if err := s.queries.RecordApplicationRateLimited(ctx, application.ID); err != nil {
		s.log.Warn("could not record an application being throttled", "application", application.ID, "error", err)
	}
	return retryAfter(c, decision.RetryAfter)
}

// channelRef names a channel an application may act in.
type channelRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// applicationView is an application as an administrator manages it.
type applicationView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
	State   string `json:"state"`
	// Requested is what it asks for; Granted what it was approved for.
	Requested           []string   `json:"requested"`
	Granted             []string   `json:"granted"`
	SendsDataExternally bool       `json:"sendsDataExternally"`
	ExternalDisclosure  string     `json:"externalDisclosure,omitempty"`
	Registered          time.Time  `json:"registered"`
	Approved            *time.Time `json:"approved,omitempty"`
	Account             string     `json:"account,omitempty"`
	// AccountID is the account it acts through, which moderation acts on.
	AccountID       string       `json:"accountId,omitempty"`
	Channels        []channelRef `json:"channels"`
	Credentials     []credential `json:"credentials,omitempty"`
	RateLimited     int64        `json:"rateLimited"`
	LastRateLimited *time.Time   `json:"lastRateLimited,omitempty"`
	// InvocationURL is where presses on its messages are sent, if anywhere.
	InvocationURL string `json:"invocationUrl,omitempty"`
}

type credential struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Created  time.Time  `json:"created"`
	LastUsed *time.Time `json:"lastUsed,omitempty"`
}

func (s *Server) viewOfApplication(c *echo.Context, application dbgen.Application, withCredentials bool) (applicationView, error) {
	ctx := c.Request().Context()
	view := applicationView{
		ID:                  application.ID.String(),
		Name:                application.Name,
		Purpose:             application.Purpose,
		State:               application.State,
		Requested:           application.RequestedPermissions,
		Granted:             application.GrantedPermissions,
		SendsDataExternally: application.SendsDataExternally,
		ExternalDisclosure:  application.ExternalDisclosure,
		Registered:          application.RegisteredAt,
		Approved:            application.ApprovedAt,
		Channels:            []channelRef{},
		RateLimited:         application.RateLimitedCount,
		LastRateLimited:     application.LastRateLimitedAt,
		InvocationURL:       application.InvocationUrl,
	}
	if application.AccountID != nil {
		view.AccountID = application.AccountID.String()
		if account, err := s.queries.GetUserByID(ctx, *application.AccountID); err == nil {
			view.Account = s.rooms.UserAddress(account.Username)
		}
	}

	channels, err := s.queries.ListApplicationChannels(ctx, application.ID)
	if err != nil {
		return view, err
	}
	for _, roomID := range channels {
		if room, err := s.queries.GetRoomByID(ctx, roomID); err == nil {
			view.Channels = append(view.Channels, channelRef{ID: roomID.String(), Name: room.Name})
		}
	}

	if withCredentials {
		tokens, err := s.queries.ListApplicationTokens(ctx, &application.ID)
		if err != nil {
			return view, err
		}
		for _, token := range tokens {
			view.Credentials = append(view.Credentials, credential{
				ID: token.ID.String(), Label: token.Label, Created: token.CreatedAt, LastUsed: token.LastUsedAt,
			})
		}
	}
	return view, nil
}

func (s *Server) handleListApplications(c *echo.Context) error {
	if s.applicationService() == nil {
		return applicationsUnavailable()
	}
	listed, err := s.queries.ListApplications(c.Request().Context())
	if err != nil {
		return err
	}
	views := make([]applicationView, 0, len(listed))
	for _, application := range listed {
		view, err := s.viewOfApplication(c, application, false)
		if err != nil {
			return err
		}
		views = append(views, view)
	}
	return c.JSON(http.StatusOK, views)
}

type registerApplicationRequest struct {
	Name                string   `json:"name"`
	Purpose             string   `json:"purpose"`
	Permissions         []string `json:"permissions"`
	SendsDataExternally bool     `json:"sendsDataExternally"`
	ExternalDisclosure  string   `json:"externalDisclosure"`
}

func (s *Server) handleRegisterApplication(c *echo.Context) error {
	user, _ := currentUser(c)
	service := s.applicationService()
	if service == nil {
		return applicationsUnavailable()
	}

	var request registerApplicationRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	application, err := service.Register(c.Request().Context(), user.ID, applications.NewApplication{
		Name:                request.Name,
		Purpose:             request.Purpose,
		Permissions:         request.Permissions,
		SendsDataExternally: request.SendsDataExternally,
		ExternalDisclosure:  request.ExternalDisclosure,
	})
	if err != nil {
		return applicationError(err)
	}

	s.recordModeration(c, user, audit.ActionApplicationRegistered, "application", application.ID.String(),
		map[string]any{"name": application.Name, "requested": application.RequestedPermissions,
			"sendsDataExternally": application.SendsDataExternally})

	view, err := s.viewOfApplication(c, application, false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, view)
}

func (s *Server) applicationParam(c *echo.Context) (dbgen.Application, error) {
	service := s.applicationService()
	if service == nil {
		return dbgen.Application{}, applicationsUnavailable()
	}
	id, err := uuid.Parse(c.Param("appId"))
	if err != nil {
		return dbgen.Application{}, applicationError(applications.ErrNotFound)
	}
	application, err := service.Get(c.Request().Context(), id)
	if err != nil {
		return dbgen.Application{}, applicationError(err)
	}
	return application, nil
}

func (s *Server) handleGetApplication(c *echo.Context) error {
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}
	view, err := s.viewOfApplication(c, application, true)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

type approveRequest struct {
	// Permissions repeats what the approver was shown. It must be what the
	// application asks for, exactly, or approval is refused.
	Permissions []string `json:"permissions"`
	Channels    []string `json:"channels"`
}

// issuedCredential is the one response that carries an application's secret.
type issuedCredential struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Token string `json:"token"`
}

type approvalResponse struct {
	applicationView
	Credential issuedCredential `json:"credential"`
}

func parseChannels(raw []string) ([]uuid.UUID, error) {
	channels := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "a channel was not named by its identifier")
		}
		if !slices.Contains(channels, id) {
			channels = append(channels, id)
		}
	}
	return channels, nil
}

func (s *Server) handleApproveApplication(c *echo.Context) error {
	user, _ := currentUser(c)
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}

	var request approveRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	channels, err := parseChannels(request.Channels)
	if err != nil {
		return err
	}

	/*
		The approver's own permissions, read afresh rather than from the
		request: a token used to approve carries a narrowed set, and an
		approval must not grant more than the person behind it holds.
	*/
	approval, err := s.applicationService().Approve(c.Request().Context(), application.ID, user,
		currentPermissions(c), request.Permissions, channels)
	if err != nil {
		return applicationError(err)
	}

	s.recordModeration(c, user, audit.ActionApplicationApproved, "application", application.ID.String(),
		map[string]any{"name": application.Name, "granted": approval.Application.GrantedPermissions,
			"channels": request.Channels})

	view, err := s.viewOfApplication(c, approval.Application, false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, approvalResponse{
		applicationView: view,
		Credential: issuedCredential{
			ID: approval.Token.ID.String(), Label: approval.Token.Label, Token: approval.Secret,
		},
	})
}

type restrictRequest struct {
	Channels []string `json:"channels"`
}

func (s *Server) handleRestrictApplication(c *echo.Context) error {
	user, _ := currentUser(c)
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}
	if application.State != "approved" {
		return applicationError(applications.ErrNotRegistered)
	}

	var request restrictRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	channels, err := parseChannels(request.Channels)
	if err != nil {
		return err
	}
	for _, roomID := range channels {
		if _, err := s.queries.GetRoomByID(c.Request().Context(), roomID); err != nil {
			return applicationError(applications.ErrUnknownChannel)
		}
	}
	if err := s.applicationService().Restrict(c.Request().Context(), application, channels); err != nil {
		return applicationError(err)
	}

	s.recordModeration(c, user, audit.ActionApplicationRestricted, "application", application.ID.String(),
		map[string]any{"name": application.Name, "channels": request.Channels})

	view, err := s.viewOfApplication(c, application, true)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

func (s *Server) handleIssueApplicationCredential(c *echo.Context) error {
	user, _ := currentUser(c)
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}
	token, secret, err := s.applicationService().IssueCredential(c.Request().Context(), application)
	if err != nil {
		return applicationError(err)
	}
	s.recordModeration(c, user, audit.ActionApplicationCredentialIssued, "application", application.ID.String(),
		map[string]any{"name": application.Name, "credential": token.ID.String()})
	return c.JSON(http.StatusCreated, issuedCredential{ID: token.ID.String(), Label: token.Label, Token: secret})
}

func (s *Server) handleRevokeApplicationCredential(c *echo.Context) error {
	user, _ := currentUser(c)
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}
	tokenID, err := uuid.Parse(c.Param("tokenId"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such credential")
	}
	revoked, err := s.queries.RevokeApplicationToken(c.Request().Context(), dbgen.RevokeApplicationTokenParams{
		ID: tokenID, ApplicationID: &application.ID,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such credential")
	}
	s.recordModeration(c, user, audit.ActionApplicationCredentialRevoke, "application", application.ID.String(),
		map[string]any{"name": application.Name, "credential": revoked.ID.String()})
	return c.NoContent(http.StatusNoContent)
}

type removeRequest struct {
	// Content is "retain" or "remove": what happens to what it posted.
	Content string `json:"content"`
}

func (s *Server) handleRemoveApplication(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()
	application, err := s.applicationParam(c)
	if err != nil {
		return err
	}

	var request removeRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	removal, err := s.applicationService().Remove(ctx, application.ID, user, applications.Disposition(request.Content))
	if err != nil {
		return applicationError(err)
	}

	// The removals are announced like any moderator's, so open clients take
	// the messages down without reloading.
	for _, event := range removal.Removed {
		s.publishEvent(ctx, event)
	}

	s.recordModeration(c, user, audit.ActionApplicationRemoved, "application", application.ID.String(),
		map[string]any{"name": application.Name, "content": request.Content,
			"messagesRemoved": len(removal.Removed)})
	return c.JSON(http.StatusOK, map[string]any{"removed": true, "messagesRemoved": len(removal.Removed)})
}

// installedView is an application as anybody on the server may see it: what it
// is, what it may do, when it was approved, and what it sends elsewhere.
type installedView struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Purpose             string    `json:"purpose"`
	Permissions         []string  `json:"permissions"`
	Approved            time.Time `json:"approved"`
	SendsDataExternally bool      `json:"sendsDataExternally"`
	ExternalDisclosure  string    `json:"externalDisclosure,omitempty"`
	Account             string    `json:"account,omitempty"`
	// ChannelCount says how widely it acts without naming channels a reader
	// may not be able to see.
	ChannelCount int `json:"channelCount"`
	// InvocationHost is where presses on its messages are sent, if anywhere.
	InvocationHost string `json:"invocationHost,omitempty"`
}

func (s *Server) installedViewOf(c *echo.Context, application dbgen.Application) installedView {
	view := installedView{
		ID: application.ID.String(), Name: application.Name, Purpose: application.Purpose,
		Permissions: application.GrantedPermissions, SendsDataExternally: application.SendsDataExternally,
		ExternalDisclosure: application.ExternalDisclosure,
		InvocationHost:     invocationHost(application),
	}
	if application.ApprovedAt != nil {
		view.Approved = *application.ApprovedAt
	}
	if application.AccountID != nil {
		if account, err := s.queries.GetUserByID(c.Request().Context(), *application.AccountID); err == nil {
			view.Account = s.rooms.UserAddress(account.Username)
		}
	}
	if channels, err := s.queries.ListApplicationChannels(c.Request().Context(), application.ID); err == nil {
		view.ChannelCount = len(channels)
	}
	return view
}

// handleInstalledApplications lists every approved application, for anybody.
func (s *Server) handleInstalledApplications(c *echo.Context) error {
	installed, err := s.queries.ListApprovedApplications(c.Request().Context())
	if err != nil {
		return err
	}
	views := make([]installedView, 0, len(installed))
	for _, application := range installed {
		views = append(views, s.installedViewOf(c, application))
	}
	return c.JSON(http.StatusOK, views)
}

/*
handleApplicationsInRoom tells a channel's readers which applications can act in
it.

Anybody who can see the channel may ask, without administrator access: an
application that can read a conversation is something its participants are
entitled to know about.
*/
func (s *Server) handleApplicationsInRoom(c *echo.Context) error {
	user, _ := currentUser(c)
	roomID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such room")
	}
	if _, err := s.rooms.Visible(c.Request().Context(), roomID, user.ID); err != nil {
		return roomError(err)
	}
	present, err := s.queries.ListApplicationsInRoom(c.Request().Context(), roomID)
	if err != nil {
		return err
	}
	views := make([]installedView, 0, len(present))
	for _, application := range present {
		views = append(views, s.installedViewOf(c, application))
	}
	return c.JSON(http.StatusOK, views)
}

// handleApplicationItself tells an application about itself: what it was
// granted and where, which is what it needs to know where it may act.
func (s *Server) handleApplicationItself(c *echo.Context) error {
	application, ok := c.Get(contextKeyApplication).(dbgen.Application)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "this request was not made by an application")
	}
	view, err := s.viewOfApplication(c, application, false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

// grantableView is one permission as somebody registering an application reads
// it: what it is called, what it allows, and whether it reaches past channels.
type grantableView struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	PerChannel     bool   `json:"perChannel"`
	Administrative bool   `json:"administrative"`
}

func (s *Server) handleApplicationPermissions(c *echo.Context) error {
	catalogue := authz.Catalogue()
	views := make([]grantableView, 0, len(catalogue))
	for _, definition := range catalogue {
		views = append(views, grantableView{
			Name:           string(definition.Name),
			Description:    definition.Description,
			PerChannel:     authz.GrantableAt(definition.Name, authz.ScopeChannel),
			Administrative: definition.Administrative,
		})
	}
	return c.JSON(http.StatusOK, views)
}

func applicationsUnavailable() error {
	return echo.NewHTTPError(http.StatusNotFound, "this server does not offer applications")
}

func applicationError(err error) error {
	switch {
	case errors.Is(err, applications.ErrNotFound), errors.Is(err, applications.ErrUnknownChannel):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, applications.ErrInvalid), errors.Is(err, applications.ErrNoChannels):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, applications.ErrNotRegistered), errors.Is(err, applications.ErrRequestChanged):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, applications.ErrNotHeld):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	default:
		return err
	}
}
