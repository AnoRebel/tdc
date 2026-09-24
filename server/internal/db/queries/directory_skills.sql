/*
The skill vocabulary and the review queue.
*/

-- name: ResolveSkill :one
-- A name, or another spelling of one, to the skill it means.
SELECT s.* FROM skills s
WHERE s.name_folded = @folded::text
   OR s.id = (SELECT skill_id FROM skill_aliases WHERE alias_folded = @folded::text)
LIMIT 1;

-- name: GetSkill :one
SELECT * FROM skills WHERE id = $1;

-- name: ListSkills :many
-- The vocabulary for somebody choosing: every active skill matching what they
-- typed, and their own proposals, which only they can attach.
SELECT * FROM skills
WHERE (status = 'active' OR proposed_by = @viewer::uuid)
  AND (@query::text = '' OR name ILIKE '%' || @query::text || '%'
       OR id IN (SELECT skill_id FROM skill_aliases
                 WHERE alias_folded LIKE '%' || lower(@query::text) || '%'))
ORDER BY (status = 'active') DESC, name
LIMIT @result_limit;

-- name: InsertSkill :one
INSERT INTO skills (id, name, name_folded, description, status, proposed_by, reviewed_by, reviewed_at)
VALUES (@id, @name, @name_folded, @description, @status, @proposed_by,
        @reviewed_by, CASE WHEN @status::text = 'active' THEN NOW() ELSE NULL END)
RETURNING *;

-- name: ListSkillQueue :many
-- Proposals waiting for a decision, oldest first, with who asked.
SELECT s.id, s.name, s.description, s.created_at,
       COALESCE(u.username, '')::text AS proposed_by_username,
       (SELECT count(*) FROM profile_skills ps WHERE ps.skill_id = s.id) AS profiles
FROM skills s
LEFT JOIN users u ON u.id = s.proposed_by
WHERE s.status = 'proposed'
ORDER BY s.created_at
LIMIT 200;

-- name: ApproveSkill :execrows
UPDATE skills SET status = 'active', reviewed_by = @reviewer, reviewed_at = NOW()
WHERE id = @id AND status = 'proposed';

-- name: DeleteProposedSkill :execrows
-- Rejecting, or the last step of merging. The proposal leaves every profile
-- it was on, by cascade.
DELETE FROM skills WHERE id = $1 AND status = 'proposed';

-- name: AddSkillAlias :exec
INSERT INTO skill_aliases (alias_folded, skill_id) VALUES ($1, $2)
ON CONFLICT (alias_folded) DO NOTHING;

-- name: MoveProfileSkills :exec
-- Everybody who had the proposal gets the skill it was merged into, keeping
-- what they wrote about it. Anybody who already had both keeps the one they
-- already had.
INSERT INTO profile_skills (user_id, skill_id, description, position, created_at, updated_at)
SELECT user_id, @merged_into::uuid, description, position, created_at, updated_at
FROM profile_skills WHERE skill_id = @merged_from::uuid
ON CONFLICT (user_id, skill_id) DO NOTHING;

-- name: ListOwnProfileSkills :many
-- Somebody's own skills, proposals included, so they can see what is waiting.
SELECT ps.skill_id, s.name, s.status, ps.description, ps.updated_at
FROM profile_skills ps
JOIN skills s ON s.id = ps.skill_id
WHERE ps.user_id = $1
ORDER BY ps.position, s.name;

-- name: ViewProfileSkills :many
-- A profile's skills as a viewer may see them: active ones only, and only
-- when the viewer may see what the profile says.
SELECT ps.skill_id, s.name, ps.description, ps.updated_at
FROM profile_skills ps
JOIN skills s ON s.id = ps.skill_id
JOIN directory_profiles p ON p.user_id = ps.user_id
WHERE ps.user_id = @user_id AND s.status = 'active'
  AND p.details_rank <= @viewer_rank::smallint
ORDER BY ps.position, s.name;

-- name: SkillAttachable :one
-- Whether somebody may put a skill on their own profile: any active skill,
-- and their own proposals.
SELECT EXISTS (
    SELECT 1 FROM skills
    WHERE id = @skill_id AND (status = 'active' OR proposed_by = @user_id::uuid)
);

-- name: SaveProfileSkill :exec
-- updated_at moves only when the description does, so a verification of the
-- claim can tell an edit from a reordering.
INSERT INTO profile_skills (user_id, skill_id, description, position)
VALUES (@user_id, @skill_id, @description, @position)
ON CONFLICT (user_id, skill_id) DO UPDATE SET
    position = EXCLUDED.position,
    description = EXCLUDED.description,
    updated_at = CASE WHEN profile_skills.description IS DISTINCT FROM EXCLUDED.description
                      THEN NOW() ELSE profile_skills.updated_at END;

-- name: DeleteProfileSkillsExcept :exec
DELETE FROM profile_skills
WHERE user_id = @user_id AND NOT (skill_id = ANY(@keep::uuid[]));
