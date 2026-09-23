package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
DiscussionVisible reports whether somebody may see an issue's discussion.

Asked by the rooms package on every read of a discussion room, which is how a
discussion is exactly as visible as its issue: a deleted issue's discussion is
gone with it, and a private project's discussions exist only for its members.
*/
func (s *Service) DiscussionVisible(ctx context.Context, roomID, userID uuid.UUID) (bool, error) {
	issue, err := s.queries.GetIssueByID(ctx, roomID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read issue: %w", err)
	}
	if issue.DeletedAt != nil {
		return false, nil
	}
	return s.Visible(ctx, issue.ProjectID, userID)
}

/*
OriginOf returns the message an issue was made from, if it was.

Read from the issue's creation event, which is the only place it is recorded:
the issue row carries no copy, so there is nothing to fall out of step with the
history.
*/
func (s *Service) OriginOf(ctx context.Context, access Access, issue dbgen.Issue) (*Origin, error) {
	created, err := s.queries.GetIssueCreation(ctx, dbgen.GetIssueCreationParams{
		ProjectID: access.Project.ID, IssueID: issue.ID.String(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read creation: %w", err)
	}

	var content struct {
		Channel string `json:"originChannel"`
		Event   string `json:"originEvent"`
	}
	if err := json.Unmarshal(created.Content, &content); err != nil {
		return nil, fmt.Errorf("decode creation: %w", err)
	}
	if content.Channel == "" || content.Event == "" {
		return nil, nil
	}
	room, err := uuid.Parse(content.Channel)
	if err != nil {
		return nil, nil
	}
	return &Origin{RoomID: room, EventID: content.Event}, nil
}
