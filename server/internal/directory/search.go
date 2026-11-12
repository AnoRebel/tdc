package directory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
The ceilings a search runs under.

A page is at most PageSize results whatever the request asks for, and paging
stops at MaxPage. Together with the rate limit in front of the route they are
what makes the directory a place to look for somebody rather than a way to copy
everybody: a caller cannot take the whole table in one request, nor walk it
quickly in many.
*/
const (
	PageSize = 20
	MaxPage  = 25
)

// Filters narrow a directory search. Every one is optional.
type Filters struct {
	// Query matches a name, and a headline or about section the viewer may
	// see.
	Query string
	// Skill is a name the vocabulary knows, or another spelling of one.
	Skill string
	// Available keeps only people whose availability is current.
	Available bool
	Location  string
	// Company is a company's address or part of its name.
	Company string
	Page    int
}

// Found is one profile in a set of results, as the viewer may see it.
type Found struct {
	Username         string
	DisplayName      string
	Headline         string
	Location         string
	Available        bool
	AvailabilityNote string
	Skills           []string
	UpdatedAt        time.Time
}

// Results are one page of a search.
type Results struct {
	Profiles []Found
	// More says there is another page, without saying how many.
	More bool
}

/*
Search looks through the directory as a viewer of the given rank.

A skill that is not in the vocabulary finds nobody rather than being ignored:
a filter the caller asked for and did not get would return people who do not
have the skill. A proposed one finds nobody too, because the query matches
active skills only — so proposing a skill cannot make its proposer findable by
it before a reviewer agrees it exists.
*/
func (s *Service) Search(ctx context.Context, rank Rank, filters Filters) (Results, error) {
	page := filters.Page
	if page < 0 {
		page = 0
	}
	if page >= MaxPage {
		return Results{Profiles: []Found{}}, nil
	}

	params := dbgen.SearchDirectoryProfilesParams{
		ViewerRank:    int16(rank),
		Query:         strings.TrimSpace(filters.Query),
		AvailableOnly: filters.Available,
		Location:      strings.TrimSpace(filters.Location),
		Company:       strings.TrimSpace(filters.Company),
		// One more than a page, to know whether there is another without
		// counting the table.
		ResultLimit:  PageSize + 1,
		ResultOffset: int32(page * PageSize),
	}

	if skill := strings.TrimSpace(filters.Skill); skill != "" {
		row, err := s.queries.ResolveSkill(ctx, FoldSkillName(skill))
		if errors.Is(err, pgx.ErrNoRows) {
			return Results{Profiles: []Found{}}, nil
		}
		if err != nil {
			return Results{}, fmt.Errorf("look up a skill: %w", err)
		}
		params.SkillID = &row.ID
	}

	rows, err := s.queries.SearchDirectoryProfiles(ctx, params)
	if err != nil {
		return Results{}, fmt.Errorf("search the directory: %w", err)
	}

	results := Results{Profiles: make([]Found, 0, len(rows))}
	if len(rows) > PageSize {
		results.More = page+1 < MaxPage
		rows = rows[:PageSize]
	}
	for _, row := range rows {
		results.Profiles = append(results.Profiles, Found{
			Username: row.Username, DisplayName: row.DisplayName,
			Headline: row.Headline, Location: row.Location,
			Available: row.Available, AvailabilityNote: row.AvailabilityNote,
			Skills: row.Skills, UpdatedAt: row.UpdatedAt,
		})
	}
	return results, nil
}
