package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
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
IsDiscussion reports whether a room is an issue's discussion, and returns the
issue if so.
*/
func (s *Service) IsDiscussion(ctx context.Context, room dbgen.Room) (dbgen.Issue, bool) {
	if room.Kind != "issue-discussion" {
		return dbgen.Issue{}, false
	}
	issue, err := s.queries.GetIssueByID(ctx, room.ID)
	if err != nil {
		return dbgen.Issue{}, false
	}
	return issue, true
}

/*
Participate makes somebody a participant in an issue's discussion: they hear
about everything said in it from now on, unless they have said otherwise.

Called for whoever posts there. Having spoken in a discussion is the plainest
sign of wanting to follow it, and the same thing a tracker's "watching" means —
without a separate button nobody knows to press.
*/
func (s *Service) Participate(ctx context.Context, roomID, userID uuid.UUID) error {
	if err := s.queries.WatchDiscussion(ctx, dbgen.WatchDiscussionParams{
		UserID: userID, RoomID: &roomID,
	}); err != nil {
		return fmt.Errorf("watch discussion: %w", err)
	}
	return nil
}

/*
AdmitMentioned brings people named in a discussion into it, so the mention
reaches them the way a mention in a channel reaches its members.

Only people who may see the issue. Naming somebody outside a private project
does not tell them anything — not that the issue exists, and not that they
were named — which is also what a mention of a non-member in a private
channel does. They are admitted as ordinary members, at their own level:
being named is not the same as wanting to follow every comment.
*/
func (s *Service) AdmitMentioned(ctx context.Context, roomID uuid.UUID, usernames []string) {
	for _, username := range usernames {
		person, err := s.queries.GetUserByUsername(ctx, identity.NormaliseUsername(username))
		if err != nil {
			continue
		}
		member, err := s.queries.IsRoomMember(ctx, dbgen.IsRoomMemberParams{RoomID: roomID, UserID: person.ID})
		if err != nil || member {
			continue
		}
		visible, err := s.DiscussionVisible(ctx, roomID, person.ID)
		if err != nil || !visible {
			continue
		}
		if err := s.rooms.AddMemberByInvitation(ctx, roomID, person.ID); err != nil {
			s.log.Warn("cannot admit somebody mentioned in a discussion",
				"roomId", roomID, "error", err)
		}
	}
}

// IssueFromRoom is an issue made from one of a room's messages.
type IssueFromRoom = dbgen.ListIssuesFromRoomRow

/*
IssuesFromRoom lists the issues made from a room's messages, that the reader
may see. The caller has already established that the reader may see the room.
*/
func (s *Service) IssuesFromRoom(ctx context.Context, roomID, userID uuid.UUID) ([]IssueFromRoom, error) {
	issues, err := s.queries.ListIssuesFromRoom(ctx, dbgen.ListIssuesFromRoomParams{
		UserID: userID, RoomID: roomID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list issues from room: %w", err)
	}
	return issues, nil
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
