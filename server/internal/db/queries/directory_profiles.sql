/*
Directory profiles.

Every read that somebody other than the owner can reach goes through a query
taking the viewer's rank, and withholds in SQL rather than in Go: a hidden bio
is never read out of the database for a viewer who may not see it, so a
mistake in a handler cannot leak what the query did not return. This is the
discipline the member-list disclosure established, applied to strangers.
*/

-- name: GetOwnDirectoryProfile :one
-- The owner's own profile, all of it. Only ever called with the signed-in
-- person's own identifier.
SELECT p.*, u.username, u.display_name
FROM directory_profiles p
JOIN users u ON u.id = p.user_id
WHERE p.user_id = $1;

-- name: SaveDirectoryProfile :one
-- Creates the profile or replaces what it says. One statement for both, so a
-- person saving twice from two tabs cannot end up with two profiles or with
-- one half-written.
INSERT INTO directory_profiles (
    user_id, headline, bio, location, experience,
    contact_email, contact_phone, contact_url,
    visibility, details_visibility, contact_visibility,
    open_to_work, availability_note, availability_expires_at, accepts_enquiries
) VALUES (
    @user_id, @headline, @bio, @location, @experience,
    @contact_email, @contact_phone, @contact_url,
    @visibility, @details_visibility, @contact_visibility,
    @open_to_work, @availability_note, sqlc.narg('availability_expires_at'),
    sqlc.narg('accepts_enquiries')
)
ON CONFLICT (user_id) DO UPDATE SET
    accepts_enquiries = EXCLUDED.accepts_enquiries,
    open_to_work = EXCLUDED.open_to_work,
    availability_note = EXCLUDED.availability_note,
    availability_expires_at = EXCLUDED.availability_expires_at,
    headline = EXCLUDED.headline,
    bio = EXCLUDED.bio,
    location = EXCLUDED.location,
    experience = EXCLUDED.experience,
    contact_email = EXCLUDED.contact_email,
    contact_phone = EXCLUDED.contact_phone,
    contact_url = EXCLUDED.contact_url,
    visibility = EXCLUDED.visibility,
    details_visibility = EXCLUDED.details_visibility,
    contact_visibility = EXCLUDED.contact_visibility,
    updated_at = NOW()
RETURNING (xmax = 0) AS created;

-- name: DeleteDirectoryProfile :execrows
-- Removes the profile and, by cascade, its portfolio. The account, its
-- messages and its sessions are other tables and are not touched.
DELETE FROM directory_profiles WHERE user_id = $1;

/*
One profile as a particular viewer may see it.

The whole row is absent — no result at all — when the viewer may not see the
profile, which is the same answer as a person with no profile and a username
that belongs to nobody. One query for all three, so a refusal takes the same
path, and roughly the same time, whichever it is.

@viewer_rank is 0 for somebody not signed in, 1 for a member, and 2 for the
owner or a moderator acting as one.

The CASE arms withhold. Each carries an ELSE with an empty value and an outer
cast, because sqlc cannot see a type through a CASE; the details_shown and
contact_shown flags are what tell the caller the difference between "empty"
and "not yours to see", and the handler omits the fields on that basis.
*/
-- name: ViewDirectoryProfile :one
SELECT
    u.id AS user_id,
    u.username,
    u.display_name,
    p.visibility,
    (p.details_rank <= @viewer_rank::smallint)::boolean AS details_shown,
    (p.contact_rank <= @viewer_rank::smallint)::boolean AS contact_shown,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint THEN p.headline ELSE '' END)::text AS headline,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint THEN p.bio ELSE '' END)::text AS bio,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint THEN p.location ELSE '' END)::text AS location,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint THEN p.experience ELSE '' END)::text AS experience,
    (CASE WHEN p.contact_rank <= @viewer_rank::smallint THEN p.contact_email ELSE '' END)::text AS contact_email,
    (CASE WHEN p.contact_rank <= @viewer_rank::smallint THEN p.contact_phone ELSE '' END)::text AS contact_phone,
    (CASE WHEN p.contact_rank <= @viewer_rank::smallint THEN p.contact_url ELSE '' END)::text AS contact_url,
    -- Available only while the statement is current: past its expiry this is
    -- false with nothing about the profile changed.
    COALESCE(p.details_rank <= @viewer_rank::smallint
             AND p.open_to_work AND p.availability_expires_at > NOW(), FALSE)::boolean AS available,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint
               AND p.open_to_work AND p.availability_expires_at > NOW()
          THEN p.availability_note ELSE '' END)::text AS availability_note,
    (CASE WHEN p.details_rank <= @viewer_rank::smallint
               AND p.open_to_work AND p.availability_expires_at > NOW()
          THEN p.availability_expires_at ELSE 'epoch'::timestamptz END)::timestamptz AS available_until,
    -- Whether this viewer could send an enquiry: the owner's choice, or the
    -- default for the profile's visibility — and never to somebody who has
    -- blocked them, which reads exactly like a profile that takes none, so
    -- the block is not disclosed.
    (COALESCE(p.accepts_enquiries, p.visibility = 'public')
     AND NOT EXISTS (SELECT 1 FROM user_blocks b
                     WHERE b.blocker_id = u.id AND b.blocked_id = @viewer::uuid))::boolean AS accepts_enquiries,
    p.updated_at
