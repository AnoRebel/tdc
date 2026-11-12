package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/directory"
	"github.com/AnoRebel/tdc/server/internal/places"
)

/*
Where somebody is: one setting for the account, shown wherever they are.

Others never read it through these routes. Every query that returns a person
to somebody else asks person_location() for it, with the viewer's rank, so the
rule is stated once in SQL and a handler cannot widen it.
*/

// maxPlace is the longest a location may be, as the directory's was.
const maxPlace = 80

const (
	locationManual    = "manual"
	locationAutomatic = "automatic"
)

type locationView struct {
	Place      string     `json:"place"`
	Mode       string     `json:"mode"`
	Visibility string     `json:"visibility"`
	DetectedAt *time.Time `json:"detectedAt,omitempty"`
}

type setLocationRequest struct {
	Place      string `json:"place"`
	Mode       string `json:"mode"`
	Visibility string `json:"visibility"`
}

/*
detectLocationRequest is a position, read once and dropped.

The coordinates are matched to a town and go no further: not stored, not
logged, not in an error message. Only the town's name is kept.
*/
type detectLocationRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

/*
viewerRankOf is how much of a person somebody may see.

'members' means signed in on this server. A call guest has a session but no
account here — and one from another server is somebody else's member — so both
see only what is public, as anybody else from outside would.
*/
func viewerRankOf(user dbgen.User) directory.Rank {
	if user.GuestCall != nil || user.RemoteServer != nil {
		return directory.Anonymous
	}
	return directory.Member
}

func (s *Server) ownLocation(c *echo.Context, user dbgen.User) (locationView, error) {
	row, err := s.queries.GetOwnLocation(c.Request().Context(), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Nobody has a location until they set one, and it starts hidden.
		return locationView{Mode: locationManual, Visibility: string(directory.Hidden)}, nil
	}
	if err != nil {
		return locationView{}, err
	}
	return locationView{Place: row.Place, Mode: row.Mode, Visibility: row.Visibility, DetectedAt: row.DetectedAt}, nil
}

func (s *Server) handleGetLocation(c *echo.Context) error {
	user, _ := currentUser(c)
	view, err := s.ownLocation(c, user)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, view)
}

/*
handleSetLocation saves the whole setting.

In automatic mode the place is whatever was last detected — a client cannot
type one in and call it detected — so the place sent is used only in manual
mode.
*/
func (s *Server) handleSetLocation(c *echo.Context) error {
	user, _ := currentUser(c)
	var request setLocationRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}
	if request.Visibility == "" {
		request.Visibility = string(directory.Hidden)
	}
	if !directory.Visibility(request.Visibility).Valid() {
		return echo.NewHTTPError(http.StatusBadRequest, `visibility must be "public", "members" or "hidden"`)
	}
	if request.Mode == "" {
		request.Mode = locationManual
	}

	place := strings.TrimSpace(request.Place)
	switch request.Mode {
	case locationManual:
		if utf8.RuneCountInString(place) > maxPlace {
			return echo.NewHTTPError(http.StatusBadRequest, "place: at most 80 characters")
		}
	case locationAutomatic:
		current, err := s.ownLocation(c, user)
		if err != nil {
			return err
		}
		place = current.Place
	default:
		return echo.NewHTTPError(http.StatusBadRequest, `mode must be "manual" or "automatic"`)
	}

	row, err := s.queries.SetOwnLocation(c.Request().Context(), dbgen.SetOwnLocationParams{
		UserID: user.ID, Place: place, Mode: request.Mode, Visibility: request.Visibility,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, locationView{
		Place: row.Place, Mode: row.Mode, Visibility: row.Visibility, DetectedAt: row.DetectedAt,
	})
}

/*
handleDetectLocation turns a position into a town and keeps the town.

Matched against the list compiled into the server, so the position reaches
nobody else. Detecting puts the setting in automatic mode, since that is the
only reason to ask; the visibility is left as it was.
*/
func (s *Server) handleDetectLocation(c *echo.Context) error {
	user, _ := currentUser(c)
	var request detectLocationRequest
	if err := c.Bind(&request); err != nil || request.Latitude == nil || request.Longitude == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "send a latitude and a longitude")
	}
	if !places.Valid(*request.Latitude, *request.Longitude) {
		return echo.NewHTTPError(http.StatusBadRequest, "that is not a position on Earth")
	}
	place, found := places.Nearest(*request.Latitude, *request.Longitude)
	if !found {
		return echo.NewHTTPError(http.StatusNotFound, "there is no town near there to name")
	}

	current, err := s.ownLocation(c, user)
	if err != nil {
		return err
	}
	now := time.Now()
	row, err := s.queries.SetOwnLocation(c.Request().Context(), dbgen.SetOwnLocationParams{
		UserID: user.ID, Place: place, Mode: locationAutomatic,
		Visibility: current.Visibility, DetectedAt: &now,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, locationView{
		Place: row.Place, Mode: row.Mode, Visibility: row.Visibility, DetectedAt: row.DetectedAt,
	})
}
