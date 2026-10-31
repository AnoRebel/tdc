package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/notify"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
What makes an issue trackable: who is on it, its labels and priority, and how
it relates to others. Each change is its own recorded event, made by the
projects package in the same transaction as the change itself.
*/
func init() {
	registerRoutes(func(s *Server) {
		issues := s.echo.Group("/api/issues", requireAuthentication, s.requireProjects)
		issues.PUT("/:ref/assignees/:username", s.handleAssign)
		issues.DELETE("/:ref/assignees/:username", s.handleUnassign)
		issues.PUT("/:ref/labels/:label", s.handleLabelIssue)
		issues.DELETE("/:ref/labels/:label", s.handleUnlabelIssue)
		issues.PUT("/:ref/priority", s.handleSetPriority)
		issues.POST("/:ref/relations", s.handleRelate)
		issues.DELETE("/:ref/relations/:kind/:other", s.handleUnrelate)

		group := s.echo.Group("/api/projects", requireAuthentication, s.requireProjects)
		group.GET("/:key/labels", s.handleListLabels)
		group.POST("/:key/labels", s.handleAddLabel)
		group.PATCH("/:key/labels/:label", s.handleUpdateLabel)
		group.DELETE("/:key/labels/:label", s.handleRemoveLabel)
		group.GET("/:key/priorities", s.handleListPriorities)
		group.POST("/:key/priorities", s.handleAddPriority)
		group.PUT("/:key/priorities", s.handleReorderPriorities)
		group.PATCH("/:key/priorities/:priority", s.handleRenamePriority)
		group.DELETE("/:key/priorities/:priority", s.handleRemovePriority)

		s.echo.GET("/api/me/issues", s.handleMyIssues, requireAuthentication, s.requireProjects)
	})

	documentRoutes(map[string]entry{
		"PUT /api/issues/:ref/assignees/:username":       {Summary: "Assign somebody to an issue", Authentication: "session or token", Permission: "issue.assign"},
		"DELETE /api/issues/:ref/assignees/:username":    {Summary: "Take somebody off an issue", Authentication: "session or token", Permission: "issue.assign"},
		"PUT /api/issues/:ref/labels/:label":             {Summary: "Label an issue", Authentication: "session or token", Permission: "issue.label"},
		"DELETE /api/issues/:ref/labels/:label":          {Summary: "Remove a label from an issue", Authentication: "session or token", Permission: "issue.label"},
		"PUT /api/issues/:ref/priority":                  {Summary: "Set an issue's priority", Authentication: "session or token", Permission: "issue.label"},
		"POST /api/issues/:ref/relations":                {Summary: "Relate two issues", Authentication: "session or token", Permission: "issue.relate"},
		"DELETE /api/issues/:ref/relations/:kind/:other": {Summary: "Remove a relationship between issues", Authentication: "session or token", Permission: "issue.relate"},
		"GET /api/projects/:key/labels":                  {Summary: "A project's labels", Authentication: "session or token"},
		"POST /api/projects/:key/labels":                 {Summary: "Add a label", Authentication: "session or token", Permission: "issue.label"},
		"PATCH /api/projects/:key/labels/:label":         {Summary: "Rename or recolour a label", Authentication: "session or token", Permission: "issue.label"},
		"DELETE /api/projects/:key/labels/:label":        {Summary: "Remove a label", Authentication: "session or token", Permission: "issue.label"},
		"GET /api/projects/:key/priorities":              {Summary: "A project's priority scale", Authentication: "session or token"},
		"POST /api/projects/:key/priorities":             {Summary: "Add a priority", Authentication: "session or token", Permission: "project.configure_states"},
		"PUT /api/projects/:key/priorities":              {Summary: "Reorder the priority scale", Authentication: "session or token", Permission: "project.configure_states"},
		"PATCH /api/projects/:key/priorities/:priority":  {Summary: "Rename a priority", Authentication: "session or token", Permission: "project.configure_states"},
		"DELETE /api/projects/:key/priorities/:priority": {Summary: "Remove a priority", Authentication: "session or token", Permission: "project.configure_states"},
		"GET /api/me/issues":                             {Summary: "Issues assigned to me", Authentication: "session or token"},
	})
}

