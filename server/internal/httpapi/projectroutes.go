package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
Projects and issues, over HTTP.

Every route answers a project or issue the caller cannot see exactly as it
answers one that does not exist — 404, with the same words — so neither a
private project's key nor the numbers of its issues can be discovered by
asking. A refusal to act on something the caller can see is a 403 naming the
permission that would have allowed it.
*/
func init() {
	registerRoutes(func(s *Server) {
		if s.db != nil && s.rooms != nil {
			s.projects = projects.NewService(s.db, s.rooms, s.log)
			// A project's history is a room's log, so its events reach
			// webhooks as every room's do — once committed.
			s.projects.OnAppended(s.queueWebhooks)
			/*
				An issue's discussion is as visible as the issue and no more.

				The rooms package asks this rather than keeping a membership
				of its own, so removing somebody from a private project
				removes the discussions with it, in the same moment.
			*/
			s.rooms.DelegateAccess("issue-discussion", func(ctx context.Context, room dbgen.Room, userID uuid.UUID) (bool, error) {
				return s.projects.DiscussionVisible(ctx, room.ID, userID)
			})
		}

		group := s.echo.Group("/api/projects", requireAuthentication, s.requireProjects)
		group.GET("", s.handleListProjects)
		group.POST("", s.handleCreateProject)
		group.GET("/:key", s.handleGetProject)
		group.PATCH("/:key", s.handleUpdateProject)
		group.GET("/:key/history", s.handleProjectHistory)
		group.GET("/:key/members", s.handleListProjectMembers)
		group.PUT("/:key/members/:username", s.handleSetProjectMember)
		group.DELETE("/:key/members/:username", s.handleRemoveProjectMember)
		group.PUT("/:key/federation", s.handleSetProjectFederation)
		group.POST("/:key/states", s.handleAddState)
		group.PUT("/:key/states", s.handleReorderStates)
		group.PATCH("/:key/states/:state", s.handleUpdateState)
		group.DELETE("/:key/states/:state", s.handleRemoveState)
		group.GET("/:key/issues", s.handleListIssues)
		group.POST("/:key/issues", s.handleCreateIssue)

		issues := s.echo.Group("/api/issues", requireAuthentication, s.requireProjects)
		issues.GET("/:ref", s.handleGetIssue)
		issues.PATCH("/:ref", s.handleEditIssue)
		issues.DELETE("/:ref", s.handleDeleteIssue)
		issues.POST("/:ref/state", s.handleTransitionIssue)
		issues.GET("/:ref/history", s.handleIssueHistory)

		s.echo.PUT("/api/admin/roles/:id/project-permissions", s.handleSetRoleProjectPermission,
			requireAuthentication, requirePermission(authz.PermRoleManage), s.requireProjects)
	})

	documentRoutes(map[string]entry{
		"GET /api/projects":                            {Summary: "List projects", Authentication: "session or token"},
		"POST /api/projects":                           {Summary: "Start a project", Authentication: "session or token", Permission: "project.create"},
		"GET /api/projects/:key":                       {Summary: "A project", Authentication: "session or token"},
		"PATCH /api/projects/:key":                     {Summary: "Rename a project or change its visibility", Authentication: "session or token", Permission: "project.administer"},
		"GET /api/projects/:key/members":               {Summary: "A project's members", Authentication: "session or token"},
		"GET /api/projects/:key/history":               {Summary: "What happened to a project itself, newest first", Authentication: "session or token"},
		"PUT /api/projects/:key/members/:username":     {Summary: "Add a project member or change their role", Authentication: "session or token", Permission: "project.administer"},
		"DELETE /api/projects/:key/members/:username":  {Summary: "Remove a project member", Authentication: "session or token", Permission: "project.administer"},
		"PUT /api/projects/:key/federation":            {Summary: "Open a project to other servers, or close it to them", Authentication: "session or token", Permission: "project.administer"},
		"POST /api/projects/:key/states":               {Summary: "Add a state", Authentication: "session or token", Permission: "project.configure_states"},
		"PUT /api/projects/:key/states":                {Summary: "Reorder states", Authentication: "session or token", Permission: "project.configure_states"},
		"PATCH /api/projects/:key/states/:state":       {Summary: "Rename a state", Authentication: "session or token", Permission: "project.configure_states"},
		"DELETE /api/projects/:key/states/:state":      {Summary: "Remove a state", Authentication: "session or token", Permission: "project.configure_states"},
		"GET /api/projects/:key/issues":                {Summary: "List a project's issues", Authentication: "session or token"},
		"POST /api/projects/:key/issues":               {Summary: "File an issue", Authentication: "session or token", Permission: "issue.create"},
		"GET /api/issues/:ref":                         {Summary: "An issue", Authentication: "session or token"},
		"PATCH /api/issues/:ref":                       {Summary: "Edit an issue's title or description", Authentication: "session or token"},
		"DELETE /api/issues/:ref":                      {Summary: "Delete an issue", Authentication: "session or token", Permission: "issue.delete"},
		"POST /api/issues/:ref/state":                  {Summary: "Move an issue to another state", Authentication: "session or token", Permission: "issue.transition"},
		"GET /api/issues/:ref/history":                 {Summary: "An issue's history", Authentication: "session or token"},
		"PUT /api/admin/roles/:id/project-permissions": {Summary: "Set a role's permission in one project", Authentication: "session or token", Permission: "role.manage"},
	})
}

