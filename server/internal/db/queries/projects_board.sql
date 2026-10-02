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
  AND (COALESCE(p.rank, 2147483647) > @after_rank::int
       OR (COALESCE(p.rank, 2147483647) = @after_rank::int AND i.number < @after_number::bigint))
ORDER BY COALESCE(p.rank, 2147483647), i.number DESC
LIMIT @page_size;

-- name: CountBoardColumns :many
-- How many issues each state holds, so a column can say how many it has
-- without sending them.
SELECT state_id, count(*)::bigint AS total
FROM issues
WHERE project_id = $1 AND deleted_at IS NULL
GROUP BY state_id;
