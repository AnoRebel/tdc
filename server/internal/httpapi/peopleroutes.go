package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Who is on this server, for an administrator.

Distinct from the ordinary directory a member browses. What is here is the
information a moderator needs to act — whether somebody is suspended, and until
when — and nothing more. An email address is not among it: an administrator
suspending somebody does not need to read their address to do it, and a list
that carried every address would be one screenshot away from being a mailing
list somebody else has.
*/

type personResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// Address is the federated form, which is how somebody is named everywhere
	// else in the interface.
	Address   string `json:"address"`
	CreatedAt string `json:"createdAt"`

	// Suspended says they cannot sign in. Present rather than inferred from
	// the dates, so a client does not have to reimplement the rule.
	Suspended bool `json:"suspended"`
	// SuspendedUntil is when it lifts on its own, absent when it does not.
	SuspendedUntil string `json:"suspendedUntil,omitempty"`
	// SuspensionNote is why, which is what makes a suspension reviewable by
	// somebody who did not impose it.
	SuspensionNote string `json:"suspensionNote,omitempty"`
	// FormerUsernames are the names this account gave up, newest first, so
	// an administrator can tell who somebody was before.
	FormerUsernames []string `json:"formerUsernames,omitempty"`
}

func (s *Server) handleListPeople(c *echo.Context) error {
	limit := int32(100)
	if raw := c.QueryParam("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = int32(parsed)
		}
	}

	// Paged, so "every account" means every account on a server with more of
	// them than one page holds.
	offset := int32(0)
	if raw := c.QueryParam("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= math.MaxInt32 {
			offset = int32(parsed)
		}
	}

	people, err := s.queries.ListUsers(c.Request().Context(), dbgen.ListUsersParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return err
	}

	// One query for the page's former names, not one per person.
	ids := make([]uuid.UUID, 0, len(people))
	for _, person := range people {
		ids = append(ids, person.ID)
	}
	former, err := s.queries.ListFormerUsernamesFor(c.Request().Context(), ids)
	if err != nil {
		return err
	}
	formerByUser := map[uuid.UUID][]string{}
	for _, row := range former {
		formerByUser[row.UserID] = append(formerByUser[row.UserID], row.Username)
	}

	response := make([]personResponse, 0, len(people))
	for _, person := range people {
		item := s.personResponse(person)
		item.FormerUsernames = formerByUser[person.ID]
		response = append(response, item)
	}

	return c.JSON(http.StatusOK, response)
}

func (s *Server) personResponse(person dbgen.User) personResponse {
	item := personResponse{
		ID:          person.ID.String(),
		Username:    person.Username,
		DisplayName: person.DisplayName,
		Address:     person.Username + "@" + s.serverName(),
		CreatedAt:   person.CreatedAt.UTC().Format(time.RFC3339),
		Suspended:   person.SuspendedAt != nil,
	}

	if person.SuspendedUntil != nil {
		item.SuspendedUntil = person.SuspendedUntil.UTC().Format(time.RFC3339)
	}
	if person.SuspensionNote != nil {
		item.SuspensionNote = *person.SuspensionNote
	}

	return item
}