// requireProjects answers 503 on a server built without a database, where
// there is nothing to serve projects from.
func (s *Server) requireProjects(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.projects == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "projects are not available on this server")
		}
		return next(c)
	}
}

// actorOf is the caller, with the permissions this request resolved.
func actorOf(c *echo.Context) projects.Actor {
	user, _ := currentUser(c)
	return projects.Actor{ID: user.ID, Permissions: currentPermissions(c)}
}

// projectError maps a projects failure to a response.
func projectError(err error) error {
	var refusal projects.RefusalError
	var conflict projects.ConflictError
	switch {
	case errors.As(err, &refusal):
		return echo.NewHTTPError(http.StatusForbidden, refusal.Error())
	case errors.As(err, &conflict):
		// Handled by the transition route, which says what the issue became;
		// anywhere else it is still a conflict.
		return echo.NewHTTPError(http.StatusConflict, conflict.Error())
	case errors.Is(err, projects.ErrProjectNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such project")
	case errors.Is(err, projects.ErrIssueNotFound),
		errors.Is(err, projects.ErrAlreadyDeleted):
		return echo.NewHTTPError(http.StatusNotFound, "no such issue")
	case errors.Is(err, projects.ErrOriginNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, projects.ErrStateNotFound):
		// Not 404: the issue exists and the request is about it. What is
		// wrong is the state it names, which is a bad request.
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, projects.ErrKeyTaken),
		errors.Is(err, projects.ErrStateNameTaken),
		errors.Is(err, projects.ErrStateInUse),
		errors.Is(err, projects.ErrLastState),
		errors.Is(err, projects.ErrLastAdmin),
		errors.Is(err, projects.ErrNotOpened):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, projects.ErrNoSuchPerson),
		errors.Is(err, projects.ErrNotAMember):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, projects.ErrInvalidKey),
		errors.Is(err, projects.ErrInvalidName),
		errors.Is(err, projects.ErrVisibility),
		errors.Is(err, projects.ErrStateName),
		errors.Is(err, projects.ErrTitle),
		errors.Is(err, projects.ErrDescription),
		errors.Is(err, projects.ErrMemberRole),
		errors.Is(err, projects.ErrRemoteAddress):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	default:
		return err
	}
}

// stateResponse is one of a project's states.
type stateResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Position     int32  `json:"position"`
	IsCompletion bool   `json:"isCompletion"`
}

func stateResponseOf(state dbgen.IssueState) stateResponse {
	return stateResponse{
		ID: state.ID.String(), Name: state.Name, Position: state.Position, IsCompletion: state.IsCompletion,
	}
}

/*
projectAbilities is what the caller may do in a project.

Sent so a client can offer what will work and leave out what will not, rather
than showing a button that answers 403. The server checks again on every
request regardless; this is a courtesy, not the authority.
*/
type projectAbilities struct {
	Administer      bool `json:"administer"`
	ConfigureStates bool `json:"configureStates"`
	CreateIssues    bool `json:"createIssues"`
	Transition      bool `json:"transition"`
	Assign          bool `json:"assign"`
	Label           bool `json:"label"`
	Relate          bool `json:"relate"`
	Delete          bool `json:"delete"`
	// Plan is making cycles and milestones; PlanIssues is putting issues
	// into them.
	Plan       bool `json:"plan"`
	PlanIssues bool `json:"planIssues"`
}