// structureError maps the refusals particular to this file, then the rest.
func structureError(err error) error {
	var cycle projects.CycleError
	switch {
	case errors.As(err, &cycle):
		return echo.NewHTTPError(http.StatusConflict, cycle.Error())
	case errors.Is(err, projects.ErrHasParent),
		errors.Is(err, projects.ErrLabelNameTaken),
		errors.Is(err, projects.ErrPriorityTaken):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, projects.ErrCannotSeeIssue),
		errors.Is(err, projects.ErrLabelName),
		errors.Is(err, projects.ErrLabelColour),
		errors.Is(err, projects.ErrPriorityName),
		errors.Is(err, projects.ErrRelationKind),
		errors.Is(err, projects.ErrOtherProject),
		errors.Is(err, projects.ErrSelfRelation),
		errors.Is(err, projects.ErrLabelNotFound),
		errors.Is(err, projects.ErrPriorityMissing):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, projects.ErrNotRelated):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return projectError(err)
}

// personRef is somebody, as an issue names them.
type personRef struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Address     string `json:"address"`
}

// labelResponse is one of a project's labels.
type labelResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
}

func labelResponseOf(label dbgen.ProjectLabel) labelResponse {
	return labelResponse{ID: label.ID.String(), Name: label.Name, Colour: label.Colour}
}

// priorityResponse is one level of a project's priority scale.
type priorityResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Rank is its place on the scale, 1 being the most urgent.
	Rank int32 `json:"rank"`
}

func priorityResponseOf(priority dbgen.ProjectPriority) priorityResponse {
	return priorityResponse{ID: priority.ID.String(), Name: priority.Name, Rank: priority.Rank}
}

// relatedIssue is another issue, as a relationship shows it.
type relatedIssue struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Completed bool   `json:"completed"`
}

/*
relationsResponse is how an issue stands among the others.

ChildProgress is how many of its children are finished, computed from their
states as they are now — which is how a parent reflects the completion of the
work beneath it.
*/
type relationsResponse struct {
	Parent        *relatedIssue  `json:"parent,omitempty"`
	Children      []relatedIssue `json:"children"`
	Blocks        []relatedIssue `json:"blocks"`
	BlockedBy     []relatedIssue `json:"blockedBy"`
	ChildProgress struct {
		Completed int `json:"completed"`
		Total     int `json:"total"`
	} `json:"childProgress"`
}

// decorate adds assignees, labels and priority to a page of summaries,
// reading each for the whole page at once.
func (s *Server) decorate(ctx context.Context, projectID uuid.UUID, issues []dbgen.Issue, summaries []issueResponse) error {
	if len(issues) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}

	assignees, err := s.queries.ListAssigneesForIssues(ctx, ids)
	if err != nil {
		return err
	}
	labels, err := s.queries.ListLabelsForIssues(ctx, ids)
	if err != nil {
		return err
	}
	cycles, err := s.queries.ListCyclesForIssues(ctx, ids)
	if err != nil {
		return err
	}
	milestones, err := s.queries.ListMilestonesForIssues(ctx, ids)
	if err != nil {
		return err
	}
	priorities, err := s.projects.Priorities(ctx, projectID)
	if err != nil {
		return err
	}
	byPriority := map[uuid.UUID]dbgen.ProjectPriority{}
	for _, priority := range priorities {
		byPriority[priority.ID] = priority
	}

	index := map[string]int{}
	for i := range summaries {
		index[summaries[i].ID] = i
		summaries[i].Assignees = []personRef{}
		summaries[i].Labels = []labelResponse{}
		if issues[i].PriorityID != nil {
			if priority, ok := byPriority[*issues[i].PriorityID]; ok {
				response := priorityResponseOf(priority)
				summaries[i].Priority = &response
			}
		}
	}
	for _, assignee := range assignees {
		i := index[assignee.IssueID.String()]
		summaries[i].Assignees = append(summaries[i].Assignees, personRef{
			Username: assignee.Username, DisplayName: assignee.DisplayName,
			Address: s.rooms.UserAddress(assignee.Username),
		})
	}
	for _, label := range labels {
		i := index[label.IssueID.String()]
		summaries[i].Labels = append(summaries[i].Labels, labelResponse{
			ID: label.ID.String(), Name: label.Name, Colour: label.Colour,
		})
	}
	for _, cycle := range cycles {
		i := index[cycle.IssueID.String()]
		summaries[i].Cycle = &planRef{ID: cycle.ID.String(), Name: cycle.Name}
	}
	for _, milestone := range milestones {
		i := index[milestone.IssueID.String()]
		summaries[i].Milestone = &planRef{ID: milestone.ID.String(), Name: milestone.Name}
	}
	return nil
}

