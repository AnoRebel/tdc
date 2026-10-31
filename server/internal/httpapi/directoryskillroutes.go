package httpapi

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/directory"
)

/*
The skill vocabulary over HTTP: choosing from it, proposing to it, and the
queue a reviewer works through.
*/

func init() {
	documentRoutes(map[string]entry{
		"GET /api/directory/skills":  {Summary: "The skill vocabulary, and your own proposals", Authentication: "optional"},
		"POST /api/directory/skills": {Summary: "Propose a skill for the vocabulary", Authentication: "session or token"},
		"GET /api/directory/moderation/skills": {
			Summary: "Skills waiting for review", Authentication: "session or token",
			Permission: string(authz.PermDirectorySkillsManage),
		},
		"POST /api/directory/moderation/skills/:id/approve": {
			Summary: "Add a proposed skill to the vocabulary", Authentication: "session or token",
			Permission: string(authz.PermDirectorySkillsManage),
		},
		"POST /api/directory/moderation/skills/:id/reject": {
			Summary: "Reject a proposed skill", Authentication: "session or token",
			Permission: string(authz.PermDirectorySkillsManage),
		},
		"POST /api/directory/moderation/skills/:id/merge": {
			Summary: "Merge a proposed skill into one the vocabulary has", Authentication: "session or token",
			Permission: string(authz.PermDirectorySkillsManage),
		},
	})
}

// skillProposalsPerDay bounds how fast one person can fill the review queue.
var skillProposalsPerDay = cache.RateLimit{Attempts: 20, Window: 24 * time.Hour}

func (api *directoryAPI) mountSkills(directoryGroup, moderation *echo.Group) {
	directoryGroup.GET("/skills", api.handleListSkills)
	directoryGroup.POST("/skills", api.handleProposeSkill, requireAuthentication)

	manage := requirePermission(authz.PermDirectorySkillsManage)
	moderation.GET("/skills", api.handleSkillQueue, manage)
	moderation.POST("/skills/:id/approve", api.handleApproveSkill, manage)
	moderation.POST("/skills/:id/reject", api.handleRejectSkill, manage)
	moderation.POST("/skills/:id/merge", api.handleMergeSkill, manage)
}

type skillView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Status is "proposed" for a skill still waiting for review, which only
	// the person who proposed it is shown.
	Status string `json:"status"`
}

func skillViewOf(skill directory.Skill) skillView {
	return skillView{
		ID: skill.ID.String(), Name: skill.Name, Description: skill.Description, Status: string(skill.Status),
	}
}

/*
handleListSkills is the vocabulary, for somebody choosing a skill or filtering
by one.

Readable without signing in, because a stranger filtering a public directory
by skill needs the list too. Somebody signed in also sees their own proposals,
and nobody else's.
*/
func (api *directoryAPI) handleListSkills(c *echo.Context) error {
	_, viewer, allowed := api.viewerRank(c)
	if !allowed {
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}
	skills, err := api.directory.Skills(c.Request().Context(), viewer, c.QueryParam("q"))
	if err != nil {
		return err
	}
	views := make([]skillView, 0, len(skills))
	for _, skill := range skills {
		views = append(views, skillViewOf(skill))
	}
	return c.JSON(http.StatusOK, views)
}

type proposeSkillRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

/*
handleProposeSkill asks for a skill to be added.

200 with the existing skill when the name already means one — including under
another spelling — and 201 when this made something new: a proposal for most
people, and a skill in the vocabulary straight away for somebody who manages
it.
*/
func (api *directoryAPI) handleProposeSkill(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	var request proposeSkillRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	decision, err := api.limiter.Allow(ctx, "directory:skill:propose:"+user.ID.String(), skillProposalsPerDay)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}

	manager := currentPermissions(c).AllowsServer(authz.PermDirectorySkillsManage).Allowed
	skill, created, err := api.directory.ProposeSkill(ctx, user.ID, request.Name, request.Description, manager)
	if err != nil {
		return directoryError(err)
	}
	if !created {
		return c.JSON(http.StatusOK, skillViewOf(skill))
	}

	if skill.Status == directory.SkillActive {
		if err := api.recordSkillDecision(c, audit.ActionDirectorySkillAdded, skill, nil); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusCreated, skillViewOf(skill))
}

type queuedSkillView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ProposedBy  string `json:"proposedBy,omitempty"`
	Profiles    int64  `json:"profiles"`
	CreatedAt   string `json:"createdAt"`
}

func (api *directoryAPI) handleSkillQueue(c *echo.Context) error {
	queue, err := api.directory.SkillQueue(c.Request().Context())
	if err != nil {
		return err
	}
	views := make([]queuedSkillView, 0, len(queue))
	for _, skill := range queue {
		views = append(views, queuedSkillView{
			ID: skill.ID.String(), Name: skill.Name, Description: skill.Description,
			ProposedBy: skill.ProposedBy, Profiles: skill.Profiles,
			CreatedAt: skill.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return c.JSON(http.StatusOK, views)
}

func skillIDParam(c *echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil(), echo.NewHTTPError(http.StatusNotFound, directory.ErrNotProposed.Error())
	}
	return id, nil
}

func (api *directoryAPI) handleApproveSkill(c *echo.Context) error {
	id, err := skillIDParam(c)
	if err != nil {
		return err
	}
	reviewer, _ := currentUser(c)
	skill, err := api.directory.ApproveSkill(c.Request().Context(), id, reviewer.ID)
	if err != nil {
		return skillError(err)
	}
	if err := api.recordSkillDecision(c, audit.ActionDirectorySkillApproved, skill, nil); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, skillViewOf(skill))
}

type rejectSkillRequest struct {
	Reason string `json:"reason"`
}

func (api *directoryAPI) handleRejectSkill(c *echo.Context) error {
	id, err := skillIDParam(c)
	if err != nil {
		return err
	}
	var request rejectSkillRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	skill, err := api.directory.RejectSkill(c.Request().Context(), id)
	if err != nil {
		return skillError(err)
	}
	if err := api.recordSkillDecision(c, audit.ActionDirectorySkillRejected, skill,
		map[string]any{"reason": request.Reason}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type mergeSkillRequest struct {
	Into string `json:"into"`
}

func (api *directoryAPI) handleMergeSkill(c *echo.Context) error {
	id, err := skillIDParam(c)
	if err != nil {
		return err
	}
	var request mergeSkillRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	into, err := uuid.Parse(request.Into)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "into: name the skill to merge into")
	}

	proposal, target, err := api.directory.MergeSkill(c.Request().Context(), id, into)
	if err != nil {
		return skillError(err)
	}
	if err := api.recordSkillDecision(c, audit.ActionDirectorySkillMerged, proposal,
		map[string]any{"into": target.ID.String(), "intoName": target.Name}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, skillViewOf(target))
}

// recordSkillDecision writes a vocabulary decision to the audit log. The
// skill's name goes in the detail: it is what the decision was about, and a
// rejected one no longer exists to be looked up.
func (api *directoryAPI) recordSkillDecision(c *echo.Context, action audit.Action, skill directory.Skill, detail map[string]any) error {
	reviewer, _ := currentUser(c)
	if detail == nil {
		detail = map[string]any{}
	}
	detail["name"] = skill.Name
	return api.audit.Record(c.Request().Context(), audit.Entry{
		ActorID:    &reviewer.ID,
		ActorLabel: reviewer.Username,
		Action:     action,
		TargetKind: "directory_skill",
		TargetID:   skill.ID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail:     detail,
	})
}

func skillError(err error) error {
	if errors.Is(err, directory.ErrNotProposed) {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return directoryError(err)
}