func abilitiesOf(access projects.Access) projectAbilities {
	return projectAbilities{
		Administer:      access.Allows(authz.PermProjectAdminister).Allowed,
		ConfigureStates: access.Allows(authz.PermProjectConfigureStates).Allowed,
		CreateIssues:    access.Allows(authz.PermIssueCreate).Allowed,
		Transition:      access.Allows(authz.PermIssueTransition).Allowed,
		Assign:          access.Allows(authz.PermIssueAssign).Allowed,
		Label:           access.Allows(authz.PermIssueLabel).Allowed,
		Relate:          access.Allows(authz.PermIssueRelate).Allowed,
		Delete:          access.Allows(authz.PermIssueDelete).Allowed,
		Plan:            access.Allows(authz.PermProjectPlan).Allowed,
		PlanIssues:      access.Allows(authz.PermIssuePlan).Allowed,
	}
}

// projectResponse is the public shape of a project.
type projectResponse struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	CreatedAt   time.Time `json:"createdAt"`
	// Role is the caller's: "admin", "member", or absent for somebody who
	// can see a public project without being in it.
	Role   string            `json:"role,omitempty"`
	States []stateResponse   `json:"states,omitempty"`
	Can    *projectAbilities `json:"can,omitempty"`
	// Its labels and priority scale, on a single project.
	Labels     []labelResponse    `json:"labels,omitempty"`
	Priorities []priorityResponse `json:"priorities,omitempty"`
	// The calendar its dates are on, an IANA zone, and what happens to
	// unfinished issues when a cycle ends: "carry-forward" or "remain".
	TimeZone    string `json:"timeZone"`
	CyclePolicy string `json:"cyclePolicy"`
	// Federated is whether it may be shared with other servers.
	Federated bool `json:"federated"`
	// Home is the server a replica lives on, absent for a project of this
	// server's own; ReplicaError is why a replica stopped following it.
	Home         string `json:"home,omitempty"`
	ReplicaError string `json:"replicaError,omitempty"`
}

func projectResponseOf(project dbgen.Project, role string) projectResponse {
	return projectResponse{
		ID:           project.ID.String(),
		Key:          project.Key,
		Name:         project.Name,
		Description:  project.Description,
		Visibility:   project.Visibility,
		CreatedAt:    project.CreatedAt,
		Role:         role,
		TimeZone:     project.TimeZone,
		CyclePolicy:  project.CyclePolicy,
		Federated:    project.Federated,
		Home:         projects.HomeOf(project),
		ReplicaError: project.ReplicaError,
	}
}

// fullProject is a project with its states and what the caller may do.
func (s *Server) fullProject(ctx context.Context, access projects.Access) (projectResponse, error) {
	response := projectResponseOf(access.Project, access.Role)
	states, err := s.projects.States(ctx, access.Project.ID)
	if err != nil {
		return projectResponse{}, err
	}
	response.States = make([]stateResponse, 0, len(states))
	for _, state := range states {
		response.States = append(response.States, stateResponseOf(state))
	}
	abilities := abilitiesOf(access)
	response.Can = &abilities

	labels, err := s.projects.Labels(ctx, access.Project.ID)
	if err != nil {
		return projectResponse{}, err
	}
	response.Labels = make([]labelResponse, 0, len(labels))
	for _, label := range labels {
		response.Labels = append(response.Labels, labelResponseOf(label))
	}
	priorities, err := s.projects.Priorities(ctx, access.Project.ID)
	if err != nil {
		return projectResponse{}, err
	}
	response.Priorities = make([]priorityResponse, 0, len(priorities))
	for _, priority := range priorities {
		response.Priorities = append(response.Priorities, priorityResponseOf(priority))
	}
	return response, nil
}

func (s *Server) handleListProjects(c *echo.Context) error {
	user, _ := currentUser(c)
	rows, err := s.projects.ListVisible(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	actor := actorOf(c)
	response := make([]projectResponse, 0, len(rows))
	for _, row := range rows {
		project := dbgen.Project{
			ID: row.ID, Key: row.Key, Name: row.Name, Description: row.Description,
			Visibility: row.Visibility, CreatedAt: row.CreatedAt,
		}
		listed := projectResponseOf(project, row.MyRole)
		// What the reader may do in each, so a client choosing where to file
		// something offers only the projects that would accept it.
		abilities := abilitiesOf(projects.AccessWithRole(actor, project, row.MyRole))
		listed.Can = &abilities
		response = append(response, listed)
	}
	return c.JSON(http.StatusOK, response)
}

type createProjectRequest struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	States      []struct {
		Name         string `json:"name"`
		IsCompletion bool   `json:"isCompletion"`
	} `json:"states,omitempty"`
}

