/*
Directory profiles: a second, separate decision to be found.

An account is complete without any of this. A profile is a row somebody creates
deliberately, and the table is sparse the way user_privacy is: no row means no
profile, and no profile means absent from the directory — not listed with
nothing in it.

Nothing here touches users, user_privacy or user_presence. Those answer who may
see somebody who already shares a conversation with them; this answers who may
find a stranger. The two are different disclosures, and keeping them in
different tables with no shared query is what stops a change to one quietly
loosening the other.
*/
CREATE TABLE directory_profiles (
    user_id     UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    -- What the person says about themselves. All optional: a profile with
    -- nothing but a visibility setting is a valid profile.
    headline    TEXT        NOT NULL DEFAULT '',
    bio         TEXT        NOT NULL DEFAULT '',
    location    TEXT        NOT NULL DEFAULT '',
    experience  TEXT        NOT NULL DEFAULT '',

    /*
      Contact details, deliberately separate from the rest.

      "Reachable by strangers" is a different disclosure from "findable by
      strangers", and somebody may want the second without the first — which
      is why these carry their own visibility, defaulting to hidden.
    */
    contact_email TEXT      NOT NULL DEFAULT '',
    contact_phone TEXT      NOT NULL DEFAULT '',
    contact_url   TEXT      NOT NULL DEFAULT '',

    /*
      Who may see what.

      'public' is anybody permitted to browse the directory, including somebody
      not signed in where the server is discoverable; 'members' is people
      signed in to this server; 'hidden' is nobody but the owner.

      Three settings rather than one, for the reason user_privacy has three:
      the profile as a whole, what it says (the details), and how to reach its
      owner are separate questions. A section is never more visible than the
      profile it belongs to, which the ranks below encode.
    */
    visibility         TEXT NOT NULL DEFAULT 'members',
    details_visibility TEXT NOT NULL DEFAULT 'members',
    -- The most restrictive setting unless the owner chooses otherwise.
    contact_visibility TEXT NOT NULL DEFAULT 'hidden',

    /*
      The same three decisions as numbers a query can compare with a viewer.

      0 is public, 1 members, 2 hidden; a viewer is 0 when anonymous, 1 when
      signed in, and 2 when it is the owner or a moderator acting as one. A
      field is visible when its rank is at most the viewer's.

      Generated rather than computed in each query, so there is exactly one
      statement of "a section is never more visible than its profile" — the
      GREATEST — and no query can forget it.
    */
    profile_rank SMALLINT GENERATED ALWAYS AS (
        CASE visibility WHEN 'public' THEN 0 WHEN 'members' THEN 1 ELSE 2 END
    ) STORED,
    details_rank SMALLINT GENERATED ALWAYS AS (
        GREATEST(
            CASE visibility WHEN 'public' THEN 0 WHEN 'members' THEN 1 ELSE 2 END,
            CASE details_visibility WHEN 'public' THEN 0 WHEN 'members' THEN 1 ELSE 2 END
        )
    ) STORED,
    contact_rank SMALLINT GENERATED ALWAYS AS (
        GREATEST(
            CASE visibility WHEN 'public' THEN 0 WHEN 'members' THEN 1 ELSE 2 END,
            CASE contact_visibility WHEN 'public' THEN 0 WHEN 'members' THEN 1 ELSE 2 END
        )
    ) STORED,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT directory_profiles_visibility_valid
        CHECK (visibility IN ('public', 'members', 'hidden')),
    CONSTRAINT directory_profiles_details_visibility_valid
        CHECK (details_visibility IN ('public', 'members', 'hidden')),
    CONSTRAINT directory_profiles_contact_visibility_valid
        CHECK (contact_visibility IN ('public', 'members', 'hidden'))
);

-- Browsing reads only profiles somebody may see at all.
CREATE INDEX directory_profiles_rank_idx ON directory_profiles (profile_rank, updated_at DESC);

/*
Links to work somebody has done.

Links rather than uploads: a profile is read on phones over metered data, and
the thing being linked to already lives somewhere. Each item has an identifier
of its own because a moderator can verify one, and that verification has to
name exactly which item it covers.
*/
CREATE TABLE portfolio_items (
    id          UUID        PRIMARY KEY,
    user_id     UUID        NOT NULL REFERENCES directory_profiles(user_id) ON DELETE CASCADE,
    title       TEXT        NOT NULL,
    url         TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    position    INTEGER     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT portfolio_items_title_not_empty CHECK (length(trim(title)) > 0)
);

CREATE INDEX portfolio_items_user_idx ON portfolio_items (user_id, position);

/*
An organisation.

Somebody creates it and becomes its first administrator. It is not an account
and cannot sign in; it is a page people are associated with, which is all the
spec asks of it.
*/
CREATE TABLE company_profiles (
    id          UUID        PRIMARY KEY,
    slug        TEXT        NOT NULL,
    slug_folded TEXT        NOT NULL UNIQUE,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    location    TEXT        NOT NULL DEFAULT '',
    -- No 'hidden': a company nobody can find has no reason to exist, and one
    -- that wants to stop existing can be deleted.
    visibility  TEXT        NOT NULL DEFAULT 'members',
    profile_rank SMALLINT GENERATED ALWAYS AS (
        CASE visibility WHEN 'public' THEN 0 ELSE 1 END
    ) STORED,
    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT company_profiles_name_not_empty CHECK (length(trim(name)) > 0),
    CONSTRAINT company_profiles_visibility_valid CHECK (visibility IN ('public', 'members'))
);

/*
Who a company says is part of it — which is only true once they agree.

A company can ask; only the person can accept. Until they do the row is
'pending' and nothing displays it: a company page listing somebody who never
agreed would be the company speaking for them, which is exactly what the
consent requirement exists to stop. Leaving deletes the row, so the association
ends on both sides at once rather than lingering on one.
*/
CREATE TABLE company_members (
    company_id  UUID        NOT NULL REFERENCES company_profiles(id) ON DELETE CASCADE,
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The role they hold, in the company's words: "Founder", "Backend lead".
    role        TEXT        NOT NULL DEFAULT '',
    -- May change the company's page and invite others. The creator is the
    -- first; there is always at least one while the company exists.
    administrator BOOLEAN   NOT NULL DEFAULT FALSE,
    status      TEXT        NOT NULL DEFAULT 'pending',
    invited_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    invited_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ,

    PRIMARY KEY (company_id, user_id),
    CONSTRAINT company_members_status_valid CHECK (status IN ('pending', 'accepted')),
    CONSTRAINT company_members_accepted_has_time
        CHECK ((status = 'accepted') = (accepted_at IS NOT NULL))
);

CREATE INDEX company_members_user_idx ON company_members (user_id);