FROM users u
JOIN directory_profiles p ON p.user_id = u.id
WHERE u.id = user_named(lower(@username::text))
  AND p.profile_rank <= @viewer_rank::smallint
  -- A suspended account is not somebody a client can engage, and listing one
  -- would advertise a person the server has stopped.
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until < NOW()));

-- name: ListPortfolioItems :many
-- The owner's own portfolio.
SELECT * FROM portfolio_items WHERE user_id = $1 ORDER BY position, created_at;

-- name: ViewPortfolioItems :many
-- A portfolio as a viewer may see it: all of it, or none of it. The rank is
-- read from the profile in the same statement, so there is no window in which
-- a profile made hidden still shows its links.
SELECT i.*
FROM portfolio_items i
JOIN directory_profiles p ON p.user_id = i.user_id
WHERE i.user_id = $1 AND p.details_rank <= @viewer_rank::smallint
ORDER BY i.position, i.created_at;

-- name: InsertPortfolioItem :exec
INSERT INTO portfolio_items (id, user_id, title, url, description, position)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdatePortfolioItem :execrows
-- Scoped to the owner, so an identifier from somebody else's portfolio
-- changes nothing. updated_at moves only when the content does, which is what
-- lets a verification tell an edited item from one merely reordered.
UPDATE portfolio_items
SET title = @title,
    url = @url,
    description = @description,
    position = @position,
    updated_at = CASE
        WHEN title IS DISTINCT FROM @title OR url IS DISTINCT FROM @url
             OR description IS DISTINCT FROM @description
        THEN NOW() ELSE updated_at END
WHERE id = @id AND user_id = @user_id;

-- name: DeletePortfolioItemsExcept :exec
-- Everything the owner did not send back, which is how an item is removed.
DELETE FROM portfolio_items
WHERE user_id = @user_id AND NOT (id = ANY(@keep::uuid[]));

-- name: ModerateDirectoryProfile :exec
-- A moderator replacing what a profile says — in practice, clearing it. The
-- visibility can be lowered in the same change, so abusive content can be
-- taken out of the directory while it is dealt with.
UPDATE directory_profiles
SET headline = @headline,
    bio = @bio,
    location = @location,
    experience = @experience,
    visibility = @visibility,
    updated_at = NOW()
WHERE user_id = @user_id;

-- name: GetDirectoryProfileOwnerByUsername :one
-- For moderation: whose profile a username names, if they have one.
SELECT u.id, u.username
FROM users u
JOIN directory_profiles p ON p.user_id = u.id
WHERE u.id = user_named(lower(@username::text));