// relationsOf gathers an issue's relationships, by which side it is on.
func (s *Server) relationsOf(ctx context.Context, project dbgen.Project, issue dbgen.Issue) (*relationsResponse, error) {
	rows, err := s.projects.Relations(ctx, issue)
	if err != nil {
		return nil, err
	}
	response := &relationsResponse{Children: []relatedIssue{}, Blocks: []relatedIssue{}, BlockedBy: []relatedIssue{}}
	for _, row := range rows {
		other := relatedIssue{
			Key:       projects.Identifier(project, dbgen.Issue{Number: row.OtherNumber}),
			Title:     row.OtherTitle,
			State:     row.OtherState,
			Completed: row.OtherCompleted,
		}
		subjectIsFrom := row.FromIssue == issue.ID
		switch {
		case row.Kind == "parent" && subjectIsFrom:
			response.Children = append(response.Children, other)
			response.ChildProgress.Total++
			if other.Completed {
				response.ChildProgress.Completed++
			}
		case row.Kind == "parent":
			response.Parent = &other
		case row.Kind == "blocks" && subjectIsFrom:
			response.Blocks = append(response.Blocks, other)
		default:
			response.BlockedBy = append(response.BlockedBy, other)
		}
	}
	return response, nil
}

func (s *Server) handleAssign(c *echo.Context) error {
	ctx := c.Request().Context()
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	if projects.IsReplica(access.Project) {
		if _, err := s.atHome(c, access, projects.Change{
			Kind: projects.ChangeAssign, Issue: issue.Number, Person: s.replicaPerson(c.Param("username")),
		}); err != nil {
			return err
		}
		return s.respondWithIssue(c, access, issue)
	}
	event, person, err := s.projects.Assign(ctx, actorOf(c), access, issue, c.Param("username"))
	if err != nil {
		return structureError(err)
	}
	// Somebody on another server is told by their own server, when the
	// event reaches its replica; only somebody here is told from here.
	if event != nil && person.ID != uuid.Nil() {
		user, _ := currentUser(c)
		s.inBackground(ctx, "assignment notification", func(ctx context.Context) {
			s.notifyAssignee(ctx, user.ID, s.rooms.UserAddress(user.Username), person, issue, access.Project, *event)
		})
	}
	return s.respondWithIssue(c, access, issue)
}

/*
notifyAssignee tells somebody an issue is now theirs — according to their own
settings for its discussion, which they have just joined: somebody who has
asked to hear nothing, there or everywhere, hears nothing about this either.
*/
func (s *Server) notifyAssignee(ctx context.Context, actorID uuid.UUID, actorAddress string, assignee dbgen.User, issue dbgen.Issue, project dbgen.Project, event dbgen.Event) {
	if s.notify == nil {
		return
	}
	preference, err := s.queries.GetNotificationPreference(ctx, dbgen.GetNotificationPreferenceParams{
		UserID: assignee.ID, RoomID: issue.ID,
	})
	if err == nil && (preference.Level == string(notify.LevelNone) || preference.Muted) {
		return
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.log.Warn("cannot read a notification preference", "error", err)
	}
	s.notify.About(ctx, assignee.ID, actorID, actorAddress,
		issue.ID, event.ID, notify.Reason("assigned"),
		projects.Identifier(project, issue)+" "+issue.Title)
}

func (s *Server) handleUnassign(c *echo.Context) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	if projects.IsReplica(access.Project) {
		if _, err := s.atHome(c, access, projects.Change{
			Kind: projects.ChangeUnassign, Issue: issue.Number, Person: s.replicaPerson(c.Param("username")),
		}); err != nil {
			return err
		}
		return s.respondWithIssue(c, access, issue)
	}
	if err := s.projects.Unassign(c.Request().Context(), actorOf(c), access, issue, c.Param("username")); err != nil {
		return structureError(err)
	}
	return s.respondWithIssue(c, access, issue)
}

