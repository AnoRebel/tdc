-- Finding issues, and what was said about them.

-- name: IndexIssue :exec
-- Writes an issue's searchable text. The issue's own row is read, never
-- written: rebuilding the index is never a change to the record.
INSERT INTO issue_search (issue_id, project_id, title, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id) DO UPDATE
SET title = EXCLUDED.title, description = EXCLUDED.description;

-- name: UnindexIssue :exec
DELETE FROM issue_search WHERE issue_id = $1;

-- name: SearchIssues :many
-- Issues matching a query and every filter given, among the projects the
-- searcher may see, most recently changed first.
--
-- The project's visibility is joined, not filtered afterwards: an issue in a
-- private project the searcher is not in is never fetched, so a mistake in
-- the filtering cannot leak it — a mistake in a join is an empty result.
--
-- websearch_to_tsquery never raises a syntax error, whatever is typed at it.
-- An empty query is no text filter at all, which is how a filter-only search
-- works; the filters narrow together, each one given having to hold.
SELECT
    i.id, i.number, i.title, i.updated_at,
    p.key AS project_key, p.name AS project_name,
    s.name AS state_name, s.is_completion,
    ts_headline('english', x.title || ' — ' || x.description,
                websearch_to_tsquery('english', @query::text),
                'StartSel=<mark>, StopSel=</mark>, MaxFragments=2, MaxWords=18, MinWords=5')::text
        AS highlighted
FROM issue_search x
JOIN issues i ON i.id = x.issue_id
JOIN projects p ON p.id = i.project_id
JOIN issue_states s ON s.id = i.state_id
LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = @user_id
WHERE (p.visibility = 'public' OR pm.user_id IS NOT NULL)
  AND i.deleted_at IS NULL
  AND (@query::text = '' OR x.search_vector @@ websearch_to_tsquery('english', @query::text))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
  AND (@state_name::text = '' OR s.name_folded = lower(@state_name::text))
  AND (sqlc.narg('completed')::boolean IS NULL OR s.is_completion = sqlc.narg('completed')::boolean)
  AND (@label_name::text = '' OR EXISTS (
        SELECT 1 FROM issue_labels il JOIN project_labels l ON l.id = il.label_id
        WHERE il.issue_id = i.id AND l.name_folded = lower(@label_name::text)))
  AND (@priority_name::text = '' OR EXISTS (
        SELECT 1 FROM project_priorities pr
        WHERE pr.id = i.priority_id AND pr.name_folded = lower(@priority_name::text)))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM issue_assignees a
        WHERE a.issue_id = i.id AND a.user_id = sqlc.narg('assignee_id')::uuid))
ORDER BY i.updated_at DESC, i.number DESC
LIMIT @result_limit;

-- name: SearchIssueTitlesFuzzy :many
-- Search-as-you-type over titles: a prefix or a near miss, which a tsquery
-- cannot find. The same visibility join as above.
SELECT
    i.id, i.number, i.title, i.updated_at,
    p.key AS project_key, p.name AS project_name,
    s.name AS state_name, s.is_completion
FROM issue_search x
JOIN issues i ON i.id = x.issue_id
JOIN projects p ON p.id = i.project_id
JOIN issue_states s ON s.id = i.state_id
LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = @user_id
WHERE (p.visibility = 'public' OR pm.user_id IS NOT NULL)
  AND i.deleted_at IS NULL
  AND (x.title ILIKE '%' || @query::text || '%' OR similarity(x.title, @query::text) > 0.3)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
ORDER BY i.updated_at DESC
LIMIT @result_limit;

-- name: SearchIssueDiscussions :many
-- What was said about issues: messages in the discussions of issues the
-- searcher may see, optionally one issue's alone.
--
-- Messages are indexed exactly as in any channel; what differs is who may read
-- them, which for a discussion is whoever may see its issue — so that is what
-- is joined, rather than the room's membership, which only records who has
-- taken part.
SELECT
    m.event_id, m.sender_address, m.sent_at,
    i.id AS issue_id, i.number, i.title,
    p.key AS project_key,
    ts_headline('english', m.body, websearch_to_tsquery('english', @query::text),
                'StartSel=<mark>, StopSel=</mark>, MaxFragments=2, MaxWords=18, MinWords=5')::text
        AS highlighted
FROM message_search m
JOIN issues i ON i.id = m.room_id
JOIN projects p ON p.id = i.project_id
LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = @user_id
WHERE (p.visibility = 'public' OR pm.user_id IS NOT NULL)
  AND i.deleted_at IS NULL
  AND m.search_vector @@ websearch_to_tsquery('english', @query::text)
  AND (sqlc.narg('issue_id')::uuid IS NULL OR i.id = sqlc.narg('issue_id')::uuid)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
ORDER BY m.sent_at DESC
LIMIT @result_limit;
