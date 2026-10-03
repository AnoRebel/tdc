-- Cycles and milestones.

-- name: SetProjectPlanning :one
UPDATE projects SET time_zone = @time_zone, cycle_policy = @cycle_policy
WHERE id = @id
RETURNING *;

-- name: CreateCycle :one
INSERT INTO cycles (id, project_id, name, starts_on, ends_on, starts_at, ends_at, created_by)
VALUES (@id, @project_id, @name, @starts_on, @ends_on, @starts_at, @ends_at, @created_by)
RETURNING *;

-- name: GetCycle :one
SELECT * FROM cycles WHERE id = $1;

-- name: ListCyclesOfProject :many
SELECT * FROM cycles WHERE project_id = $1 ORDER BY starts_at, name;

-- name: UpdateCycle :one
UPDATE cycles
SET name = @name, starts_on = @starts_on, ends_on = @ends_on,
    starts_at = @starts_at, ends_at = @ends_at
WHERE id = @id
RETURNING *;

-- name: SetCycleInstants :exec
-- After a project's zone changes: the same days, at different instants.
UPDATE cycles SET starts_at = @starts_at, ends_at = @ends_at WHERE id = @id;

-- name: DeleteCycle :exec
DELETE FROM cycles WHERE id = $1;

-- name: CountOverlappingCycles :one
-- Cycles of the project sharing any instant with [starts_at, ends_at), apart
-- from the one being changed.
SELECT count(*)::bigint FROM cycles
WHERE project_id = @project_id
  AND id <> @except_id
  AND starts_at < @ends_at
  AND ends_at > @starts_at;

-- name: ListCyclesToClose :many
-- Cycles that have ended and whose end has not been dealt with, oldest first.
SELECT * FROM cycles
WHERE closed_at IS NULL AND ends_at <= @now
ORDER BY ends_at
LIMIT @batch;

-- name: LockCycle :one
-- Taken by whoever deals with a cycle's end, so two servers sharing a database
-- cannot both do it. SKIP LOCKED: the other simply moves on.
SELECT * FROM cycles WHERE id = $1 AND closed_at IS NULL FOR UPDATE SKIP LOCKED;

-- name: CloseCycle :exec
UPDATE cycles SET closed_at = @closed_at, closed_policy = @closed_policy WHERE id = @id;

-- name: NextCycleAfter :one
-- Where unfinished work goes when a cycle ends: the first cycle starting no
-- earlier than that one ended, that has not itself ended.
SELECT * FROM cycles
WHERE project_id = @project_id
  AND id <> @after_id
  AND starts_at >= @ended_at
  AND closed_at IS NULL
ORDER BY starts_at
LIMIT 1;

-- name: ListUnfinishedIssuesInCycle :many
-- Issues still in a cycle whose state does not mean done, in number order.
SELECT i.*
FROM issue_cycle ic
JOIN issues i ON i.id = ic.issue_id
JOIN issue_states s ON s.id = i.state_id
WHERE ic.cycle_id = $1 AND i.deleted_at IS NULL AND NOT s.is_completion
ORDER BY i.number;

-- name: GetIssueCycle :one
SELECT c.* FROM issue_cycle ic JOIN cycles c ON c.id = ic.cycle_id WHERE ic.issue_id = $1;

-- name: SetIssueCycle :exec
INSERT INTO issue_cycle (issue_id, cycle_id) VALUES (@issue_id, @cycle_id)
ON CONFLICT (issue_id) DO UPDATE SET cycle_id = EXCLUDED.cycle_id, added_at = NOW();

-- name: ClearIssueCycle :exec
DELETE FROM issue_cycle WHERE issue_id = $1;

-- name: CountCycleIssues :many
-- How many issues each of a project's cycles holds, and how many are done.
SELECT ic.cycle_id,
       count(*)::bigint AS total,
       count(*) FILTER (WHERE s.is_completion)::bigint AS completed
FROM issue_cycle ic
JOIN issues i ON i.id = ic.issue_id
JOIN issue_states s ON s.id = i.state_id
WHERE i.project_id = $1 AND i.deleted_at IS NULL
GROUP BY ic.cycle_id;

-- name: ListIssuesInCycle :many
SELECT i.*
FROM issue_cycle ic JOIN issues i ON i.id = ic.issue_id
WHERE ic.cycle_id = $1 AND i.deleted_at IS NULL
ORDER BY i.number DESC;

-- name: ListCyclesForIssues :many
SELECT ic.issue_id, c.id, c.name, c.starts_on, c.ends_on
FROM issue_cycle ic JOIN cycles c ON c.id = ic.cycle_id
WHERE ic.issue_id = ANY(@issue_ids::uuid[]);

-- name: CreateMilestone :one
INSERT INTO milestones (id, project_id, name, description, target_date, due_at, created_by)
VALUES (@id, @project_id, @name, @description, @target_date, @due_at, @created_by)
RETURNING *;

-- name: GetMilestone :one
SELECT * FROM milestones WHERE id = $1;

-- name: ListMilestonesOfProject :many
SELECT * FROM milestones WHERE project_id = $1
ORDER BY target_date NULLS LAST, name;

-- name: UpdateMilestone :one
UPDATE milestones
SET name = @name, description = @description, target_date = @target_date, due_at = @due_at
WHERE id = @id
RETURNING *;

-- name: SetMilestoneDue :exec
UPDATE milestones SET due_at = @due_at WHERE id = @id;

-- name: DeleteMilestone :exec
DELETE FROM milestones WHERE id = $1;

-- name: GetIssueMilestone :one
SELECT m.* FROM issue_milestone im JOIN milestones m ON m.id = im.milestone_id WHERE im.issue_id = $1;

-- name: SetIssueMilestone :exec
INSERT INTO issue_milestone (issue_id, milestone_id) VALUES (@issue_id, @milestone_id)
ON CONFLICT (issue_id) DO UPDATE SET milestone_id = EXCLUDED.milestone_id, added_at = NOW();

-- name: ClearIssueMilestone :exec
DELETE FROM issue_milestone WHERE issue_id = $1;

-- name: CountMilestoneIssues :many
SELECT im.milestone_id,
       count(*)::bigint AS total,
       count(*) FILTER (WHERE s.is_completion)::bigint AS completed
FROM issue_milestone im
JOIN issues i ON i.id = im.issue_id
JOIN issue_states s ON s.id = i.state_id
WHERE i.project_id = $1 AND i.deleted_at IS NULL
GROUP BY im.milestone_id;

-- name: ListIssuesInMilestone :many
SELECT i.*
FROM issue_milestone im JOIN issues i ON i.id = im.issue_id
WHERE im.milestone_id = $1 AND i.deleted_at IS NULL
ORDER BY i.number DESC;

-- name: ListMilestoneCycles :many
-- The cycles a milestone's issues are in: what it spans.
SELECT DISTINCT c.*
FROM issue_milestone im
JOIN issues i ON i.id = im.issue_id
JOIN issue_cycle ic ON ic.issue_id = im.issue_id
JOIN cycles c ON c.id = ic.cycle_id
WHERE im.milestone_id = $1 AND i.deleted_at IS NULL
ORDER BY c.starts_at;

-- name: ListMilestonesForIssues :many
SELECT im.issue_id, m.id, m.name, m.target_date
FROM issue_milestone im JOIN milestones m ON m.id = im.milestone_id
WHERE im.issue_id = ANY(@issue_ids::uuid[]);
