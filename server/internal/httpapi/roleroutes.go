package httpapi

import (
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/webhooks"
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

// roleEntryResponse is one thing a role says about one permission.
type roleEntryResponse struct {
	Permission string `json:"permission"`
	// Granted false is an explicit denial. A permission a role says nothing
	// about has no entry at all.
	Granted bool `json:"granted"`
	// Scope is "server", "channel" or "project".
	Scope string `json:"scope"`
	// ScopeID and ScopeName name the channel or project a scoped entry is for.
	// A name left empty is a channel that has since gone.
	ScopeID   string `json:"scopeId,omitempty"`
	ScopeName string `json:"scopeName,omitempty"`
}

// roleSummaryResponse is a role as the list shows it: who holds it, and
// everything it says.
type roleSummaryResponse struct {
	roleResponse
	MemberCount int64               `json:"memberCount"`
	Permissions []roleEntryResponse `json:"permissions"`
}

/*
handleListRoles is what an administrator sees before changing anything.

Every role with every entry on it, channel ones included, in one answer. A
server has a handful of roles and each a few dozen entries; a screen that has to
ask again for each role it shows is slower and no simpler.
*/
func (s *Server) handleListRoles(c *echo.Context) error {
	roles, err := s.authz.RoleSummaries(c.Request().Context())
	if err != nil {
		return err
	}

	listed := make([]roleSummaryResponse, 0, len(roles))
	for _, role := range roles {
		entries := make([]roleEntryResponse, 0, len(role.Entries))
		for _, entry := range role.Entries {
			item := roleEntryResponse{
				Permission: entry.Permission,
				Granted:    entry.Granted,
				Scope:      string(authz.ScopeServer),
			}
			if entry.ScopeID != nil {
				item.Scope = string(authz.ScopeChannel)
				if entry.InProject {
					item.Scope = string(authz.ScopeProject)
				}
				item.ScopeID = entry.ScopeID.String()
				item.ScopeName = entry.ScopeName
			}
			entries = append(entries, item)
		}
		listed = append(listed, roleSummaryResponse{
			roleResponse: roleResponse{
				ID:          role.ID.String(),
				Name:        role.Name,
				Description: role.Description,
				BuiltIn:     role.BuiltIn,
			},
			MemberCount: role.MemberCount,
			Permissions: entries,
		})
	}
	return c.JSON(http.StatusOK, listed)
}

type updateRoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleUpdateRole renames a role or changes what it says it is for.
func (s *Server) handleUpdateRole(c *echo.Context) error {
	ctx := c.Request().Context()
	actor, _ := currentUser(c)

	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}

	var request updateRoleRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	before, err := s.queries.GetRoleByID(ctx, roleID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "there is no such role")
	}

	role, err := s.authz.UpdateRole(ctx, roleID, request.Name, request.Description)
	switch {
	case errors.Is(err, authz.ErrRoleNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "there is no such role")
	case errors.Is(err, authz.ErrBuiltInName):
		return echo.NewHTTPError(http.StatusConflict,
			"the server finds this role by its name, so it keeps it — its description can change")
	case errors.Is(err, authz.ErrRoleNameTaken):
		return echo.NewHTTPError(http.StatusConflict, "a role already has that name")
	case errors.Is(err, authz.ErrInvalidRoleName):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case err != nil:
		return err
	}

	detail := map[string]any{}
	if before.Name != role.Name {
		detail["from"] = before.Name
		detail["to"] = role.Name
	}
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     audit.ActionRoleUpdated,
		TargetKind: "role",
		TargetID:   role.ID.String(),
		Detail:     detail,
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, roleResponse{
		ID:          role.ID.String(),
		Name:        role.Name,
		Description: role.Description,
		BuiltIn:     role.BuiltIn,
	})
}

/*
handleClearRolePermission puts a role back to saying nothing about a
permission, at server scope or, with ?channel=, in one channel.

The third state beside granting and denying. Withdrawing through the PUT stores
a denial, which beats every other role's grant; this leaves the question to
them.
*/
func (s *Server) handleClearRolePermission(c *echo.Context) error {
	ctx := c.Request().Context()
	actor, _ := currentUser(c)

	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}
	permission := c.Param("permission")

	var scope *uuid.UUID
	if raw := c.QueryParam("channel"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "that is not a channel")
		}
		scope = &parsed
	}

	err = s.authz.ClearRolePermission(ctx, roleID, authz.Permission(permission), scope,
		actor.ID, c.QueryParam("confirmed") == "true")
	switch {
	case errors.Is(err, authz.ErrUnknownPermission):
		return echo.NewHTTPError(http.StatusBadRequest, "this server has no such permission")
	case errors.Is(err, authz.ErrLastAdministrator):
		return echo.NewHTTPError(http.StatusConflict,
			"this would leave the server with no administrator")
	case errors.Is(err, authz.ErrWouldLockYourselfOut):
		return echo.NewHTTPError(http.StatusConflict,
			"this would remove your own authority; add ?confirmed=true to go ahead")
	case err != nil:
		return err
	}

	detail := map[string]any{"permission": permission, "cleared": true}
	if scope != nil {
		detail["channel"] = scope.String()
	}
	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &actor.ID,
		ActorLabel: actor.Username,
		Action:     audit.ActionRolePermissionChanged,
		TargetKind: "role",
		TargetID:   roleID.String(),
		Detail:     detail,
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// roleMemberResponse is somebody holding a role.
type roleMemberResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Address     string `json:"address"`
}

