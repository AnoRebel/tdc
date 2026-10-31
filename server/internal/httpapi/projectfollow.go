package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
Following a public project from another server, and asking to join one.

On the project's home: the public projects a trusted peer may follow, the
peer's following and unfollowing, and somebody of the peer's asking to join —
which the project's administrators answer. On the server following: browsing a
peer's projects, following one, and asking to join it, each a signed request
made on a person's behalf.
*/
func init() {
	registerRoutes(func(s *Server) {
		federated := s.echo.Group("/api/federation", s.requireFederation, s.requireSignature, s.requireProjects)
		federated.GET("/projects", s.handleFederatedProjectList)
		federated.POST("/projects/follow", s.handleProjectFollow)
		federated.POST("/projects/unfollow", s.handleProjectUnfollow)
		federated.POST("/projects/join", s.handleProjectJoinRequest)

		elsewhere := s.echo.Group("/api/projects-elsewhere", requireAuthentication, s.requireProjects)
		elsewhere.GET("", s.handleBrowsePeerProjects)
		elsewhere.POST("/follow", s.handleFollowPeerProject)
		elsewhere.POST("/unfollow", s.handleUnfollowPeerProject)

		group := s.echo.Group("/api/projects", requireAuthentication, s.requireProjects)
		group.POST("/:key/join", s.handleAskToJoin)
		group.GET("/:key/join-requests", s.handleListJoinRequests)
		group.POST("/:key/join-requests/:address", s.handleAcceptJoinRequest)
		group.DELETE("/:key/join-requests/:address", s.handleDeclineJoinRequest)
	})

	documentRoutes(map[string]entry{
		"GET /api/projects-elsewhere":                      {Summary: "Browse the public projects another server lets this one add", Authentication: "session or token"},
		"POST /api/projects-elsewhere/follow":              {Summary: "Add a public project on another server, holding a copy here", Authentication: "session or token", Permission: "project.create"},
		"POST /api/projects-elsewhere/unfollow":            {Summary: "Remove a project of another server's from this one", Authentication: "session or token", Permission: "project.create"},
		"POST /api/projects/:key/join":                     {Summary: "Ask a shared project's home to be let in as a member", Authentication: "session or token"},
		"GET /api/projects/:key/join-requests":             {Summary: "Who on other servers asked to join a project", Authentication: "session or token", Permission: "project.administer"},
		"POST /api/projects/:key/join-requests/:address":   {Summary: "Let somebody who asked into a project, as a member", Authentication: "session or token", Permission: "project.administer"},
		"DELETE /api/projects/:key/join-requests/:address": {Summary: "Decline somebody's asking to join a project", Authentication: "session or token", Permission: "project.administer"},
	})
}

// The home's half.

// handleFederatedProjectList serves a peer the projects it may follow — none
// to a peer this server is not discoverable by, said as an empty list.
func (s *Server) handleFederatedProjectList(c *echo.Context) error {
	if !s.mayDiscover(c) {
		return c.JSON(http.StatusOK, map[string]any{"projects": []any{}})
	}
	published, err := s.projects.Published(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"projects": published})
}

type followRequest struct {
	Project uuid.UUID `json:"project"`
}

// handleProjectFollow takes the signing server as a follower of a public
// project, to be offered a replica of it. A project that is not public and
// open answers as one that does not exist.
func (s *Server) handleProjectFollow(c *echo.Context) error {
	origin, server := verifiedOrigin(c)
	var request followRequest
	if err := c.Bind(&request); err != nil || server == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the request")
	}
	if !s.mayDiscover(c) {
		return echo.NewHTTPError(http.StatusNotFound, "no such project")
	}
	if err := s.projects.Follow(c.Request().Context(), request.Project, server, origin); err != nil {
		return projectError(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"following": true})
}

