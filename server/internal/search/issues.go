package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
IssueSearcher finds issues, and what was said about them.

A sibling of Searcher rather than a second implementation of it: the same
Postgres full-text machinery, the same rule that authorisation is a join in the
query rather than a filter afterwards, and the same Service behind both. An
operator who points message search somewhere else one day can point this with
it.
*/
type IssueSearcher interface {
	// SearchIssues finds issues matching a query and every filter given,
	// among the projects the person searching may see.
	SearchIssues(ctx context.Context, userID uuid.UUID, query IssueQuery) ([]IssueResult, error)
	// SearchDiscussions finds what was said in the discussions of issues the
	// person may see.
	SearchDiscussions(ctx context.Context, userID uuid.UUID, query DiscussionQuery) ([]DiscussionResult, error)
}

var _ IssueSearcher = (*Service)(nil)

/*
IssueQuery is what to look for among issues.

Text may be empty, which searches by the filters alone. Filters name states,
labels and priorities rather than identifying them, because a search across
projects has no single project's identifiers to use: "Done" and "bug" mean the
same thing to somebody searching everything they can see. Every filter given
must hold.
*/
type IssueQuery struct {
	Text string
	// Prefix asks for search-as-you-type over titles, which a word search
	// cannot do: "logi" finds nothing there however many issues say "login".
	Prefix bool

	Project  *uuid.UUID
	State    string
	Label    string
	Priority string
	// Assignee is somebody the issue must be assigned to.
	Assignee *uuid.UUID
	// Completed, when set, keeps only finished or only unfinished issues.
	Completed *bool

	Limit int
}

// IssueResult is one issue that matched.
type IssueResult struct {
	ID          uuid.UUID
	Key         string
	Number      int64
	Title       string
	ProjectKey  string
	ProjectName string
	State       string
	Completed   bool
	UpdatedAt   time.Time
	// Highlighted is the title and description with the matched words marked,
	// when the search had words to mark.
	Highlighted string
}

// DiscussionQuery is what to look for in issues' discussions.
type DiscussionQuery struct {
	Text string
	// Issue narrows the search to one issue's discussion.
	Issue *uuid.UUID
	// Project narrows it to one project's issues.
	Project *uuid.UUID
	Limit   int
}

// DiscussionResult is one message that matched, and the issue it is about.
type DiscussionResult struct {
	EventID     string
	Sender      string
	SentAt      int64
	IssueID     uuid.UUID
	IssueKey    string
	IssueTitle  string
	Highlighted string
}

// SearchIssues implements IssueSearcher.
func (s *Service) SearchIssues(ctx context.Context, userID uuid.UUID, query IssueQuery) ([]IssueResult, error) {
	text := strings.TrimSpace(query.Text)
	limit := query.Limit
	if limit <= 0 || limit > maxResults {
		limit = maxResults
	}

	if query.Prefix && text != "" {
		rows, err := s.queries.SearchIssueTitlesFuzzy(ctx, dbgen.SearchIssueTitlesFuzzyParams{
			UserID: userID, Query: text, ProjectID: query.Project, ResultLimit: int32(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("search issue titles: %w", err)
		}
		results := make([]IssueResult, 0, len(rows))
		for _, row := range rows {
			results = append(results, IssueResult{
				ID: row.ID, Key: fmt.Sprintf("%s-%d", row.ProjectKey, row.Number), Number: row.Number,
				Title: row.Title, ProjectKey: row.ProjectKey, ProjectName: row.ProjectName,
				State: row.StateName, Completed: row.IsCompletion, UpdatedAt: row.UpdatedAt,
			})
		}
		return results, nil
	}

	rows, err := s.queries.SearchIssues(ctx, dbgen.SearchIssuesParams{
		Query:        text,
		UserID:       userID,
		ProjectID:    query.Project,
		StateName:    strings.TrimSpace(query.State),
		Completed:    query.Completed,
		LabelName:    strings.TrimSpace(query.Label),
		PriorityName: strings.TrimSpace(query.Priority),
		AssigneeID:   query.Assignee,
		ResultLimit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	results := make([]IssueResult, 0, len(rows))
	for _, row := range rows {
		result := IssueResult{
			ID: row.ID, Key: fmt.Sprintf("%s-%d", row.ProjectKey, row.Number), Number: row.Number,
			Title: row.Title, ProjectKey: row.ProjectKey, ProjectName: row.ProjectName,
			State: row.StateName, Completed: row.IsCompletion, UpdatedAt: row.UpdatedAt,
		}
		// Only where there were words to mark: a filter-only search would
		// otherwise return the text back with nothing highlighted, which
		// reads like a match on nothing.
		if text != "" {
			result.Highlighted = row.Highlighted
		}
		results = append(results, result)
	}
	return results, nil
}

// SearchDiscussions implements IssueSearcher.
func (s *Service) SearchDiscussions(ctx context.Context, userID uuid.UUID, query DiscussionQuery) ([]DiscussionResult, error) {
	text := strings.TrimSpace(query.Text)
	if text == "" {
		// Nothing to look for. Running it would list every discussion.
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 || limit > maxResults {
		limit = maxResults
	}
	rows, err := s.queries.SearchIssueDiscussions(ctx, dbgen.SearchIssueDiscussionsParams{
		Query: text, UserID: userID, IssueID: query.Issue, ProjectID: query.Project, ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search discussions: %w", err)
	}
	results := make([]DiscussionResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, DiscussionResult{
			EventID: row.EventID, Sender: row.SenderAddress, SentAt: row.SentAt,
			IssueID: row.IssueID, IssueKey: fmt.Sprintf("%s-%d", row.ProjectKey, row.Number),
			IssueTitle: row.Title, Highlighted: row.Highlighted,
		})
	}
	return results, nil
}
