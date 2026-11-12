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

-- name: ListIssuesFromRoom :many
-- The issues made from a room's messages, that this person may see.
--
-- The project's visibility is the authorisation, joined rather than filtered
-- afterwards: an issue in a private project the reader is not in is never
-- fetched, so the room's members learn nothing about it — not its title, and
-- not that it exists.
SELECT
    (e.content->>'originEvent')::text AS event_id,
    i.id AS issue_id,
    i.number,
    i.title,
    p.key AS project_key,
    s.name AS state_name,
    s.is_completion
FROM events e
JOIN issues i ON i.id = (e.content->>'issue')::uuid
JOIN projects p ON p.id = i.project_id
JOIN issue_states s ON s.id = i.state_id
LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = @user_id
WHERE e.type = 'issue.created'
  AND e.content->>'originChannel' = @room_id::text
  AND i.deleted_at IS NULL
  AND (p.visibility = 'public' OR pm.user_id IS NOT NULL)
ORDER BY e.sequence;

-- name: WatchDiscussion :exec
-- Somebody taking part in an issue's discussion hears about everything said
-- in it, as a tracker's participants expect — unless they have said
-- otherwise: a setting of their own for this discussion is left alone, and so
-- is somebody who has asked to hear nothing anywhere.
INSERT INTO notification_preferences (user_id, room_id, level, muted)
SELECT @user_id, @room_id, 'all', FALSE
WHERE NOT EXISTS (
    SELECT 1 FROM notification_preferences global
    WHERE global.user_id = @user_id
      AND global.room_id IS NULL
      AND (global.level = 'none' OR global.muted)
)
ON CONFLICT (user_id, room_id) WHERE room_id IS NOT NULL DO NOTHING;

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