// handleProjectUnfollow stops the signing server following a project; its
// replica is discarded unless people of that server are members.
func (s *Server) handleProjectUnfollow(c *echo.Context) error {
	_, server := verifiedOrigin(c)
	var request followRequest
	if err := c.Bind(&request); err != nil || server == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the request")
	}
	if err := s.projects.Unfollow(c.Request().Context(), request.Project, server); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"following": false})
}

/*
handleProjectJoinRequest records somebody on the signing server asking to be
let into a project they read there. As with a change, the server must speak for
them and hold a replica. The project's administrators see who is waiting on
the project itself.
*/
func (s *Server) handleProjectJoinRequest(c *echo.Context) error {
	ctx := c.Request().Context()
	origin, server := verifiedOrigin(c)
	var request struct {
		Project uuid.UUID `json:"project"`
		Actor   string    `json:"actor"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the request")
	}
	if err := s.peerSpeaksFor(origin, request.Actor); err != nil {
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	if holds, err := s.projects.HoldsReplica(ctx, request.Project, server); err != nil || !holds {
		return echo.NewHTTPError(http.StatusNotFound, "no such project")
	}
	if _, err := s.projects.RemoteAccess(ctx, request.Project, request.Actor); err == nil {
		return echo.NewHTTPError(http.StatusConflict, "they are already a member")
	}
	if _, err := s.projects.AskToJoin(ctx, request.Project, request.Actor); err != nil {
		return projectError(err)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"asked": true})
}

// handleListJoinRequests lists who asked to join, for the project's
// administrators.
func (s *Server) handleListJoinRequests(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	requests, err := s.projects.JoinRequests(c.Request().Context(), access)
	if err != nil {
		return projectError(err)
	}
	type joinRequest struct {
		Address     string    `json:"address"`
		RequestedAt time.Time `json:"requestedAt"`
	}
	answer := make([]joinRequest, 0, len(requests))
	for _, request := range requests {
		answer = append(answer, joinRequest{Address: request.Address, RequestedAt: request.RequestedAt})
	}
	return c.JSON(http.StatusOK, answer)
}

func (s *Server) handleAcceptJoinRequest(c *echo.Context) error {
	return s.decideJoinRequest(c, true)
}

func (s *Server) handleDeclineJoinRequest(c *echo.Context) error {
	return s.decideJoinRequest(c, false)
}

// decideJoinRequest answers somebody's asking; letting them in is adding a
// member, and recorded as one.
func (s *Server) decideJoinRequest(c *echo.Context, accept bool) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	address := c.Param("address")
	if accept {
		if err := s.remoteMemberRefusal(c, address); err != nil {
			return err
		}
	}
	if err := s.projects.DecideJoinRequest(ctx, actorOf(c), access, address, accept); err != nil {
		if errors.Is(err, projects.ErrJoinRequestNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return projectError(err)
	}
	if accept {
		user, _ := currentUser(c)
		if err := s.audit.Record(ctx, audit.Entry{
			ActorID: &user.ID, ActorLabel: user.Username,
			Action:     audit.ActionProjectMemberAdded,
			TargetKind: "project", TargetID: access.Project.ID.String(),
			Detail:   map[string]any{"member": address, "role": "member", "asked": true},
			SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
		}); err != nil {
			return err
		}
	}
	return c.NoContent(http.StatusNoContent)
}

// The follower's half.

/*
peerAt is the server somebody named, and where to reach it: an address as
written, scheme and port and all, or a host reached where this server already
knows it to be. Whether it may be reached at all is the trust settings' to say,
when it is.
*/
func (s *Server) peerAt(c *echo.Context, named string) (host, address string, err error) {
	if !s.federationEnabled(c) || s.config.CanonicalAddress == "" {
		return "", "", echo.NewHTTPError(http.StatusConflict, "this server does not federate")
	}
	named = strings.TrimRight(strings.TrimSpace(named), "/")
	host, err = federation.Host(named)
	if err != nil || named == "" {
		return "", "", echo.NewHTTPError(http.StatusBadRequest, "name a server, as example.org or https://example.org")
	}
	if strings.Contains(named, "://") {
		return host, named, nil
	}
	return host, s.peerAddressFor(c.Request().Context(), host), nil
}

// handleBrowsePeerProjects lists the projects another server lets this one
// follow — empty when it lists none to us, or cannot be reached.
func (s *Server) handleBrowsePeerProjects(c *echo.Context) error {
	host, address, err := s.peerAt(c, c.QueryParam("server"))
	if err != nil {
		return err
	}
	var answer struct {
		Projects []projects.PublishedProject `json:"projects"`
	}
	if err := s.callSender(c.Request().Context()).Get(c.Request().Context(), address, "/api/federation/projects", &answer); err != nil {
		s.log.Warn("could not browse a peer's projects", "peer", host, "error", err.Error())
		return c.JSON(http.StatusOK, []any{})
	}
	if answer.Projects == nil {
		answer.Projects = []projects.PublishedProject{}
	}
	return c.JSON(http.StatusOK, answer.Projects)
}

type followElsewhereRequest struct {
	Server  string    `json:"server"`
	Project uuid.UUID `json:"project"`
	Name    string    `json:"name"`
}

/*
handleFollowPeerProject follows a public project on another server, for
everybody here to read. Bringing a project in is starting one here, so it
takes the same permission; the copy arrives with the home's offer, moments
later.
*/
func (s *Server) handleFollowPeerProject(c *echo.Context) error {
	return s.followElsewhere(c, true)
}

// handleUnfollowPeerProject stops following a project; its home withdraws the
// copy unless people here are members.
func (s *Server) handleUnfollowPeerProject(c *echo.Context) error {
	return s.followElsewhere(c, false)
}

func (s *Server) followElsewhere(c *echo.Context, follow bool) error {
	if decision := currentPermissions(c).AllowsServer(authz.PermProjectCreate); !decision.Allowed {
		return echo.NewHTTPError(http.StatusForbidden, decision.Error())
	}
	var request followElsewhereRequest
	if err := c.Bind(&request); err != nil || request.Project == uuid.Nil() {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	host, address, err := s.peerAt(c, request.Server)
	if err != nil {
		return err
	}
	path := "/api/federation/projects/unfollow"
	if follow {
		path = "/api/federation/projects/follow"
	}
	if err := s.askPeer(c, address, path, map[string]any{"project": request.Project}); err != nil {
		return err
	}
	if follow {
		if err := s.projects.Following(c.Request().Context(), request.Project, request.Name, host); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"following": follow})
}

// handleAskToJoin asks a replica's home to let the caller in. Nothing changes
// here: if they are let in, they become a member as the home's events arrive.
func (s *Server) handleAskToJoin(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	if !projects.IsReplica(access.Project) {
		return echo.NewHTTPError(http.StatusConflict, "this project lives here; its administrators add people to it")
	}
	if access.Role != "" {
		return echo.NewHTTPError(http.StatusConflict, "you are already a member")
	}
	if !s.federationEnabled(c) {
		return echo.NewHTTPError(http.StatusConflict, "this server no longer federates")
	}
	user, _ := currentUser(c)
	address := s.peerAddressFor(c.Request().Context(), projects.HomeOf(access.Project))
	if err := s.askPeer(c, address, "/api/federation/projects/join", map[string]any{
		"project": access.Project.ID, "actor": s.rooms.UserAddress(user.Username),
	}); err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, map[string]any{"asked": true})
}

// askPeer sends a peer a signed request on a person's behalf, passing its
// refusal on in its own words.
func (s *Server) askPeer(c *echo.Context, address, path string, payload any) error {
	response, err := s.callSender(c.Request().Context()).Send(c.Request().Context(), address, path, payload)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the other server could not be reached: "+err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		var answer struct {
			Message string `json:"message"`
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		if decodeErr := json.Unmarshal(body, &answer); decodeErr != nil || answer.Message == "" {
			answer.Message = "the other server refused: " + response.Status
		}
		return echo.NewHTTPError(response.StatusCode, answer.Message)
	}
	return nil
}
