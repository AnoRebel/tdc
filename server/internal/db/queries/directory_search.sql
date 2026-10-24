/*
Searching the directory.

A query of its own, deliberately separate from SearchPeople: member search has
its own disclosure contract — a name and an address, to people on this server —
and this one has another. Neither calls the other and neither borrows the
other's columns, so a change to one cannot widen the other.

It starts from directory_profiles, which holds only people who chose to be
here, so it cannot reach an account that did not opt in however it is asked.

Every filter on something a profile may hide applies only where the viewer may
see that thing. Otherwise a hidden location could be discovered by filtering on
guesses and watching who appears; a profile whose details are hidden from the
viewer simply never matches a filter on them.

The page size is capped in the service, not trusted from the request.
*/

-- name: SearchDirectoryProfiles :many
SELECT
    u.username,
    u.display_name,
    (p.details_rank <= @viewer_rank::smallint)::boolean AS details_shown,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint THEN p.headline ELSE '' END)::text AS headline,
    person_location(p.user_id, @viewer_rank::smallint, p.details_rank)::text AS location,
    COALESCE(p.details_rank <= @viewer_rank::smallint
             AND p.open_to_work AND p.availability_expires_at > NOW(), FALSE)::boolean AS available,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint
               AND p.open_to_work AND p.availability_expires_at > NOW()
          THEN p.availability_note ELSE '' END)::text AS availability_note,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint
          THEN ARRAY(SELECT s.name FROM profile_skills ps
                     JOIN skills s ON s.id = ps.skill_id AND s.status = 'active'
                     WHERE ps.user_id = p.user_id
                     ORDER BY ps.position, s.name LIMIT 8)
          ELSE ARRAY[]::text[] END)::text[] AS skills,
    p.updated_at
FROM directory_profiles p
JOIN users u ON u.id = p.user_id
WHERE p.profile_rank <= @viewer_rank::smallint
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until < NOW()))
  AND (
        @query::text = ''
        OR u.username ILIKE '%' || @query::text || '%'
        OR u.display_name ILIKE '%' || @query::text || '%'
        OR (p.details_rank <= @viewer_rank::smallint
            AND (p.headline ILIKE '%' || @query::text || '%' OR p.bio ILIKE '%' || @query::text || '%'))
      )
  AND (
        sqlc.narg('skill_id')::uuid IS NULL
        OR (p.details_rank <= @viewer_rank::smallint AND EXISTS (
              SELECT 1 FROM profile_skills ps
              JOIN skills s ON s.id = ps.skill_id AND s.status = 'active'
              WHERE ps.user_id = p.user_id AND ps.skill_id = sqlc.narg('skill_id')::uuid))
      )
  AND (
        NOT @available_only::boolean
        OR (p.details_rank <= @viewer_rank::smallint
            AND p.open_to_work AND p.availability_expires_at > NOW())
      )
  AND (
        @location::text = ''
        OR person_location(p.user_id, @viewer_rank::smallint, p.details_rank) ILIKE '%' || @location::text || '%'
      )
  AND (
        @company::text = ''
        OR (p.details_rank <= @viewer_rank::smallint AND EXISTS (
              SELECT 1 FROM company_members m
              JOIN company_profiles c ON c.id = m.company_id
              WHERE m.user_id = p.user_id AND m.status = 'accepted'
                AND c.profile_rank <= @viewer_rank::smallint
                AND (c.slug_folded = lower(@company::text) OR c.name ILIKE '%' || @company::text || '%')))
      )
ORDER BY available DESC, p.updated_at DESC, p.user_id
LIMIT @result_limit OFFSET @result_offset;