func (s *Server) handleCreateProject(c *echo.Context) error {
	ctx := c.Request().Context()
	actor := actorOf(c)
	user, _ := currentUser(c)

	var request createProjectRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	input := projects.NewProject{
		Key: request.Key, Name: request.Name, Description: request.Description, Visibility: request.Visibility,
	}
	for _, state := range request.States {
		input.States = append(input.States, projects.NewState{Name: state.Name, IsCompletion: state.IsCompletion})
	}

	project, err := s.projects.Create(ctx, actor, input)
	if err != nil {
		return projectError(err)
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username,
		Action:     audit.ActionProjectCreated,
		TargetKind: "project", TargetID: project.ID.String(),
		Detail:   map[string]any{"key": project.Key, "visibility": project.Visibility},
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	access, err := s.projects.ProjectByID(ctx, actor, project.ID)
	if err != nil {
		return projectError(err)
	}
	response, err := s.fullProject(ctx, access)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, response)
}

// projectFrom resolves the project named in the path, or answers as if it
// did not exist.
func (s *Server) projectFrom(c *echo.Context) (projects.Access, error) {
	access, err := s.projects.ProjectByKey(c.Request().Context(), actorOf(c), c.Param("key"))
	if err != nil {
		return projects.Access{}, projectError(err)
	}
	return access, nil
}

func (s *Server) handleGetProject(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	response, err := s.fullProject(c.Request().Context(), access)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Visibility  *string `json:"visibility"`
}

func (s *Server) handleUpdateProject(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}

	var request updateProjectRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	updated, err := s.projects.Update(ctx, actorOf(c), access, projects.ProjectChanges{
		Name: request.Name, Description: request.Description, Visibility: request.Visibility,
	})
	if err != nil {
		return projectError(err)
	}

	if updated.Visibility != access.Project.Visibility {
		user, _ := currentUser(c)
		if err := s.audit.Record(ctx, audit.Entry{
			ActorID: &user.ID, ActorLabel: user.Username,
			Action:     audit.ActionProjectVisibilityChanged,
			TargetKind: "project", TargetID: updated.ID.String(),
			Detail:   map[string]any{"previous": access.Project.Visibility, "new": updated.Visibility},
			SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
		}); err != nil {
			return err
		}
	}

	access.Project = updated
	response, err := s.fullProject(ctx, access)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