// respondWithIssue answers with the issue as it now is.
func (s *Server) respondWithIssue(c *echo.Context, access projects.Access, issue dbgen.Issue) error {
	ctx := c.Request().Context()
	current, err := s.queries.GetIssueByID(ctx, issue.ID)
	if err != nil {
		return err
	}
	response, err := s.fullIssue(ctx, c, access, current)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

// relateAtHome relates, or unrelates, a replica's issue to another of its
// issues, named by its identifier here and by number at home.
func (s *Server) relateAtHome(c *echo.Context, access projects.Access, issue dbgen.Issue, kind, relation, other string) error {
	_, number, ok := projects.ParseIdentifier(other)
	if !ok {
		return structureError(projects.ErrIssueNotFound)
	}
	_, err := s.atHome(c, access, projects.Change{Kind: kind, Issue: issue.Number, Relation: relation, Other: number})
	return err
}

func uuidParam(c *echo.Context, name string, missing error) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil(), structureError(missing)
	}
	return id, nil
}

func (s *Server) handleLabelIssue(c *echo.Context) error {
	return s.setIssueLabel(c, true)
}

func (s *Server) handleUnlabelIssue(c *echo.Context) error {
	return s.setIssueLabel(c, false)
}

func (s *Server) setIssueLabel(c *echo.Context, present bool) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	labelID, err := uuidParam(c, "label", projects.ErrLabelNotFound)
	if err != nil {
		return err
	}
	if projects.IsReplica(access.Project) {
		kind := projects.ChangeUnlabel
		if present {
			kind = projects.ChangeLabel
		}
		if _, err := s.atHome(c, access, projects.Change{Kind: kind, Issue: issue.Number, Label: &labelID}); err != nil {
			return err
		}
		return s.respondWithIssue(c, access, issue)
	}
	if _, err := s.projects.SetLabel(c.Request().Context(), actorOf(c), access, issue, labelID, present); err != nil {
		return structureError(err)
	}
	return s.respondWithIssue(c, access, issue)
}

func (s *Server) handleSetPriority(c *echo.Context) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		// PriorityID is null to clear it.
		PriorityID *string `json:"priorityId"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	var priorityID *uuid.UUID
	if request.PriorityID != nil && *request.PriorityID != "" {
		id, err := uuid.Parse(*request.PriorityID)
		if err != nil {
			return structureError(projects.ErrPriorityMissing)
		}
		priorityID = &id
	}
	if projects.IsReplica(access.Project) {
		if _, err := s.atHome(c, access, projects.Change{Kind: projects.ChangePriority, Issue: issue.Number, Priority: priorityID}); err != nil {
			return err
		}
		return s.respondWithIssue(c, access, issue)
	}
	if _, err := s.projects.SetPriority(c.Request().Context(), actorOf(c), access, issue, priorityID); err != nil {
		return structureError(err)
	}
	return s.respondWithIssue(c, access, issue)
}

func (s *Server) handleRelate(c *echo.Context) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		// Kind is what this issue is to the other: parent, child, blocks or
		// blocked_by.
		Kind  string `json:"kind"`
		Issue string `json:"issue"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if projects.IsReplica(access.Project) {
		if err := s.relateAtHome(c, access, issue, projects.ChangeRelate, request.Kind, request.Issue); err != nil {
			return err
		}
		return s.respondWithIssue(c, access, issue)
	}
	if err := s.projects.Relate(c.Request().Context(), actorOf(c), access, issue, request.Kind, request.Issue); err != nil {
		return structureError(err)
	}
	return s.respondWithIssue(c, access, issue)
}

func (s *Server) handleUnrelate(c *echo.Context) error {
	access, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	if projects.IsReplica(access.Project) {
		if err := s.relateAtHome(c, access, issue, projects.ChangeUnrelate, c.Param("kind"), c.Param("other")); err != nil {
			return err
		}
		return s.respondWithIssue(c, access, issue)
	}
	if err := s.projects.Unrelate(c.Request().Context(), actorOf(c), access, issue, c.Param("kind"), c.Param("other")); err != nil {
		return structureError(err)
	}
	return s.respondWithIssue(c, access, issue)
}

func (s *Server) handleListLabels(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	labels, err := s.projects.Labels(c.Request().Context(), access.Project.ID)
	if err != nil {
		return err
	}
	response := make([]labelResponse, 0, len(labels))
	for _, label := range labels {
		response = append(response, labelResponseOf(label))
	}
	return c.JSON(http.StatusOK, response)
}

type labelRequest struct {
	Name   *string `json:"name"`
	Colour *string `json:"colour"`
}

