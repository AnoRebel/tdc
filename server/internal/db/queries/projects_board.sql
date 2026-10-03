-- A project's board: its states as columns, each a page of small cards.

-- name: ListBoardColumn :many
-- One page of one column, most urgent first and newest first within a level.
--
-- Keyset rather than offset: the cursor is the last card's (rank, number), so
-- a card moving out of the column while somebody pages through it cannot make
-- the next page skip or repeat one. Issues with no priority rank after every
-- level, as they do in the list.
--
-- Only what a card shows. A board is drawn on a phone on metered data, and a
-- description, a history or a comment count is what turns a page of cards into
-- a page of issues.
SELECT
    i.id, i.number, i.title, i.priority_id,
    COALESCE(p.rank, 2147483647)::int AS rank
FROM issues i
LEFT JOIN project_priorities p ON p.id = i.priority_id
WHERE i.project_id = @project_id
  AND i.state_id = @state_id
  AND i.deleted_at IS NULL
  AND (@label_name::text = '' OR EXISTS (
        SELECT 1 FROM issue_labels il JOIN project_labels l ON l.id = il.label_id
        WHERE il.issue_id = i.id AND l.name_folded = lower(@label_name::text)))
  AND (@priority_name::text = '' OR p.name_folded = lower(@priority_name::text))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM issue_assignees a
        WHERE a.issue_id = i.id AND a.user_id = sqlc.narg('assignee_id')::uuid))
  -- A cycle by its identifier, or 'none' for what is in no cycle.
  AND (@cycle_filter::text = ''
       OR (@cycle_filter::text = 'none'
           AND NOT EXISTS (SELECT 1 FROM issue_cycle ic WHERE ic.issue_id = i.id))
       OR EXISTS (SELECT 1 FROM issue_cycle ic
                  WHERE ic.issue_id = i.id AND ic.cycle_id::text = @cycle_filter::text))
  AND (COALESCE(p.rank, 2147483647) > @after_rank::int
       OR (COALESCE(p.rank, 2147483647) = @after_rank::int AND i.number < @after_number::bigint))
ORDER BY COALESCE(p.rank, 2147483647), i.number DESC
LIMIT @page_size;

-- name: CountBoardColumns :many
-- How many issues each state holds, so a column can say how many it has
-- without sending them.
--
-- Narrowed by the same filters as the cards, so a filtered board's counts are
-- of what it shows.
SELECT i.state_id, count(*)::bigint AS total
FROM issues i
LEFT JOIN project_priorities p ON p.id = i.priority_id
WHERE i.project_id = @project_id AND i.deleted_at IS NULL
  AND (@label_name::text = '' OR EXISTS (
        SELECT 1 FROM issue_labels il JOIN project_labels l ON l.id = il.label_id
        WHERE il.issue_id = i.id AND l.name_folded = lower(@label_name::text)))
  AND (@priority_name::text = '' OR p.name_folded = lower(@priority_name::text))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM issue_assignees a
        WHERE a.issue_id = i.id AND a.user_id = sqlc.narg('assignee_id')::uuid))
  -- A cycle by its identifier, or 'none' for what is in no cycle.
  AND (@cycle_filter::text = ''
       OR (@cycle_filter::text = 'none'
           AND NOT EXISTS (SELECT 1 FROM issue_cycle ic WHERE ic.issue_id = i.id))
       OR EXISTS (SELECT 1 FROM issue_cycle ic
                  WHERE ic.issue_id = i.id AND ic.cycle_id::text = @cycle_filter::text))
GROUP BY i.state_id;
