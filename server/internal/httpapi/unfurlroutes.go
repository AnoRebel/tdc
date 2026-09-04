package httpapi

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/unfurl"
)

// previewResponse is what a client shows under a message carrying a link.
type previewResponse struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	SiteName    string `json:"siteName,omitempty"`
	// ImageURL is the picture the page nominated, and is never fetched by this
	// server — see the unfurl package. A client decides under its own policy
	// whether to load it, which is why it arrives as a foreign URL rather than
	// as something stored here.
	ImageURL string `json:"imageUrl,omitempty"`
}

type previewRequest struct {
	URL string `json:"url"`
}

/*
handlePreviewLink returns what a link is, or nothing.

A POST rather than a GET with the URL in the query string, deliberately: a
link someone was sent is private, and a query string ends up in access logs,
proxy logs and browser history. The body does not.
*/
func (s *Server) handlePreviewLink(c *echo.Context) error {
	user, _ := currentUser(c)
	ctx := c.Request().Context()

	if s.unfurl == nil || !s.unfurl.Enabled() {
		// The operator has switched previews off entirely. Reported as "there
		// is nothing here" rather than as an error, because from a client's
		// point of view that is exactly what it is.
		return c.NoContent(http.StatusNoContent)
	}

	var request previewRequest
	if err := c.Bind(&request); err != nil || request.URL == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "no link was given")
	}

	// The person's own choice is read here rather than trusted from the
	// client: someone who has turned previews off must not have a request made
	// on their behalf by a client that forgot.
	enabled, err := s.queries.GetLinkPreviewPreference(ctx, user.ID)
	if err != nil {
		return err
	}

	preview, err := s.unfurl.Preview(ctx, request.URL, unfurl.UserPreference{Enabled: enabled})
	if err != nil {
		if errors.Is(err, unfurl.ErrNotPreviewable) {
			return c.NoContent(http.StatusNoContent)
		}
		return err
	}

	return c.JSON(http.StatusOK, previewResponse{
		URL:         preview.URL,
		Title:       preview.Title,
		Description: preview.Description,
		SiteName:    preview.SiteName,
		ImageURL:    preview.ImageURL,
	})
}

type previewPreferenceRequest struct {
	Enabled bool `json:"enabled"`
}

// handleSetPreviewPreference records whether someone wants links fetched.
func (s *Server) handleSetPreviewPreference(c *echo.Context) error {
	user, _ := currentUser(c)

	var request previewPreferenceRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if err := s.queries.SetLinkPreviewPreference(c.Request().Context(),
		dbgen.SetLinkPreviewPreferenceParams{
			ID:                  user.ID,
			LinkPreviewsEnabled: request.Enabled,
		}); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
