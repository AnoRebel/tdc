/*
Proposals, responses and agreements. Inserts and reads only: there is no query
here that changes one, because nothing may.
*/

-- name: InsertProposalVersion :one
-- The next version for the engagement. Numbered here, in the statement that
-- inserts, so two people sending at once cannot both claim the same number:
-- the unique key refuses the second.
-- A version sent within a revision carries the revision; the numbering runs on
-- across them, so "version 4" names one set of terms on the engagement.
INSERT INTO proposal_versions (id, engagement_id, version, sent_by, sent_by_label,
                               scope, timeline, line_items, currency, total_minor, revision_id)
SELECT @id, @engagement_id,
       COALESCE((SELECT max(version) FROM proposal_versions WHERE engagement_id = @engagement_id), 0) + 1,
       @sent_by, @sent_by_label, @scope, @timeline, @line_items, @currency, @total_minor,
       sqlc.narg(revision_id)::uuid
RETURNING *;

-- name: ListProposalVersions :many
SELECT * FROM proposal_versions WHERE engagement_id = $1 ORDER BY version;

-- name: LatestProposalVersion :one
SELECT * FROM proposal_versions WHERE engagement_id = $1 ORDER BY version DESC LIMIT 1;

-- name: InsertProposalResponse :one
INSERT INTO proposal_responses (id, version_id, engagement_id, responder_id, responder_label,
                                kind, part, comment)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListProposalResponses :many
SELECT * FROM proposal_responses WHERE engagement_id = $1 ORDER BY created_at, id;

-- name: AcceptancesOf :many
-- Who has accepted one version, and when.
SELECT responder_id, created_at FROM proposal_responses
WHERE version_id = $1 AND kind = 'accept';

-- name: InsertAgreement :one
INSERT INTO engagement_agreements (id, engagement_id, version_id, version, client_id, developer_id,
                                   client_label, developer_label, client_accepted_at,
                                   developer_accepted_at, terms, revision_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetAgreement :one
-- The agreement in force: the latest version both parties accepted.
SELECT * FROM engagement_agreements WHERE engagement_id = $1 ORDER BY version DESC LIMIT 1;

-- name: ListAgreements :many
-- Every agreement on an engagement, the first one first.
SELECT * FROM engagement_agreements WHERE engagement_id = $1 ORDER BY version;

-- name: LockEngagement :exec
-- Holds an engagement still for the rest of a transaction, so that opening,
-- answering and ending a revision, and finishing the work, happen one at a
-- time on it.
SELECT id FROM engagements WHERE id = $1 FOR UPDATE;

-- name: InsertRevision :one
INSERT INTO agreement_revisions (id, engagement_id, number, opened_by, opened_by_label, reason)
SELECT @id, @engagement_id,
       COALESCE((SELECT max(number) FROM agreement_revisions WHERE engagement_id = @engagement_id), 0) + 1,
       @opened_by, @opened_by_label, @reason
RETURNING *;

-- name: ListRevisions :many
-- Every revision of an engagement's agreement with how it ended, if it has.
SELECT r.id, r.engagement_id, r.number, r.opened_by, r.opened_by_label, r.reason, r.opened_at,
       o.outcome, o.actor_label AS outcome_by, o.note AS outcome_note, o.occurred_at AS outcome_at
FROM agreement_revisions r
LEFT JOIN agreement_revision_outcomes o ON o.revision_id = r.id
WHERE r.engagement_id = $1
ORDER BY r.number;

-- name: GetOpenRevision :one
-- The revision under negotiation, if there is one.
SELECT r.* FROM agreement_revisions r
WHERE r.engagement_id = $1
  AND NOT EXISTS (SELECT 1 FROM agreement_revision_outcomes o WHERE o.revision_id = r.id)
ORDER BY r.number DESC LIMIT 1;

-- name: InsertRevisionOutcome :exec
INSERT INTO agreement_revision_outcomes (revision_id, engagement_id, outcome, actor_id, actor_label, note)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: RequestCompletion :execrows
UPDATE engagements
SET completion_requested_by = @requested_by, completion_requested_at = NOW(), updated_at = NOW()
WHERE id = @id AND state = 'active';