type projectMemberResponse struct {
	// Username is somebody here's username, or the address of somebody on
	// another server — what the member routes take to name them.
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Address     string    `json:"address"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
	// Remote is somebody on another server.
	Remote bool `json:"remote,omitempty"`
}

func (s *Server) handleListProjectMembers(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	members, err := s.projects.Members(c.Request().Context(), access)
	if err != nil {
		return err
	}
	response := make([]projectMemberResponse, 0, len(members))
	for _, member := range members {
		response = append(response, projectMemberResponse{
			Username: member.Username, DisplayName: member.DisplayName,
			Address: s.rooms.UserAddress(member.Username), Role: member.Role, JoinedAt: member.JoinedAt,
		})
	}
	remote, err := s.projects.RemoteMembers(c.Request().Context(), access)
	if err != nil {
		return err
	}
	for _, member := range remote {
		// Called by the name half of their address: their server has told
		// this one nothing more about them.
		name, _, _ := strings.Cut(member.Address, "@")
		response = append(response, projectMemberResponse{
			Username: member.Address, DisplayName: name, Address: member.Address,
			Role: member.Role, JoinedAt: member.JoinedAt, Remote: true,
		})
	}
	return c.JSON(http.StatusOK, response)
}

/*
remoteMemberRefusal refuses naming somebody on another server while this server
does not federate, or does not federate with theirs. Nil for somebody here.

Checked here rather than in the projects package, which knows nothing of this
server's settings: a membership the server would never act on is refused
before it is recorded.
*/
func (s *Server) remoteMemberRefusal(c *echo.Context, who string) error {
	_, host, found := strings.Cut(who, "@")
	if !found || strings.EqualFold(host, s.rooms.ServerName()) {
		return nil
	}
	if !s.federationEnabled(c) {
		return echo.NewHTTPError(http.StatusConflict, "this server does not federate, so it cannot hold people from other servers")
	}
	if allowed, _ := s.federationTrust(c.Request().Context()).Allows(strings.ToLower(host)); !allowed {
		return echo.NewHTTPError(http.StatusForbidden, "this server does not federate with "+host)
	}
	return nil
}

func (s *Server) handleSetProjectMember(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		Role string `json:"role"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if err := s.remoteMemberRefusal(c, c.Param("username")); err != nil {
		return err
	}
	if err := s.projects.SetMember(ctx, actorOf(c), access, c.Param("username"), request.Role); err != nil {
		return projectError(err)
	}

	user, _ := currentUser(c)
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username,
		Action:     audit.ActionProjectMemberAdded,
		TargetKind: "project", TargetID: access.Project.ID.String(),
		Detail:   map[string]any{"member": c.Param("username"), "role": request.Role},
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

/*
handleSetProjectFederation opens a project to other servers, or closes it.

The project's administrators decide, not the operator: the operator decided
when they turned federation on whether anything here may be shared at all, so
opening one project is only possible on a server that federates. Closing always
is, so a project can be withdrawn after federation has been turned off.
*/
func (s *Server) handleSetProjectFederation(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		Federated bool `json:"federated"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Federated && !s.federationEnabled(c) {
		return echo.NewHTTPError(http.StatusConflict, "this server does not federate, so it cannot share a project")
	}
	if err := s.projects.SetFederated(ctx, actorOf(c), access, request.Federated); err != nil {
		return projectError(err)
	}

	user, _ := currentUser(c)
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username,
		Action:     audit.ActionProjectFederationChanged,
		TargetKind: "project", TargetID: access.Project.ID.String(),
		Detail:   map[string]any{"federated": request.Federated},
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	access.Project.Federated = request.Federated
	response, err := s.fullProject(ctx, access)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleRemoveProjectMember(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	if err := s.projects.RemoveMember(ctx, actorOf(c), access, c.Param("username")); err != nil {
		return projectError(err)
	}

	user, _ := currentUser(c)
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username,
		Action:     audit.ActionProjectMemberRemoved,
		TargetKind: "project", TargetID: access.Project.ID.String(),
		Detail:   map[string]any{"member": c.Param("username")},
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type stateRequest struct {
	Name         *string `json:"name"`
	IsCompletion *bool   `json:"isCompletion"`
}

func (s *Server) handleAddState(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request stateRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	input := projects.NewState{}
	if request.Name != nil {
		input.Name = *request.Name
	}
	if request.IsCompletion != nil {
		input.IsCompletion = *request.IsCompletion
	}
	state, err := s.projects.AddState(c.Request().Context(), actorOf(c), access, input)
	if err != nil {
		return projectError(err)
	}
	return c.JSON(http.StatusCreated, stateResponseOf(state))
}

// stateParam reads a state identifier from the path. One that is not an
// identifier names no state of this project, which is what it is told.
func stateParam(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("state"))
	if err != nil {
		return uuid.Nil, projectError(projects.ErrStateNotFound)
	}
	return id, nil
}

func (s *Server) handleUpdateState(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	stateID, err := stateParam(c)
	if err != nil {
		return err
	}
	var request stateRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	state, err := s.projects.UpdateState(c.Request().Context(), actorOf(c), access, stateID,
		projects.StateChanges{Name: request.Name, IsCompletion: request.IsCompletion})
	if err != nil {
		return projectError(err)
	}
	return c.JSON(http.StatusOK, stateResponseOf(state))
}

func (s *Server) handleReorderStates(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		Order []string `json:"order"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	order := make([]uuid.UUID, 0, len(request.Order))
	for _, raw := range request.Order {
		id, err := uuid.Parse(raw)
		if err != nil {
			return projectError(projects.ErrStateNotFound)
		}
		order = append(order, id)
	}
	states, err := s.projects.ReorderStates(c.Request().Context(), actorOf(c), access, order)
	if err != nil {
		return projectError(err)
	}
	response := make([]stateResponse, 0, len(states))
	for _, state := range states {
		response = append(response, stateResponseOf(state))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleRemoveState(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	stateID, err := stateParam(c)
	if err != nil {
		return err
	}
	var moveTo *uuid.UUID
	if raw := c.QueryParam("moveTo"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return projectError(projects.ErrStateNotFound)
		}
		moveTo = &id
	}
	if err := s.projects.RemoveState(c.Request().Context(), actorOf(c), access, stateID, moveTo); err != nil {
		return projectError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// issueResponse is the public shape of an issue.
type issueResponse struct {
	ID string `json:"id"`
	// Key is the identifier people use: WEB-12. It never changes.
	Key       string        `json:"key"`
	Number    int64         `json:"number"`
	Title     string        `json:"title"`
	State     stateResponse `json:"state"`
	Author    string        `json:"author,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`

	Assignees []personRef        `json:"assignees"`
	Labels    []labelResponse    `json:"labels"`
	Priority  *priorityResponse  `json:"priority,omitempty"`
	Relations *relationsResponse `json:"relations,omitempty"`
	// The cycle it is in and the milestone it counts towards, if any.
	Cycle     *planRef `json:"cycle,omitempty"`
	Milestone *planRef `json:"milestone,omitempty"`

	// Only on the full view of one issue.
	Description *string          `json:"description,omitempty"`
	Project     *projectResponse `json:"project,omitempty"`
	Can         *issueAbilities  `json:"can,omitempty"`
	Origin      *originResponse  `json:"origin,omitempty"`
}

// issueAbilities is what the caller may do to this one issue.
type issueAbilities struct {
	Edit       bool `json:"edit"`
	Transition bool `json:"transition"`
	Assign     bool `json:"assign"`
	Label      bool `json:"label"`
	Relate     bool `json:"relate"`
	Delete     bool `json:"delete"`
	Plan       bool `json:"plan"`
}

// issueSummaries renders a page of issues, reading each state and author once.
func (s *Server) issueSummaries(ctx context.Context, project dbgen.Project, issues []dbgen.Issue) ([]issueResponse, error) {
	states, err := s.projects.States(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]dbgen.IssueState, len(states))
	for _, state := range states {
		byID[state.ID] = state
	}

	authors := map[uuid.UUID]string{}
	response := make([]issueResponse, 0, len(issues))
	for _, issue := range issues {
		summary := issueResponse{
			ID:        issue.ID.String(),
			Key:       projects.Identifier(project, issue),
			Number:    issue.Number,
			Title:     issue.Title,
			State:     stateResponseOf(byID[issue.StateID]),
			CreatedAt: issue.CreatedAt,
			UpdatedAt: issue.UpdatedAt,
		}
		if issue.AuthorID != nil {
			name, known := authors[*issue.AuthorID]
			if !known {
				if author, err := s.queries.GetUserByID(ctx, *issue.AuthorID); err == nil {
					name = s.rooms.UserAddress(author.Username)
				}
				authors[*issue.AuthorID] = name
			}
			summary.Author = name
		}
		response = append(response, summary)
	}
	if err := s.decorate(ctx, project.ID, issues, response); err != nil {
		return nil, err
	}
	return response, nil
}

func (s *Server) handleListIssues(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}

	filter := projects.IssueFilter{ByPriority: c.QueryParam("sort") == "priority"}
	for _, parameter := range []struct {
		name    string
		target  **uuid.UUID
		missing error
	}{
		{"state", &filter.StateID, projects.ErrStateNotFound},
		{"label", &filter.LabelID, projects.ErrLabelNotFound},
		{"priority", &filter.PriorityID, projects.ErrPriorityMissing},
	} {
		if raw := c.QueryParam(parameter.name); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return structureError(parameter.missing)
			}
			*parameter.target = &id
		}
	}
	if username := c.QueryParam("assignee"); username != "" {
		person, err := s.queries.GetUserByUsername(ctx, identity.NormaliseUsername(username))
		if err != nil {
			// Nobody by that name is assigned to anything.
			return c.JSON(http.StatusOK, []issueResponse{})
		}
		filter.AssigneeID = &person.ID
	}
	filter.Before, _ = strconv.ParseInt(c.QueryParam("before"), 10, 64)
	filter.Limit, _ = strconv.Atoi(c.QueryParam("limit"))

	issues, err := s.projects.ListIssues(ctx, access, filter)
	if err != nil {
		return err
	}
	response, err := s.issueSummaries(ctx, access.Project, issues)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

type createIssueRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	StateID     string `json:"stateId"`
	// The message this issue is made from, when it is. Both or neither.
	OriginChannel string `json:"originChannel"`
	OriginEvent   string `json:"originEvent"`
}

