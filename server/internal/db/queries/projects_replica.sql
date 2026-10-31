/*
A replica's tables, written from its stream.

Only the projector's results reach these: everything is an upsert to what the
stream says, followed by removing whatever it no longer says. The order the
writer uses keeps every foreign key satisfied at every step: definitions,
then issues, then what hangs from issues, then definitions nothing uses.
*/

-- name: AdoptReplicaRoom :exec
-- The room a replica's project, or one of its issues' discussions, lives in:
-- the home server's identifier, so its events name the same room everywhere.
-- Only ever a replica of that same home: an offer naming the identifier of a
-- room this server owns, or another server's, changes nothing.
INSERT INTO rooms (id, name, kind, visibility, home_server, federated)
VALUES ($1, $2, $3, $4, $5, TRUE)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, federated = TRUE
WHERE rooms.home_server = EXCLUDED.home_server AND rooms.kind = EXCLUDED.kind;

-- name: UpsertReplicaProject :exec
INSERT INTO projects (id, key, key_folded, name, description, visibility, time_zone,
                      cycle_policy, next_issue_number, federated)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE SET
    key = EXCLUDED.key, key_folded = EXCLUDED.key_folded, name = EXCLUDED.name,
    description = EXCLUDED.description, visibility = EXCLUDED.visibility,
    time_zone = EXCLUDED.time_zone, cycle_policy = EXCLUDED.cycle_policy,
    next_issue_number = EXCLUDED.next_issue_number, federated = EXCLUDED.federated,
    replica_error = '', updated_at = NOW();

-- name: SetReplicaError :exec
UPDATE projects SET replica_error = $2 WHERE id = $1;

-- name: ClearReplicaMembers :exec
DELETE FROM project_members WHERE project_id = $1;

-- name: ClearReplicaRemoteMembers :exec
DELETE FROM project_remote_members WHERE project_id = $1;

-- name: UpsertReplicaState :exec
INSERT INTO issue_states (id, project_id, name, name_folded, position, is_completion)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, name_folded = EXCLUDED.name_folded,
    position = EXCLUDED.position, is_completion = EXCLUDED.is_completion;

-- name: UpsertReplicaLabel :exec
INSERT INTO project_labels (id, project_id, name, name_folded, colour)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, name_folded = EXCLUDED.name_folded,
    colour = EXCLUDED.colour;

-- name: UpsertReplicaPriority :exec
INSERT INTO project_priorities (id, project_id, name, name_folded, rank)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, name_folded = EXCLUDED.name_folded,
    rank = EXCLUDED.rank;

-- name: UpsertReplicaCycle :exec
INSERT INTO cycles (id, project_id, name, starts_on, ends_on, starts_at, ends_at, closed_at, closed_policy)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, starts_on = EXCLUDED.starts_on,
    ends_on = EXCLUDED.ends_on, starts_at = EXCLUDED.starts_at, ends_at = EXCLUDED.ends_at,
    closed_at = EXCLUDED.closed_at, closed_policy = EXCLUDED.closed_policy;

