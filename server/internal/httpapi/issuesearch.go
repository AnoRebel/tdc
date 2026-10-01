package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/search"
)

/*
Searching issues, and what was said about them.

The same search service messages use, over a table of its own, with the same
rule: the project's visibility is joined into the query, so an issue in a
private project somebody is not in is never read on their behalf — not read
and then hidden.
*/
func init() {
	registerRoutes(func(s *Server) {
		s.echo.GET("/api/issues/search", s.handleSearchIssues, requireAuthentication, s.requireProjects)
		s.echo.GET("/api/issues/:ref/discussion/search", s.handleSearchDiscussion,
			requireAuthentication, s.requireProjects)
	})
	documentRoutes(map[string]entry{
		"GET /api/issues/search":                 {Summary: "Search issues, and their discussions", Authentication: "session or token"},
		"GET /api/issues/:ref/discussion/search": {Summary: "Search one issue's discussion", Authentication: "session or token"},
	})
}

// issueSearchResult is one issue that matched.
type issueSearchResult struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	ProjectKey  string    `json:"projectKey"`
	ProjectName string    `json:"projectName"`
	State       string    `json:"state"`
	Completed   bool      `json:"completed"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Highlighted string    `json:"highlighted,omitempty"`
}

// discussionSearchResult is one message about an issue that matched.
type discussionSearchResult struct {
	EventID     string    `json:"eventId"`
	Sender      string    `json:"sender"`
	SentAt      time.Time `json:"sentAt"`
	IssueKey    string    `json:"issueKey"`
	IssueTitle  string    `json:"issueTitle"`
	Highlighted string    `json:"highlighted"`
}

func discussionResults(found []search.DiscussionResult) []discussionSearchResult {
	results := make([]discussionSearchResult, 0, len(found))
	for _, result := range found {
		results = append(results, discussionSearchResult{
			EventID: result.EventID, Sender: result.Sender, SentAt: time.UnixMilli(result.SentAt).UTC(),
			IssueKey: result.IssueKey, IssueTitle: result.IssueTitle, Highlighted: result.Highlighted,
		})
	}
	return results
}

/*
handleSearchIssues finds issues across every project the caller may see.

Text and filters combine: every one given must hold. A project named by a key
the caller cannot see answers with nothing, as a project that does not exist
would — narrowing to it is not a way of learning it is there.
*/
func (s *Server) handleSearchIssues(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	query := search.IssueQuery{
		Text:     c.QueryParam("q"),
		Prefix:   c.QueryParam("prefix") == "true",
		State:    c.QueryParam("state"),
		Label:    c.QueryParam("label"),
		Priority: c.QueryParam("priority"),
	}
	query.Limit, _ = strconv.Atoi(c.QueryParam("limit"))
	empty := map[string]any{"issues": []issueSearchResult{}, "discussions": []discussionSearchResult{}}

	if key := c.QueryParam("project"); key != "" {
		access, err := s.projects.ProjectByKey(ctx, actorOf(c), key)
		if err != nil {
			return c.JSON(http.StatusOK, empty)
		}
		query.Project = &access.Project.ID
	}
	if username := c.QueryParam("assignee"); username != "" {
		person, err := s.queries.GetUserByUsername(ctx, identity.NormaliseUsername(username))
		if err != nil {
			return c.JSON(http.StatusOK, empty)
		}
		query.Assignee = &person.ID
	}
	if completed := c.QueryParam("completed"); completed != "" {
		value := completed == "true"
		query.Completed = &value
	}

	issues, err := s.search.SearchIssues(ctx, user.ID, query)
	if err != nil {
		return err
	}
	results := make([]issueSearchResult, 0, len(issues))
	for _, issue := range issues {
		results = append(results, issueSearchResult{
			ID: issue.ID.String(), Key: issue.Key, Title: issue.Title,
			ProjectKey: issue.ProjectKey, ProjectName: issue.ProjectName,
			State: issue.State, Completed: issue.Completed, UpdatedAt: issue.UpdatedAt,
			Highlighted: issue.Highlighted,
		})
	}

	// What was said about issues, alongside the issues themselves — unless
	// the search was only filters, where there are no words to find.
	discussions := []discussionSearchResult{}
	if query.Text != "" && !query.Prefix && c.QueryParam("discussions") != "false" {
		found, err := s.search.SearchDiscussions(ctx, user.ID, search.DiscussionQuery{
			Text: query.Text, Project: query.Project, Limit: query.Limit,
		})
		if err != nil {
			return err
		}
		discussions = discussionResults(found)
	}

	return c.JSON(http.StatusOK, map[string]any{"issues": results, "discussions": discussions})
}

// handleSearchDiscussion searches one issue's discussion, for anybody who
// may see the issue — whether or not they have ever taken part in it.
func (s *Server) handleSearchDiscussion(c *echo.Context) error {
	ctx := c.Request().Context()
	_, issue, err := s.issueFrom(c)
	if err != nil {
		return err
	}
	user, _ := currentUser(c)
	found, err := s.search.SearchDiscussions(ctx, user.ID, search.DiscussionQuery{
		Text: c.QueryParam("q"), Issue: &issue.ID,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, discussionResults(found))
}
