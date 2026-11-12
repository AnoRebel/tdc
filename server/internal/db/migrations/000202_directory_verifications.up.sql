/*
Verification: a moderator's attestation about one specific claim.

Not a badge on a person. A profile with one verified skill and nine
unverified ones shows exactly that — there is no row here that could stand for
"this person checks out", because nobody checked that. Each row names the one
claim it covers and holds a copy of what the claim said when it was checked, so
it can never be mistaken for having covered anything else, including a later
version of the same claim.
*/
CREATE TABLE profile_verifications (
    id            UUID        PRIMARY KEY,
    /*
      Deferred, and the reason is specific. Deleting several accounts in one
      statement — the moderator who verified a claim and the person who made
      it — cascades this row away through the profile while the moderator's
      deletion is nulling verified_by on the same row. Checked immediately,
      that update fails against a profile already gone; checked at commit, the
      row is gone too and there is nothing to fail.
    */
    user_id       UUID        NOT NULL REFERENCES directory_profiles(user_id) ON DELETE CASCADE
                              DEFERRABLE INITIALLY DEFERRED,

    /*
      Which claim. Both columns are required and the kind is one of four, so a
      verification cannot exist without saying exactly what it verified:

      - 'skill': claim_id is the skill; the claim is the skill and what the
        person wrote about it.
      - 'portfolio': claim_id is the portfolio item; its title, link and
        description.
      - 'identity': claim_id is the person; their name and the link they give
        as who they are elsewhere.
      - 'company': claim_id is the company; that they are part of it, in the
        role it gave them.
    */
    claim_kind    TEXT        NOT NULL,
    claim_id      UUID        NOT NULL,

    /*
      What the claim said when it was checked, and a digest of it.

      The digest is what is compared with the claim as it stands: when they
      differ, the claim was edited after it was verified and the verification
      no longer applies to it. The snapshot is kept so anybody reviewing the
      record later can see what was actually checked.
    */
    claim_digest  TEXT        NOT NULL,
    claim_snapshot JSONB      NOT NULL,

    -- What the moderator looked at. Required: a verification whose meaning
    -- nobody wrote down is one nobody can evaluate.
    note          TEXT        NOT NULL,
    verified_by   UUID        REFERENCES users(id) ON DELETE SET NULL,
    verified_by_label TEXT    NOT NULL DEFAULT '',
    verified_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Withdrawn by a moderator: the evidence turned out false, or lapsed.
    revoked_at    TIMESTAMPTZ,
    revoked_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
    revoke_reason TEXT,

    -- Cleared because the claim it covered was edited or removed. Kept rather
    -- than deleted, so the record of the verification survives it.
    cleared_at    TIMESTAMPTZ,

    CONSTRAINT profile_verifications_kind_valid
        CHECK (claim_kind IN ('skill', 'portfolio', 'identity', 'company')),
    CONSTRAINT profile_verifications_note_not_empty CHECK (length(trim(note)) > 0),
    CONSTRAINT profile_verifications_revocation_has_reason
        CHECK (revoked_at IS NULL OR length(trim(coalesce(revoke_reason, ''))) > 0)
);

-- At most one standing verification per claim.
CREATE UNIQUE INDEX profile_verifications_standing_idx
    ON profile_verifications (user_id, claim_kind, claim_id)
    WHERE revoked_at IS NULL AND cleared_at IS NULL;
