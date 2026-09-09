package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/labstack/echo/v5"
)

/*
Which introductions somebody has already been given.

Kept on the account rather than in a browser so an introduction shown on the
web client is not shown again on the desktop one. A tour is an obstacle to
anybody who has seen it, and that includes the same person on a second device.

The server does not know what a tour is. It stores a small structure the client
defines and hands it back unchanged — which is what lets a phase add an
introduction without a migration. What is checked here is shape and size, not
meaning.
*/

/*
tourProgressLimit is the largest record this will store.

Generous for what it holds — a list of names and a step number each — and small
enough that a program writing to this cannot use somebody's account as
storage. Without a limit the only bound is the request body size, which is set
for attachments.
*/
const tourProgressLimit = 4 << 10

// handleGetTourProgress reports which introductions have been given.
func (s *Server) handleGetTourProgress(c *echo.Context) error {
	user, _ := currentUser(c)

	stored, err := s.queries.GetTourProgress(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	/*
		An account that has never stored anything holds the column default.
		Returned as an empty object rather than null so a client can read it
		into its own structure without a special case for "never set".
	*/
	if len(stored) == 0 {
		return c.JSONBlob(http.StatusOK, []byte("{}"))
	}

	return c.JSONBlob(http.StatusOK, stored)
}

/*
handleSetTourProgress records it.

The whole structure, replaced rather than merged. The client reads it, changes
it and writes it back, which is the only ordering that keeps "seen" and
"reached" consistent — a merge would have to know that finishing a tour makes
its step number meaningless.
*/
func (s *Server) handleSetTourProgress(c *echo.Context) error {
	user, _ := currentUser(c)

	body := http.MaxBytesReader(c.Response(), c.Request().Body, tourProgressLimit)
	raw, err := io.ReadAll(body)
	if err != nil {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge,
			"that is more than this holds; it is a short record of which introductions you have seen")
	}

	/*
		An object, and nothing else.

		The client reads this into a structure with named fields, so an array
		or a number would be a runtime error in the interface rather than a
		refusal here — and this is an API a program calls directly, with
		nothing upstream having already shaped the value.
	*/
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &shape); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			"the record could not be read; it should be a JSON object")
	}

	if err := s.queries.SetTourProgress(c.Request().Context(), dbgen.SetTourProgressParams{
		ID:           user.ID,
		TourProgress: raw,
	}); err != nil {
		return err
	}

	return c.JSONBlob(http.StatusOK, raw)
}