func (s *Server) handleCreateIssue(c *echo.Context) error {
	ctx := c.Request().Context()
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}

	var request createIssueRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	input := projects.NewIssue{Title: request.Title, Description: request.Description}
	if request.StateID != "" {
		id, err := uuid.Parse(request.StateID)
		if err != nil {
			return projectError(projects.ErrStateNotFound)
		}
		input.StateID = &id
	}
	if request.OriginChannel != "" || request.OriginEvent != "" {
		room, err := uuid.Parse(request.OriginChannel)
		if err != nil || request.OriginEvent == "" {
			return projectError(projects.ErrOriginNotFound)
		}
		input.Origin = &projects.Origin{RoomID: room, EventID: request.OriginEvent}
	}

	issue, err := s.projects.CreateIssue(ctx, actorOf(c), access, input)
	if err != nil {
		return projectError(err)
	}

	response, err := s.fullIssue(ctx, c, access, issue)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, response)
}

// issueFrom resolves the issue named in the path, or answers as if it did not
// exist.
func (s *Server) issueFrom(c *echo.Context) (projects.Access, dbgen.Issue, error) {
	access, issue, err := s.projects.IssueByIdentifier(c.Request().Context(), actorOf(c), c.Param("ref"))
	if err != nil {
		return projects.Access{}, dbgen.Issue{}, projectError(err)
	}
	return access, issue, nil
}

