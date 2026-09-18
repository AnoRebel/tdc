package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
)

/*
Roles, and who holds them.

The service underneath has been complete since the authorization work: creating
a role, granting a permission, assigning it, and refusing the changes that would
leave the server or the person making them without authority. What was missing
was any way to reach it — so in practice a server had exactly one role, made
during setup, and only the first administrator could do anything that needed a
permission.

Every route here needs role.manage, which is administrative: deciding who may do
what is the authority that confers every other one.
*/

type roleResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	/*
		BuiltIn marks a role the server made for itself, which cannot be
		deleted. Its permissions can still be changed: an operator who wants
		members not to create channels withdraws that from the member role, and
		nothing else about the server changes.
	*/
	BuiltIn bool `json:"builtIn"`
}

// handleListRoles is what an administrator sees before changing anything.
func (s *Server) handleListRoles(c *echo.Context) error {
	roles, err := s.authz.ListRoles(c.Request().Context())
	if err != nil {
		return err
	}

	listed := make([]roleResponse, 0, len(roles))
	for _, role := range roles {
		listed = append(listed, roleResponse{
			ID:          role.ID.String(),
			Name:        role.Name,
			Description: role.Description,
			BuiltIn:     role.BuiltIn,
		})
	}
	return c.JSON(http.StatusOK, listed)
}

type createRoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleCreateRole makes a role, which starts with no permissions at all.
func (s *Server) handleCreateRole(c *echo.Context) error {
	ctx := c.Request().Context()
	actor, _ := currentUser(c)

	var request createRoleRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	role, err := s.authz.CreateRole(ctx, request.Name, request.Description)
	switch {
	case errors.Is(err, authz.ErrRoleNameTaken):
		return echo.NewHTTPError(http.StatusConflict, "a role already has that name")
	case errors.Is(err, authz.ErrInvalidRoleName):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case err != nil:
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     audit.ActionRoleCreated,
		TargetKind: "role",
		TargetID:   role.ID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, roleResponse{
		ID:          role.ID.String(),
		Name:        role.Name,
		Description: role.Description,
		BuiltIn:     role.BuiltIn,
	})
}

// handleDeleteRole removes one, unless the server needs it.
func (s *Server) handleDeleteRole(c *echo.Context) error {
	ctx := c.Request().Context()
	actor, _ := currentUser(c)

	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}

	err = s.authz.DeleteRole(ctx, roleID)
	switch {
	case errors.Is(err, authz.ErrLastAdministrator):
		return echo.NewHTTPError(http.StatusConflict,
			"this would leave the server with no administrator")
	case errors.Is(err, authz.ErrRoleProtected):
		return echo.NewHTTPError(http.StatusConflict,
			"this role is part of how the server works and cannot be removed — take its permissions away instead")
	case err != nil:
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     audit.ActionRoleDeleted,
		TargetKind: "role",
		TargetID:   roleID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

type setPermissionRequest struct {
	Permission string `json:"permission"`
	// Granted false withdraws it. A permission can also be explicitly denied
	// at channel scope, which is what the channel field is for.
	Granted bool `json:"granted"`
	// Channel scopes the grant to one channel. Absent means server-wide.
	Channel string `json:"channel,omitempty"`
	/*
		Confirmed acknowledges a change that would remove the actor's own
		authority.

		Asked for rather than refused outright: an administrator stepping down
		deliberately is a real thing, and the server's job is to make sure it
		was not an accident.
	*/
	Confirmed bool `json:"confirmed,omitempty"`
}

// handleSetRolePermission grants or withdraws one permission on one role.
func (s *Server) handleSetRolePermission(c *echo.Context) error {
	ctx := c.Request().Context()
	actor, _ := currentUser(c)

	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}

	var request setPermissionRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	var channel *uuid.UUID
	if request.Channel != "" {
		parsed, err := uuid.Parse(request.Channel)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "that is not a channel")
		}
		channel = &parsed
	}

	err = s.authz.SetRolePermission(ctx, roleID, authz.Permission(request.Permission),
		channel, request.Granted, actor.ID, request.Confirmed)
	switch {
	case errors.Is(err, authz.ErrUnknownPermission):
		return echo.NewHTTPError(http.StatusBadRequest, "this server has no such permission")
	case errors.Is(err, authz.ErrScopeNotAllowed):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, authz.ErrLastAdministrator):
		return echo.NewHTTPError(http.StatusConflict,
			"this would leave the server with no administrator")
	case errors.Is(err, authz.ErrWouldLockYourselfOut):
		/*
			Not a refusal so much as a question.

			The caller may mean it — somebody stepping down does — so the answer
			says what to send to go ahead rather than simply saying no.
		*/
		return echo.NewHTTPError(http.StatusConflict,
			"this would remove your own authority; send it again with confirmed to go ahead")
	case err != nil:
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     audit.ActionRolePermissionChanged,
		TargetKind: "role",
		TargetID:   roleID.String(),
		Detail: map[string]any{
			"permission": request.Permission,
			"granted":    request.Granted,
		},
		SourceIP:  requestIP(c),
		UserAgent: c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

type assignRoleRequest struct {
	User string `json:"user"`
}

// handleAssignRole puts somebody in a role.
func (s *Server) handleAssignRole(c *echo.Context) error {
	ctx := c.Request().Context()
	actor, _ := currentUser(c)

	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}

	var request assignRoleRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	userID, err := uuid.Parse(request.User)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not an account")
	}

	if err := s.authz.AssignRole(ctx, userID, roleID, &actor.ID); err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     audit.ActionRoleAssigned,
		TargetKind: "user",
		TargetID:   userID.String(),
		Detail:     map[string]any{"role": roleID.String()},
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// handleRemoveRole takes somebody out of one.
func (s *Server) handleRemoveRole(c *echo.Context) error {
	ctx := c.Request().Context()
	actor, _ := currentUser(c)

	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}
	userID, err := uuid.Parse(c.Param("user"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not an account")
	}

	err = s.authz.RemoveRole(ctx, userID, roleID, actor.ID, c.QueryParam("confirmed") == "true")
	switch {
	case errors.Is(err, authz.ErrLastAdministrator):
		return echo.NewHTTPError(http.StatusConflict,
			"this would leave the server with no administrator")
	case errors.Is(err, authz.ErrWouldLockYourselfOut):
		return echo.NewHTTPError(http.StatusConflict,
			"this would remove your own authority; add ?confirmed=true to go ahead")
	case err != nil:
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     audit.ActionRoleRemoved,
		TargetKind: "user",
		TargetID:   userID.String(),
		Detail:     map[string]any{"role": roleID.String()},
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
