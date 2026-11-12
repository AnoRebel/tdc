/*
Where somebody is, as a place name, and who may see it.

One per account, shown wherever the person is: their directory profile, the
member lists they appear in. It used to be a free-text field of the directory
profile alone; this replaces it, so there is one answer to "where is this
person" rather than one per screen.

Sparse, the way user_privacy is: no row means no location, which is what most
people have.

Only a place name is ever stored. Detection sends coordinates to this server,
which matches them against a bundled list of towns and keeps the town — the
coordinates are not written here or anywhere else.
*/
CREATE TABLE user_locations (
    user_id     UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    -- "Nairobi, Kenya", or whatever the person wrote. Empty is no location.
    place       TEXT        NOT NULL DEFAULT '',

    -- 'manual' is typed by the person; 'automatic' is detected by their
    -- device and refreshed while they use the application.
    mode        TEXT        NOT NULL DEFAULT 'manual',
    detected_at TIMESTAMPTZ,

    -- The same three answers the directory uses: 'public' is anybody allowed
    -- to see the person at all, including people on other servers; 'members'
    -- is people signed in here; 'hidden' is nobody but the owner. Hidden
    -- unless chosen otherwise.
    visibility  TEXT        NOT NULL DEFAULT 'hidden',

    -- The directory's encoding: 0 public, 1 members, 2 hidden, compared with
    -- a viewer who is 0 anonymous or remote, 1 signed in, 2 the owner.
    location_rank SMALLINT GENERATED ALWAYS AS (
        CASE visibility WHEN 'public' THEN 0 WHEN 'members' THEN 1 ELSE 2 END
    ) STORED,

    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT user_locations_mode_valid CHECK (mode IN ('manual', 'automatic')),
    CONSTRAINT user_locations_visibility_valid
        CHECK (visibility IN ('public', 'members', 'hidden')),
    CONSTRAINT user_locations_place_length CHECK (char_length(place) <= 80)
);

/*
The one rule for showing somebody's location.

Every query that returns a person to somebody else asks this, rather than
comparing ranks itself, so "who may see where somebody is" means the same
thing in a member list as in the directory. The ceiling is the rank of
whatever the location appears inside — a directory profile passes its
details rank, so the location is never more visible than the profile showing
it; elsewhere it is 0.

Empty when the viewer may not see it, which is also what a person with no
location returns: the two are indistinguishable on purpose.
*/
CREATE FUNCTION person_location(person UUID, viewer_rank SMALLINT, ceiling SMALLINT) RETURNS TEXT
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT place FROM user_locations
          WHERE user_id = person AND GREATEST(location_rank, ceiling) <= viewer_rank),
        '')
$$;

/*
What the directory said, moved across.

The visibility is the profile's details rank, which already folds in the
profile's own visibility: a location shown only to members stays shown only to
members, and one in a hidden profile becomes hidden. Nothing becomes more
visible than it was.
*/
INSERT INTO user_locations (user_id, place, mode, visibility, updated_at)
SELECT user_id, left(trim(location), 80), 'manual',
       CASE details_rank WHEN 0 THEN 'public' WHEN 1 THEN 'members' ELSE 'hidden' END,
       updated_at
FROM directory_profiles
WHERE trim(location) <> '';

ALTER TABLE directory_profiles DROP COLUMN location;