// fullIssue is an issue with everything the detail view shows.
func (s *Server) fullIssue(ctx context.Context, c *echo.Context, access projects.Access, issue dbgen.Issue) (issueResponse, error) {
	summaries, err := s.issueSummaries(ctx, access.Project, []dbgen.Issue{issue})
	if err != nil {
		return issueResponse{}, err
	}
	response := summaries[0]

	description := issue.Description
	response.Description = &description
	// With its states, which are where the issue can move to.
	project, err := s.fullProject(ctx, access)
	if err != nil {
		return issueResponse{}, err
	}
	response.Project = &project
	response.Can = &issueAbilities{
		Edit:       access.MayEdit(issue).Allowed,
		Transition: access.AllowsOn(authz.PermIssueTransition, issue).Allowed,
		Assign:     access.AllowsOn(authz.PermIssueAssign, issue).Allowed,
		Label:      access.AllowsOn(authz.PermIssueLabel, issue).Allowed,
		Relate:     access.AllowsOn(authz.PermIssueRelate, issue).Allowed,
		Delete:     access.Allows(authz.PermIssueDelete).Allowed,
		Plan:       access.AllowsOn(authz.PermIssuePlan, issue).Allowed,
	}

	origin, err := s.originOf(ctx, c, access, issue)
	if err != nil {
		return issueResponse{}, err
	}
	response.Origin = origin

	if response.Relations, err = s.relationsOf(ctx, access.Project, issue); err != nil {
		return issueResponse{}, err
	}
	return response, nil
}

func (s *Server) handleGetIssue(c *echo.Context) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	response, err := s.fullIssue(c.Request().Context(), c, access, issue)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleEditIssue(c *echo.Context) error {
	ctx := c.Request().Context()
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	edited, err := s.projects.Edit(ctx, actorOf(c), access, issue, projects.IssueChanges{
		Title: request.Title, Description: request.Description,
	})
	if err != nil {
		return projectError(err)
	}
	response, err := s.fullIssue(ctx, c, access, edited)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleDeleteIssue(c *echo.Context) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	if err := s.projects.Delete(c.Request().Context(), actorOf(c), access, issue); err != nil {
		return projectError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

type transitionRequest struct {
	StateID string `json:"stateId"`
	// ExpectedStateID is the state the caller saw the issue in. Given, a move
	// against an issue somebody else has just moved is refused with what it
	// became, rather than overwriting their move.
	ExpectedStateID string `json:"expectedStateId,omitempty"`
}

func (s *Server) handleTransitionIssue(c *echo.Context) error {
	ctx := c.Request().Context()
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}

	var request transitionRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	to, err := uuid.Parse(request.StateID)
	if err != nil {
		return projectError(projects.ErrStateNotFound)
	}
	var expected *uuid.UUID
	if request.ExpectedStateID != "" {
		id, err := uuid.Parse(request.ExpectedStateID)
		if err != nil {
			return projectError(projects.ErrStateNotFound)
		}
		expected = &id
	}

	moved, err := s.projects.Transition(ctx, actorOf(c), access, issue, to, expected)
	var conflict projects.ConflictError
	if errors.As(err, &conflict) {
		/*
			The issue as it now is, so the client can put the card where it
			actually went. A client that only heard "conflict" would have to
			guess, or re-fetch the whole board to find one card.
		*/
		current, renderErr := s.issueSummaries(ctx, access.Project, []dbgen.Issue{conflict.Issue})
		if renderErr != nil {
			return renderErr
		}
		return c.JSON(http.StatusConflict, map[string]any{
			"message": conflict.Error(),
			"issue":   current[0],
		})
	}
	if err != nil {
		return projectError(err)
	}

	summaries, err := s.issueSummaries(ctx, access.Project, []dbgen.Issue{moved})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, summaries[0])
}

