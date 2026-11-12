package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
)

/*
History is one issue's history: its project's stream, filtered to it.

Read, never written. There is no function in this package that updates or
deletes an event, and the stream's room has no members, so the rooms package's
own paths for editing and redacting — which require membership — cannot reach
it either. What was recorded stays recorded.
*/
func (s *Service) History(ctx context.Context, access Access, issue dbgen.Issue) ([]dbgen.Event, error) {
	history, err := s.queries.ListIssueEvents(ctx, dbgen.ListIssueEventsParams{
		ProjectID: access.Project.ID,
		IssueID:   issue.ID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	return history, nil
}

/*
Replayed is what an issue's history says it is now.

The subset the columns cache, which is what makes replaying useful: a replay
that agrees with the columns is the evidence that the columns are what the
stream says, rather than something that drifted from it.
*/
type Replayed struct {
	Number      int64
	Title       string
	Description string
	StateID     uuid.UUID
	Deleted     bool
}

/*
Replay reconstructs every issue in a stream from its events alone.

Nothing is read from the issue tables. Events are applied in the order the
stream holds them, so a state an issue passed through and left does not win
over the one it is in.
*/
func Replay(stream []dbgen.Event) (map[uuid.UUID]Replayed, error) {
	issues := make(map[uuid.UUID]Replayed)
	for _, event := range stream {
		if !strings.HasPrefix(event.Type, "issue.") {
			continue
		}

		var content struct {
			Issue       string `json:"issue"`
			Key         string `json:"key"`
			Title       string `json:"title"`
			Description string `json:"description"`
			State       string `json:"state"`
			New         any    `json:"new"`
		}
		if err := json.Unmarshal(event.Content, &content); err != nil {
			return nil, fmt.Errorf("event %s: %w", event.ID, err)
		}
		id, err := uuid.Parse(content.Issue)
		if err != nil {
			return nil, fmt.Errorf("event %s names no issue: %w", event.ID, err)
		}
		issue := issues[id]

		switch events.Type(event.Type) {
		case events.TypeIssueCreated:
			_, number, _ := ParseIdentifier(content.Key)
			state, err := uuid.Parse(content.State)
			if err != nil {
				return nil, fmt.Errorf("event %s names no state: %w", event.ID, err)
			}
			issue = Replayed{Number: number, Title: content.Title, Description: content.Description, StateID: state}
		case events.TypeIssueStateChanged:
			state, err := uuid.Parse(fmt.Sprint(content.New))
			if err != nil {
				return nil, fmt.Errorf("event %s names no state: %w", event.ID, err)
			}
			issue.StateID = state
		case events.TypeIssueTitleChanged:
			issue.Title = fmt.Sprint(content.New)
		case events.TypeIssueDescriptionChanged:
			issue.Description = fmt.Sprint(content.New)
		case events.TypeIssueDeleted:
			issue.Deleted = true
		}
		issues[id] = issue
	}
	return issues, nil
}

// replayPage bounds one read of a stream while replaying it.
const replayPage = 500

// Stream reads a project's whole stream, in order.
func (s *Service) Stream(ctx context.Context, projectID uuid.UUID) ([]dbgen.Event, error) {
	var all []dbgen.Event
	var after int64
	for {
		page, err := s.queries.ListProjectStream(ctx, dbgen.ListProjectStreamParams{
			ProjectID: projectID, AfterSequence: after, PageSize: replayPage,
		})
		if err != nil {
			return nil, fmt.Errorf("read stream: %w", err)
		}
		all = append(all, page...)
		if len(page) < replayPage {
			return all, nil
		}
		after = page[len(page)-1].Sequence
	}
}

// defaultHistoryPage is how much of a project's own history one read returns.
const defaultHistoryPage = 50

/*
ProjectHistory is what happened to a project itself — its details, members,
states, labels, priorities, cycles and milestones — newest first, a page
before a position in its stream; zero is the newest.

Read, never written, like an issue's history, and for the same reason: what was
recorded stays recorded.
*/
func (s *Service) ProjectHistory(ctx context.Context, access Access, before int64, limit int) ([]dbgen.Event, error) {
	if limit <= 0 || limit > maxIssuePage {
		limit = defaultHistoryPage
	}
	history, err := s.queries.ListProjectHistory(ctx, dbgen.ListProjectHistoryParams{
		ProjectID: access.Project.ID, BeforeSequence: before, PageSize: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("read project history: %w", err)
	}
	return history, nil
}