-- name: UpsertReplicaMilestone :exec
INSERT INTO milestones (id, project_id, name, description, target_date, due_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
    target_date = EXCLUDED.target_date, due_at = EXCLUDED.due_at;

-- name: UpsertReplicaIssue :exec
INSERT INTO issues (id, project_id, number, title, description, state_id, author_id,
                    author_address, priority_id, deleted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, CASE WHEN sqlc.arg(deleted)::bool THEN NOW() END)
ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, description = EXCLUDED.description,
    state_id = EXCLUDED.state_id, author_id = EXCLUDED.author_id,
    author_address = EXCLUDED.author_address, priority_id = EXCLUDED.priority_id,
    deleted_at = CASE WHEN sqlc.arg(deleted)::bool THEN COALESCE(issues.deleted_at, NOW()) END,
    updated_at = NOW();

-- name: ClearReplicaIssueLinks :exec
-- Everything that hangs from a replica's issues, written again after.
WITH project_issues AS (SELECT issues.id FROM issues WHERE issues.project_id = $1),
     labels AS (DELETE FROM issue_labels WHERE issue_id IN (SELECT id FROM project_issues)),
     assignees AS (DELETE FROM issue_assignees WHERE issue_id IN (SELECT id FROM project_issues)),
     remote AS (DELETE FROM issue_remote_assignees WHERE issue_id IN (SELECT id FROM project_issues)),
     cycle AS (DELETE FROM issue_cycle WHERE issue_id IN (SELECT id FROM project_issues)),
     milestone AS (DELETE FROM issue_milestone WHERE issue_id IN (SELECT id FROM project_issues)),
     search AS (DELETE FROM issue_search WHERE issue_search.project_id = $1)
DELETE FROM issue_relations WHERE from_issue IN (SELECT id FROM project_issues);

-- name: AssignRemoteToIssue :execrows
INSERT INTO issue_remote_assignees (issue_id, address, home_server, assigned_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: UnassignRemoteFromIssue :execrows
DELETE FROM issue_remote_assignees WHERE issue_id = $1 AND address = $2;

-- name: RemoveReplicaStatesExcept :exec
DELETE FROM issue_states WHERE project_id = $1 AND NOT (id = ANY(sqlc.arg(keep)::uuid[]));

-- name: RemoveReplicaLabelsExcept :exec
DELETE FROM project_labels WHERE project_id = $1 AND NOT (id = ANY(sqlc.arg(keep)::uuid[]));

-- name: RemoveReplicaPrioritiesExcept :exec
DELETE FROM project_priorities WHERE project_id = $1 AND NOT (id = ANY(sqlc.arg(keep)::uuid[]));

-- name: RemoveReplicaCyclesExcept :exec
DELETE FROM cycles WHERE project_id = $1 AND NOT (id = ANY(sqlc.arg(keep)::uuid[]));

-- name: RemoveReplicaMilestonesExcept :exec
DELETE FROM milestones WHERE project_id = $1 AND NOT (id = ANY(sqlc.arg(keep)::uuid[]));

-- name: DiscardReplica :execrows
-- A replica, its issues' discussions and everything in them, gone.
WITH discussions AS (
    DELETE FROM rooms WHERE rooms.id IN (SELECT issues.id FROM issues WHERE issues.project_id = $1) AND rooms.home_server = $2
)
DELETE FROM rooms WHERE rooms.id = $1 AND rooms.home_server = $2;

/*
The home server's side: which servers hold a replica of its projects.
*/

-- name: AddEntitledReplicas :exec
-- Every server hosting a member of an opened project this server is home to is
-- to hold a replica — never of a replica held here, whose members and switch
-- are its home's to act on. One told to discard its copy that is entitled
-- again is offered it afresh.
INSERT INTO project_replicas (project_id, server, address)
SELECT DISTINCT prm.project_id, prm.home_server, ''
FROM project_remote_members prm
JOIN projects ON projects.id = prm.project_id AND projects.federated AND projects.key_folded NOT LIKE '%@%'
ON CONFLICT (project_id, server) DO UPDATE SET
    discarding = FALSE,
    offered_at = CASE WHEN project_replicas.discarding THEN NULL ELSE project_replicas.offered_at END,
    failures = CASE WHEN project_replicas.discarding THEN 0 ELSE project_replicas.failures END,
    next_attempt_at = CASE WHEN project_replicas.discarding THEN NULL ELSE project_replicas.next_attempt_at END;

-- name: DiscardUnentitledReplicas :exec
-- A server no longer hosting any member, or holding a project closed to
-- federation, is to discard its copy.
UPDATE project_replicas SET discarding = TRUE, failures = 0, next_attempt_at = NULL, last_error = ''
WHERE NOT discarding AND NOT EXISTS (
    SELECT 1 FROM project_remote_members prm
    JOIN projects ON projects.id = prm.project_id AND projects.federated AND projects.key_folded NOT LIKE '%@%'
    WHERE prm.project_id = project_replicas.project_id AND prm.home_server = project_replicas.server
);

-- name: StopDeliveringToDiscarding :exec
-- Nothing more is sent to a server told to discard its copy: its peer rows on
-- the project and on every issue's discussion go.
DELETE FROM room_peers rp
USING project_replicas pr
WHERE pr.discarding AND rp.peer = pr.server
  AND (rp.room_id = pr.project_id OR rp.room_id IN (SELECT id FROM issues WHERE issues.project_id = pr.project_id));

-- name: FederateProjectRooms :exec
-- A project's room and its issues' discussions are federated exactly while the
-- project is.
UPDATE rooms SET federated = projects.federated
FROM projects
WHERE (rooms.id = projects.id OR rooms.id IN (SELECT id FROM issues WHERE issues.project_id = projects.id))
  AND rooms.home_server = sqlc.arg(home) AND rooms.federated <> projects.federated;

-- name: DeliverToOfferedReplicas :exec
-- A server that accepted a project is a peer of its room and of every issue's
-- discussion, the new ones included.
INSERT INTO room_peers (room_id, peer, address)
SELECT rooms.id, pr.server, pr.address
FROM project_replicas pr
JOIN rooms ON rooms.id = pr.project_id OR rooms.id IN (SELECT id FROM issues WHERE issues.project_id = pr.project_id)
WHERE pr.offered_at IS NOT NULL AND NOT pr.discarding
ON CONFLICT (room_id, peer) DO NOTHING;

-- name: ClaimReplicaWork :many
-- Offers to make and discards to announce, claimed for one pass the way peers
-- owed events are.
UPDATE project_replicas SET next_attempt_at = NOW() + interval '1 minute'
WHERE (project_id, server) IN (
    SELECT project_id, server FROM project_replicas
    WHERE (offered_at IS NULL OR discarding)
      AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
    ORDER BY project_id, server
    LIMIT sqlc.arg(max)
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ReplicaOffered :exec
UPDATE project_replicas SET offered_at = NOW(), address = $3, failures = 0, next_attempt_at = NULL, last_error = ''
WHERE project_id = $1 AND server = $2;

-- name: ReplicaWorkFailed :exec
UPDATE project_replicas SET failures = failures + 1, last_error = $3,
    next_attempt_at = NOW() + sqlc.arg(retry_after)::interval
WHERE project_id = $1 AND server = $2;

-- name: ForgetReplica :exec
DELETE FROM project_replicas WHERE project_id = $1 AND server = $2 AND discarding;

-- name: RemoteMembersOn :many
-- A project's members on one server, which an offer names.
SELECT address FROM project_remote_members WHERE project_id = $1 AND home_server = $2 ORDER BY address;
