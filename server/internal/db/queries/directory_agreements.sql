/*
Proposals, responses and agreements. Inserts and reads only: there is no query
here that changes one, because nothing may.
*/

-- name: InsertProposalVersion :one
-- The next version for the engagement. Numbered here, in the statement that
-- inserts, so two people sending at once cannot both claim the same number:
-- the unique key refuses the second.
INSERT INTO proposal_versions (id, engagement_id, version, sent_by, sent_by_label,
                               scope, timeline, line_items, currency, total_minor)
SELECT @id, @engagement_id,
       COALESCE((SELECT max(version) FROM proposal_versions WHERE engagement_id = @engagement_id), 0) + 1,
       @sent_by, @sent_by_label, @scope, @timeline, @line_items, @currency, @total_minor
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
                                   developer_accepted_at, terms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetAgreement :one
SELECT * FROM engagement_agreements WHERE engagement_id = $1;

-- name: RequestCompletion :execrows
UPDATE engagements
SET completion_requested_by = @requested_by, completion_requested_at = NOW(), updated_at = NOW()
WHERE id = @id AND state = 'active';