// historyEntry is one recorded change to an issue.
type historyEntry struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Actor    string    `json:"actor"`
	At       time.Time `json:"at"`
	Sequence int64     `json:"sequence"`
	// Content is what the event recorded, as it recorded it: for a change to
	// a field, its field, previous and new values.
	Content json.RawMessage `json:"content"`
}

func (s *Server) handleIssueHistory(c *echo.Context) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	history, err := s.projects.History(c.Request().Context(), access, issue)
	if err != nil {
		return err
	}
	response := make([]historyEntry, 0, len(history))
	for _, event := range history {
		response = append(response, historyEntry{
			ID:       event.ID,
			Type:     event.Type,
			Actor:    event.SenderAddress,
			At:       time.UnixMilli(event.CreatedAt).UTC(),
			Sequence: event.Sequence,
			Content:  json.RawMessage(event.Content),
		})
	}
	return c.JSON(http.StatusOK, response)
}

/*
handleProjectHistory is what happened to a project itself, newest first, a page
at a time: ?before= the sequence of the oldest entry already shown.

A baseline's content is left out. It is every row the project held, which is
for a projection to read rather than a person, and would make one page of
history weigh as much as the whole project.
*/
func (s *Server) handleProjectHistory(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	before, _ := strconv.ParseInt(c.QueryParam("before"), 10, 64)
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	history, err := s.projects.ProjectHistory(c.Request().Context(), access, before, limit)
	if err != nil {
		return err
	}
	response := make([]historyEntry, 0, len(history))
	for _, event := range history {
		content := json.RawMessage(event.Content)
		if events.Type(event.Type) == events.TypeProjectBaseline {
			content = json.RawMessage(`{}`)
		}
		response = append(response, historyEntry{
			ID:       event.ID,
			Type:     event.Type,
			Actor:    event.SenderAddress,
			At:       time.UnixMilli(event.CreatedAt).UTC(),
			Sequence: event.Sequence,
			Content:  content,
		})
	}
	return c.JSON(http.StatusOK, response)
}

type roleProjectPermissionRequest struct {
	Project    string `json:"project"`
	Permission string `json:"permission"`
	Granted    bool   `json:"granted"`
}

/*
handleSetRoleProjectPermission grants or withdraws a permission on a role, in
one project.

The project is named by its key and must be one the administrator can see.
Granting something in a private project one is not in would be a way of
learning, by the answer, whether a key is taken.
*/
func (s *Server) handleSetRoleProjectPermission(c *echo.Context) error {
	ctx := c.Request().Context()
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}
	var request roleProjectPermissionRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	access, err := s.projects.ProjectByKey(ctx, actorOf(c), strings.TrimSpace(request.Project))
	if err != nil {
		return projectError(err)
	}

	err = s.authz.SetRolePermissionInProject(ctx, roleID, authz.Permission(request.Permission),
		access.Project.ID, request.Granted)
	switch {
	case errors.Is(err, authz.ErrUnknownPermission):
		return echo.NewHTTPError(http.StatusBadRequest, "this server has no such permission")
	case errors.Is(err, authz.ErrScopeNotAllowed):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case err != nil:
		return err
	}

	user, _ := currentUser(c)
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID: &user.ID, ActorLabel: user.Username,
		Action:     audit.ActionProjectPermissionChanged,
		TargetKind: "role", TargetID: roleID.String(),
		Detail: map[string]any{
			"project": access.Project.Key, "permission": request.Permission, "granted": request.Granted,
		},
		SourceIP: requestIP(c), UserAgent: c.Request().UserAgent(),
	}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
