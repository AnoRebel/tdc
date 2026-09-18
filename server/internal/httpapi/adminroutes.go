package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/identity"
)

// handleSetupStatus reports whether the server still needs its first
// administrator. It is public because nobody can authenticate before one
// exists.
func (s *Server) handleSetupStatus(c *echo.Context) error {
	needed, err := s.identity.SetupNeeded(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"setupRequired": needed})
}

func (s *Server) handleCompleteSetup(c *echo.Context) error {
	ctx := c.Request().Context()

	var request registerRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	user, err := s.identity.CompleteSetup(ctx, identity.NewUser{
		Username:    request.Username,
		Email:       request.Email,
		Password:    request.Password,
		DisplayName: request.DisplayName,
	})
	if err != nil {
		if errors.Is(err, identity.ErrSetupComplete) {
			return echo.NewHTTPError(http.StatusConflict,
				"this server has already been set up")
		}
		return registrationError(err)
	}

	// The first administrator needs the authority the role confers, so the
	// role is created and assigned as part of setup rather than left to a
	// later step the operator would have no way to perform.
	if err := s.grantAdministration(ctx, user.ID); err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionSetupComplete,
		TargetKind: "user",
		TargetID:   user.ID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, s.userResponse(user))
}

// grantAdministration creates the administrator role if it does not exist and
// assigns it.
//
// Done as part of setup because the first administrator would otherwise have
// no authority and no way to grant themselves any: there is nobody else to do
// it.
/*
grantMembership puts a new account in the role every account holds.

Called from every path that creates one, because an account with no role holds
no permissions — and on a server where channel.create is enforced, that is an
account that cannot do the first thing anybody joining a community wants to do.

A failure here is logged rather than returned. The account exists and the person
can sign in; what they lack is a permission an administrator can grant. Undoing
the registration over it would be worse, and so would refusing the sign-in they
have just earned.
*/
func (s *Server) grantMembership(ctx context.Context, userID uuid.UUID) {
	role, err := s.authz.EnsureDefaultRole(ctx)
	if err != nil {
		s.log.Error("could not find the default role", "error", err)
		return
	}

	if err := s.authz.AssignRole(ctx, userID, role.ID, nil); err != nil {
		s.log.Error("could not put a new account in the default role",
			"user", userID, "error", err)
	}
}

func (s *Server) grantAdministration(ctx context.Context, userID uuid.UUID) error {
	const roleName = "Administrator"

	role, err := s.authz.EnsureRole(ctx, roleName,
		"Full authority over this server", authz.PermServerAdminister)
	if err != nil {
		return err
	}

	return s.authz.AssignRole(ctx, userID, role.ID, nil)
}

// handleCurrentPermissions tells a client what the signed-in user may do.
//
// The client uses this to decide what to show. It is a courtesy, not
// enforcement: every action is checked again on the server.
func (s *Server) handleCurrentPermissions(c *echo.Context) error {
	permissions := currentPermissions(c)

	allowed := make([]string, 0)
	for _, definition := range authz.Catalogue() {
		if permissions.AllowsServer(definition.Name).Allowed {
			allowed = append(allowed, string(definition.Name))
		}
	}

	return c.JSON(http.StatusOK, map[string]any{"permissions": allowed})
}

// handlePermissionCatalogue lists every permission the server understands.
func (s *Server) handlePermissionCatalogue(c *echo.Context) error {
	definitions := authz.Catalogue()

	response := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		scopes := make([]string, 0, len(definition.Scopes))
		for _, scope := range definition.Scopes {
			scopes = append(scopes, string(scope))
		}
		response = append(response, map[string]any{
			"name":           string(definition.Name),
			"description":    definition.Description,
			"scopes":         scopes,
			"administrative": definition.Administrative,
		})
	}

	return c.JSON(http.StatusOK, response)
}

// handleReadAudit returns audit entries.
//
// Reading the log is itself audited, so that access to the record is part of
// the record.
func (s *Server) handleReadAudit(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	query := audit.Query{Limit: 100}
	if raw := c.QueryParam("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			query.Limit = int32(parsed)
		}
	}
	if action := c.QueryParam("action"); action != "" {
		query.Action = &action
	}

	entries, err := s.audit.List(ctx, query)
	if err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Entry{
		ActorID:    &user.ID,
		ActorLabel: user.Username,
		Action:     audit.ActionAuditRead,
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
	}); err != nil {
		return err
	}

	response := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		item := map[string]any{
			"id":         entry.ID,
			"occurredAt": entry.OccurredAt.Format(time.RFC3339),
			"actor":      entry.ActorLabel,
			"action":     entry.Action,
			"detail":     string(entry.Detail),
		}
		if entry.TargetKind != "" {
			item["targetKind"] = entry.TargetKind
			item["targetId"] = entry.TargetID
		}
		if entry.SourceIp != nil {
			item["sourceIp"] = entry.SourceIp.String()
		}
		response = append(response, item)
	}

	return c.JSON(http.StatusOK, response)
}
