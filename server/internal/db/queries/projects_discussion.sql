-- Issues' discussions, which are rooms, and their participants.

-- name: RemoveFromProjectDiscussions :exec
-- Takes somebody out of every discussion in a project, when they leave a
-- private one. A membership row left behind would keep delivering the
-- discussion's notifications to somebody who may no longer read it.
DELETE FROM room_members
USING issues
WHERE room_members.room_id = issues.id
  AND issues.project_id = @project_id
  AND room_members.user_id = @user_id;

-- name: RemoveDiscussionOutsiders :exec
-- When a project becomes private, everybody taking part in its discussions who
-- is not in the project stops taking part.
DELETE FROM room_members
USING issues
WHERE room_members.room_id = issues.id
  AND issues.project_id = $1
  AND NOT EXISTS (
      SELECT 1 FROM project_members
      WHERE project_members.project_id = $1
        AND project_members.user_id = room_members.user_id
  );

-- name: LatestMessageEdit :one
-- The newest edit of a message, so a message an issue was made from reads as
-- it reads in its channel now, rather than as it was first written.
SELECT * FROM events
WHERE room_id = @room_id
  AND type = 'message.edited'
  AND content->>'replaces' = @event_id::text
  AND redacted_at IS NULL
ORDER BY sequence DESC
LIMIT 1;

-- name: GetIssueCreation :one
-- The event that created an issue, which is where its origin is recorded.
SELECT * FROM events
WHERE room_id = @project_id
  AND type = 'issue.created'
  AND content->>'issue' = @issue_id::text
ORDER BY sequence
LIMIT 1;