type roleMembersResponse struct {
	Members []roleMemberResponse `json:"members"`
	// Truncated says there are more than were listed, which on the role every
	// account holds is the ordinary case.
	Truncated bool `json:"truncated"`
}

// roleMembersShown is how many holders of a role are listed at most.
const roleMembersShown = 500

// handleListRoleMembers is who holds a role.
func (s *Server) handleListRoleMembers(c *echo.Context) error {
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a role")
	}

	rows, err := s.authz.RoleMembers(c.Request().Context(), roleID, roleMembersShown+1)
	switch {
	case errors.Is(err, authz.ErrRoleNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "there is no such role")
	case err != nil:
		return err
	}

	response := roleMembersResponse{Members: make([]roleMemberResponse, 0, len(rows))}
	if len(rows) > roleMembersShown {
		rows = rows[:roleMembersShown]
		response.Truncated = true
	}
	for _, row := range rows {
		response.Members = append(response.Members, roleMemberResponse{
			ID:          row.ID.String(),
			Username:    row.Username,
			DisplayName: row.DisplayName,
			Address:     s.rooms.UserAddress(row.Username),
		})
	}
	return c.JSON(http.StatusOK, response)
}

// permissionSourceResponse is one role's entry behind an answer.
type permissionSourceResponse struct {
	RoleID   string `json:"roleId"`
	RoleName string `json:"roleName"`
	Granted  bool   `json:"granted"`
	// Scope is "server", or "channel" for an entry for the channel asked about.
	Scope string `json:"scope"`
	// Baseline marks an entry on the role every account holds, which any other
	// role's entry at the same scope replaces.
	Baseline bool `json:"baseline"`
}

type effectivePermissionResponse struct {
	Name    string `json:"name"`
	Allowed bool   `json:"allowed"`
	// Basis is where the answer came from: "administrator", "server", "scope"
	// for the channel asked about, or "none".
	Basis   string                     `json:"basis"`
	Sources []permissionSourceResponse `json:"sources"`
}

type effectiveRoleResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BuiltIn bool   `json:"builtIn"`
}

type effectivePermissionsResponse struct {
	User        roleMemberResponse            `json:"user"`
	Roles       []effectiveRoleResponse       `json:"roles"`
	Permissions []effectivePermissionResponse `json:"permissions"`
}

/*
handleExplainUser answers what somebody may do, and why.

Every permission in the catalogue, decided exactly as a request of theirs would
be — server-wide, or in one channel with ?channel= — with the entries on their
roles that bear on it. An administrator setting up a moderator asks this to see
that the role did what they meant, rather than finding out from the moderator.

Their roles as they are now, not as a token of theirs might narrow them: this is
about the account.
*/
func (s *Server) handleExplainUser(c *echo.Context) error {
	ctx := c.Request().Context()

	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not an account")
	}
	var channel *uuid.UUID
	if raw := c.QueryParam("channel"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "that is not a channel")
		}
		channel = &parsed
	}

	user, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "there is no such account")
	}

	roles, err := s.authz.RolesFor(ctx, userID)
	if err != nil {
		return err
	}
	permissions, sources, err := s.authz.Explain(ctx, userID)
	if err != nil {
		return err
	}

	response := effectivePermissionsResponse{
		User: roleMemberResponse{
			ID:          user.ID.String(),
			Username:    user.Username,
			DisplayName: user.DisplayName,
			Address:     s.rooms.UserAddress(user.Username),
		},
		Roles:       make([]effectiveRoleResponse, 0, len(roles)),
		Permissions: []effectivePermissionResponse{},
	}
	for _, role := range roles {
		response.Roles = append(response.Roles, effectiveRoleResponse{
			ID: role.ID.String(), Name: role.Name, BuiltIn: role.BuiltIn,
		})
	}

	for _, definition := range authz.Catalogue() {
		decision := permissions.AllowsServer(definition.Name)
		if channel != nil {
			decision = permissions.AllowsChannel(definition.Name, *channel)
		}

		item := effectivePermissionResponse{
			Name:    string(definition.Name),
			Allowed: decision.Allowed,
			Basis:   string(decision.Basis),
			Sources: []permissionSourceResponse{},
		}
		for _, source := range sources {
			if source.Permission != definition.Name {
				continue
			}
			scope := string(authz.ScopeServer)
			if source.ChannelID != nil {
				if channel == nil || *source.ChannelID != *channel {
					continue
				}
				scope = string(authz.ScopeChannel)
			}
			item.Sources = append(item.Sources, permissionSourceResponse{
				RoleID:   source.RoleID.String(),
				RoleName: source.RoleName,
				Granted:  source.Granted,
				Scope:    scope,
				Baseline: source.Baseline,
			})
		}
		response.Permissions = append(response.Permissions, item)
	}

	return c.JSON(http.StatusOK, response)
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
	s.announceRoleChange(ctx, actor, userID, roleID, "assigned")

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
	s.announceRoleChange(ctx, actor, userID, roleID, "removed")

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

/*
announceRoleChange tells server-wide webhooks that somebody's role changed,
for whoever may read every account — which includes the roles each holds.
*/
func (s *Server) announceRoleChange(ctx context.Context, actor dbgen.User, userID, roleID uuid.UUID, change string) {
	person, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		return
	}
	role, err := s.queries.GetRoleByID(ctx, roleID)
	if err != nil {
		return
	}
	s.queueServerEvent(ctx, webhooks.TypeAccountRoleChanged, s.rooms.UserAddress(actor.Username),
		map[string]any{
			"account": s.accountRef(person),
			"role":    map[string]any{"id": role.ID.String(), "name": role.Name},
			"change":  change,
		})
}
