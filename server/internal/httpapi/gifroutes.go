package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/gifs"
)

// gifSearchResponse is what a picker needs to show and send a result.
type gifSearchResponse struct {
	// Provider names the library, which both providers' terms require a client
	// to attribute.
	Provider string     `json:"provider"`
	Results  []gifs.GIF `json:"results"`
}

/*
handleSearchGifs proxies a search to the configured library.

Proxied rather than called from the browser, for two reasons. The operator's
key stays on the server. And the provider sees one server making requests
rather than an address and a request per keystroke for every person using it —
searching a GIF library should not hand a third party a profile of who is
typing what.

Answers 503 when nothing is configured, which is the default. A client asks
/api/server for whether the feature exists and hides the control, so reaching
this at all means something is out of step.
*/
func (s *Server) handleSearchGifs(c *echo.Context) error {
	if s.gifs == nil || !s.gifs.Enabled() {
		return echo.NewHTTPError(http.StatusServiceUnavailable,
			"gif search is not enabled on this server")
	}

	limit := 0
	if raw := c.QueryParam("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}

	// An empty query is not an error: it asks the provider for what is
	// popular, so opening the picker shows something to choose from.
	results, err := s.gifs.Search(c.Request().Context(), c.QueryParam("q"), limit)
	if errors.Is(err, gifs.ErrDisabled) {
		return echo.NewHTTPError(http.StatusServiceUnavailable,
			"gif search is not enabled on this server")
	}
	if err != nil {
		// The service has already logged what happened, with the provider's
		// own message. What reaches the caller says only that it failed: the
		// provider's message can quote the request, and the key with it.
		return echo.NewHTTPError(http.StatusBadGateway,
			"the gif provider could not be reached")
	}

	return c.JSON(http.StatusOK, gifSearchResponse{
		Provider: string(s.gifs.Provider()),
		Results:  results,
	})
}
