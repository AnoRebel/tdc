/*
Company profiles, and the people who have agreed to be part of one.

Every read a viewer can reach lists accepted members only. A pending row is a
question the company asked and the person has not answered, and nothing on the
company's page may present it as an answer.
*/

-- name: CreateCompanyProfile :one
INSERT INTO company_profiles (id, slug, slug_folded, name, description, location, visibility, created_by)
VALUES ($1, $2, lower($2), $3, $4, $5, $6, $7)
RETURNING *;

-- name: AddCompanyFounder :exec
-- Whoever creates a company is its first administrator, accepted from the
-- start: they are consenting by creating it.
INSERT INTO company_members (company_id, user_id, role, administrator, status, invited_by, accepted_at)
VALUES ($1, $2, $3, TRUE, 'accepted', $2, NOW());

-- name: ViewCompanyProfile :one
-- A company as a viewer of the given rank may see it, or nothing.
SELECT * FROM company_profiles
WHERE slug_folded = lower(@slug::text) AND profile_rank <= @viewer_rank::smallint;

-- name: GetCompanyBySlug :one
-- For the company's own administrators, whatever its visibility.
SELECT * FROM company_profiles WHERE slug_folded = lower(@slug::text);

-- name: UpdateCompanyProfile :one
UPDATE company_profiles
SET name = @name, description = @description, location = @location,
    visibility = @visibility, updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: DeleteCompanyProfile :exec
DELETE FROM company_profiles WHERE id = $1;

-- name: ListAcceptedCompanyMembers :many
-- The people a company may list: those who agreed.
SELECT u.id, u.username, u.display_name, m.role, m.administrator, m.accepted_at
FROM company_members m
JOIN users u ON u.id = m.user_id
WHERE m.company_id = $1 AND m.status = 'accepted'
ORDER BY m.accepted_at, u.username;

-- name: ListPendingCompanyMembers :many
-- Invitations still unanswered. Only a company's administrators see these,
-- and they already know: they sent them.
SELECT u.id, u.username, u.display_name, m.role, m.invited_at
FROM company_members m
JOIN users u ON u.id = m.user_id
WHERE m.company_id = $1 AND m.status = 'pending'
ORDER BY m.invited_at;

-- name: GetCompanyMembership :one
SELECT * FROM company_members WHERE company_id = $1 AND user_id = $2;

-- name: InviteCompanyMember :execrows
-- Asks somebody to be listed. Does nothing if they already are, or were
-- already asked: inviting twice is not a second question.
INSERT INTO company_members (company_id, user_id, role, invited_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (company_id, user_id) DO NOTHING;

-- name: AcceptCompanyMembership :execrows
UPDATE company_members
SET status = 'accepted', accepted_at = NOW()
WHERE company_id = $1 AND user_id = $2 AND status = 'pending';

-- name: RemoveCompanyMember :execrows
-- Leaving, declining, or being removed. The row goes, so neither side displays
-- it from this moment.
DELETE FROM company_members WHERE company_id = $1 AND user_id = $2;

-- name: CountCompanyAdministrators :one
SELECT count(*) FROM company_members
WHERE company_id = $1 AND status = 'accepted' AND administrator;

-- name: ListMyCompanies :many
-- Somebody's own associations, answered and unanswered.
SELECT c.id, c.slug, c.name, c.visibility, m.role, m.administrator, m.status,
       m.invited_at, m.accepted_at
FROM company_members m
JOIN company_profiles c ON c.id = m.company_id
WHERE m.user_id = $1
ORDER BY m.status DESC, c.name;

-- name: ViewProfileCompanies :many
-- The companies a profile references, as a viewer may see them: accepted
-- associations only, with companies the viewer may see, on a profile whose
-- details the viewer may see — the companies are part of what it says.
SELECT c.id, c.slug, c.name, m.role
FROM company_members m
JOIN company_profiles c ON c.id = m.company_id
JOIN directory_profiles p ON p.user_id = m.user_id
WHERE m.user_id = @user_id AND m.status = 'accepted'
  AND c.profile_rank <= @viewer_rank::smallint
  AND p.details_rank <= @viewer_rank::smallint
ORDER BY m.accepted_at;