func (s *Server) handleAddLabel(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request labelRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	name, colour := "", ""
	if request.Name != nil {
		name = *request.Name
	}
	if request.Colour != nil {
		colour = *request.Colour
	}
	label, err := s.projects.AddLabel(c.Request().Context(), actorOf(c), access, name, colour)
	if err != nil {
		return structureError(err)
	}
	return c.JSON(http.StatusCreated, labelResponseOf(label))
}

func (s *Server) handleUpdateLabel(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	labelID, err := uuidParam(c, "label", projects.ErrLabelNotFound)
	if err != nil {
		return err
	}
	var request labelRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	label, err := s.projects.UpdateLabel(c.Request().Context(), actorOf(c), access, labelID, request.Name, request.Colour)
	if err != nil {
		return structureError(err)
	}
	return c.JSON(http.StatusOK, labelResponseOf(label))
}

func (s *Server) handleRemoveLabel(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	labelID, err := uuidParam(c, "label", projects.ErrLabelNotFound)
	if err != nil {
		return err
	}
	if err := s.projects.RemoveLabel(c.Request().Context(), actorOf(c), access, labelID); err != nil {
		return structureError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleListPriorities(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	priorities, err := s.projects.Priorities(c.Request().Context(), access.Project.ID)
	if err != nil {
		return err
	}
	response := make([]priorityResponse, 0, len(priorities))
	for _, priority := range priorities {
		response = append(response, priorityResponseOf(priority))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleAddPriority(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	var request struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	priority, err := s.projects.AddPriority(c.Request().Context(), actorOf(c), access, request.Name)
	if err != nil {
		return structureError(err)
	}
	return c.JSON(http.StatusCreated, priorityResponseOf(priority))
}

func (s *Server) handleRenamePriority(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	priorityID, err := uuidParam(c, "priority", projects.ErrPriorityMissing)
	if err != nil {
		return err
	}
	var request struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	priority, err := s.projects.RenamePriority(c.Request().Context(), actorOf(c), access, priorityID, request.Name)
	if err != nil {
		return structureError(err)
	}
	return c.JSON(http.StatusOK, priorityResponseOf(priority))
}

func (s *Server) handleReorderPriorities(c *echo.Context) error {
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
			return structureError(projects.ErrPriorityMissing)
		}
		order = append(order, id)
	}
	priorities, err := s.projects.ReorderPriorities(c.Request().Context(), actorOf(c), access, order)
	if err != nil {
		return structureError(err)
	}
	response := make([]priorityResponse, 0, len(priorities))
	for _, priority := range priorities {
		response = append(response, priorityResponseOf(priority))
	}
	return c.JSON(http.StatusOK, response)
}

func (s *Server) handleRemovePriority(c *echo.Context) error {
	access, err := s.projectFrom(c)
	if err != nil {
		return err
	}
	priorityID, err := uuidParam(c, "priority", projects.ErrPriorityMissing)
	if err != nil {
		return err
	}
	if err := s.projects.RemovePriority(c.Request().Context(), actorOf(c), access, priorityID); err != nil {
		return structureError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// myIssue is an issue assigned to the caller, with the project it is in.
type myIssue struct {
	issueResponse
	ProjectKey  string `json:"projectKey"`
	ProjectName string `json:"projectName"`
}

func (s *Server) handleMyIssues(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)
	limit, _ := strconv.Atoi(c.QueryParam("limit"))

	rows, err := s.projects.MyIssues(ctx, user.ID, limit)
	if err != nil {
		return err
	}
	response := make([]myIssue, 0, len(rows))
	for _, row := range rows {
		project := dbgen.Project{ID: row.ProjectID, Key: row.ProjectKey, Name: row.ProjectName}
		issue := dbgen.Issue{
			ID: row.ID, ProjectID: row.ProjectID, Number: row.Number, Title: row.Title,
			Description: row.Description, StateID: row.StateID, AuthorID: row.AuthorID,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, PriorityID: row.PriorityID,
		}
		summaries, err := s.issueSummaries(ctx, project, []dbgen.Issue{issue})
		if err != nil {
			return err
		}
		response = append(response, myIssue{
			issueResponse: summaries[0], ProjectKey: row.ProjectKey, ProjectName: row.ProjectName,
		})
	}
	return c.JSON(http.StatusOK, response)
}
