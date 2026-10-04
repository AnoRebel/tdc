/*
Engagements and their history.

Every read is scoped to the two parties in the query itself: an engagement is
found by somebody who is one of them, or not at all. There is no moderator path
here, because nobody outside an engagement may read it.
*/

-- name: InsertEngagement :one
INSERT INTO engagements (id, enquiry_id, client_id, developer_id, client_label, developer_label,
                         title, room_id, state)
VALUES (@id, @enquiry_id, @client_id, @developer_id, @client_label, @developer_label,
        @title, @room_id, 'discussion')
RETURNING *;

-- name: GetEngagementFor :one
-- An engagement, if the viewer is one of its two parties.
SELECT * FROM engagements
WHERE id = @id AND (client_id = @viewer::uuid OR developer_id = @viewer::uuid);

-- name: ListEngagementsFor :many
SELECT * FROM engagements
WHERE client_id = @viewer::uuid OR developer_id = @viewer::uuid
ORDER BY updated_at DESC
LIMIT 100;

-- name: MoveEngagement :execrows
-- A change of state, only from the state the caller saw: two people acting at
-- once cannot both move it, and nothing moves an engagement past a state it
-- is no longer in.
UPDATE engagements SET state = @to_state, updated_at = NOW()
WHERE id = @id AND state = @from_state;

-- name: RecordTransition :exec
INSERT INTO engagement_transitions (engagement_id, from_state, to_state, actor_id, actor_label, note)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListTransitions :many
SELECT * FROM engagement_transitions WHERE engagement_id = $1 ORDER BY id;

-- name: ArchiveEngagementRoom :exec
-- Closing an engagement's conversation to new messages, keeping what was said.
UPDATE rooms SET archived_at = NOW(), archived_by = @actor
WHERE id = @id AND archived_at IS NULL AND participant_key LIKE 'enquiry:%';

-- name: SetEngagementProject :execrows
-- Links an engagement to a project, or unlinks it with a null project, for one
-- of its two parties only.
UPDATE engagements
SET project_id = sqlc.narg('project_id')::uuid, updated_at = NOW()
WHERE id = @id AND (client_id = @actor::uuid OR developer_id = @actor::uuid);

-- name: ListProjectEngagementsFor :many
-- The engagements linked to a project that the viewer is a party to. Nobody
-- else learns that a project has any.
SELECT id, title, state FROM engagements
WHERE project_id = @project_id
  AND (client_id = @viewer::uuid OR developer_id = @viewer::uuid)
ORDER BY updated_at DESC
LIMIT 100;
