package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/directory"
)

/*
Searching and browsing the directory.

Browsing is a search with no filters. Both are paged at a size the server
decides, rate limited per person or address, and answered from directory
profiles alone — never from the member list, whose search is untouched by any
of this.
*/

func init() {
	documentRoutes(map[string]entry{
		"GET /api/directory/search": {Summary: "Search the directory", Authentication: "optional"},
	})
}

func (api *directoryAPI) mountSearch(directoryGroup *echo.Group) {
	directoryGroup.GET("/search", api.handleSearchDirectory)
}

type foundView struct {
	Username         string   `json:"username"`
	DisplayName      string   `json:"displayName"`
	Address          string   `json:"address"`
	Headline         string   `json:"headline,omitempty"`
	Location         string   `json:"location,omitempty"`
	Available        bool     `json:"available,omitempty"`
	AvailabilityNote string   `json:"availabilityNote,omitempty"`
	Skills           []string `json:"skills,omitempty"`
	UpdatedAt        string   `json:"updatedAt"`
}

type searchResultsView struct {
	Profiles []foundView `json:"profiles"`
	Page     int         `json:"page"`
	// PageSize is the server's, whatever was asked for; a client pages by
	// asking for the next page, never for a bigger one.
	PageSize int  `json:"pageSize"`
	More     bool `json:"more"`
}

/*
handleSearchDirectory answers one page of a directory search.

Filters: q (a name, or words in a headline or about section the viewer may
see), skill, available=true, location, company, page. There is deliberately no
parameter for the page size: a client cannot ask for more than one page.
*/
func (api *directoryAPI) handleSearchDirectory(c *echo.Context) error {
	rank, _, allowed := api.viewerRank(c)
	if !allowed {
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}
	if err := api.withinReadLimit(c); err != nil {
		return err
	}

	page, _ := strconv.Atoi(c.QueryParam("page"))
	filters := directory.Filters{
		Query:     c.QueryParam("q"),
		Skill:     c.QueryParam("skill"),
		Available: c.QueryParam("available") == "true",
		Location:  c.QueryParam("location"),
		Company:   c.QueryParam("company"),
		Page:      page,
	}

	results, err := api.directory.Search(c.Request().Context(), rank, filters)
	if err != nil {
		return err
	}

	view := searchResultsView{
		Profiles: make([]foundView, 0, len(results.Profiles)),
		Page:     max(page, 0),
		PageSize: directory.PageSize,
		More:     results.More,
	}
	for _, found := range results.Profiles {
		view.Profiles = append(view.Profiles, foundView{
			Username: found.Username, DisplayName: found.DisplayName,
			Address:  api.rooms.UserAddress(found.Username),
			Headline: found.Headline, Location: found.Location,
			Available: found.Available, AvailabilityNote: found.AvailabilityNote,
			Skills: found.Skills, UpdatedAt: found.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return c.JSON(http.StatusOK, view)
}
