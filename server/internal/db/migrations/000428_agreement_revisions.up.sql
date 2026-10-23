/*
Revising an agreement that is in force.

Things change after two people agree — the scope grows, a date moves, a price
is renegotiated. A revision is how they change the agreement without rewriting
it: one of them opens a revision with a reason and amended terms, the two
negotiate it exactly as they negotiated the first proposal — versions that are
never changed, answers to the latest one — and only when both accept the same
version is a new agreement recorded beside the old one. Until then the
agreement in force is the one already recorded.

Nothing here is ever updated: a revision, how it ended, and every agreement are
inserted once and refused any change afterwards, the same as the records they
sit beside.
*/

/*
The account references that account deletion may blank now include the ones
these tables add. Everything else about the rule is unchanged.
*/
CREATE OR REPLACE FUNCTION directory_refuse_change() RETURNS trigger AS $$
DECLARE
    people CONSTANT text[] := ARRAY['sent_by', 'responder_id', 'client_id', 'developer_id',
                                    'opened_by', 'actor_id'];
    column_name text;
BEGIN
    IF (to_jsonb(NEW) - people) IS DISTINCT FROM (to_jsonb(OLD) - people) THEN
        RAISE EXCEPTION '% is a record and cannot be changed', TG_TABLE_NAME
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    FOREACH column_name IN ARRAY people LOOP
        IF to_jsonb(NEW) ? column_name
           AND (to_jsonb(NEW) -> column_name) IS DISTINCT FROM (to_jsonb(OLD) -> column_name)
           AND (to_jsonb(NEW) -> column_name) <> 'null'::jsonb THEN
            RAISE EXCEPTION '% is a record and cannot be changed', TG_TABLE_NAME
                USING ERRCODE = 'integrity_constraint_violation';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

/*
One revision of an engagement's agreement: who opened it, when, and why.

Numbered per engagement, in the statement that inserts, so two people opening
one at the same moment cannot both have one: the unique key refuses the
second.
*/
CREATE TABLE agreement_revisions (
    id              UUID        PRIMARY KEY,
    engagement_id   UUID        NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    number          INTEGER     NOT NULL,
    opened_by       UUID        REFERENCES users(id) ON DELETE SET NULL,
    opened_by_label TEXT        NOT NULL,
    reason          TEXT        NOT NULL,
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (engagement_id, number),
    CONSTRAINT agreement_revisions_reason_given CHECK (length(trim(reason)) > 0)
);

CREATE TRIGGER agreement_revisions_immutable BEFORE UPDATE ON agreement_revisions
    FOR EACH ROW EXECUTE FUNCTION directory_refuse_change();

/*
How a revision ended — accepted by both, declined, or withdrawn by whoever
opened it. A revision with no row here is open. One row per revision, by its
key: a revision ends once.
*/
CREATE TABLE agreement_revision_outcomes (
    revision_id   UUID        PRIMARY KEY REFERENCES agreement_revisions(id) ON DELETE CASCADE,
    engagement_id UUID        NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    outcome       TEXT        NOT NULL,
    actor_id      UUID        REFERENCES users(id) ON DELETE SET NULL,
    actor_label   TEXT        NOT NULL,
    note          TEXT        NOT NULL DEFAULT '',
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT agreement_revision_outcomes_valid CHECK (outcome IN ('accepted', 'declined', 'withdrawn'))
);

CREATE TRIGGER agreement_revision_outcomes_immutable BEFORE UPDATE ON agreement_revision_outcomes
    FOR EACH ROW EXECUTE FUNCTION directory_refuse_change();

-- A version sent within a revision names it; the first negotiation's do not.
ALTER TABLE proposal_versions
    ADD COLUMN revision_id UUID REFERENCES agreement_revisions(id) ON DELETE CASCADE;

/*
An engagement may now hold several agreements: the first, and one for each
revision both accepted. Still at most one that is not a revision, and at most
one per revision. The one in force is the latest version agreed.
*/
ALTER TABLE engagement_agreements DROP CONSTRAINT engagement_agreements_engagement_id_key;
ALTER TABLE engagement_agreements
    ADD COLUMN revision_id UUID UNIQUE REFERENCES agreement_revisions(id);
CREATE UNIQUE INDEX engagement_agreements_first_idx
    ON engagement_agreements (engagement_id) WHERE revision_id IS NULL;
CREATE UNIQUE INDEX engagement_agreements_version_idx
    ON engagement_agreements (engagement_id, version);

/*
A response is given by a party to the engagement, and nobody else.

The application only ever records the signed-in person as the responder; this
holds that for any other path too, so an acceptance can never be put in the
name of somebody who is not party to the engagement.
*/
CREATE FUNCTION directory_response_by_party() RETURNS trigger AS $$
BEGIN
    IF NEW.responder_id IS NULL OR NOT EXISTS (
        SELECT 1 FROM engagements e
        WHERE e.id = NEW.engagement_id
          AND NEW.responder_id IN (e.client_id, e.developer_id)
    ) THEN
        RAISE EXCEPTION 'only a party to the engagement answers its proposals'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER proposal_responses_by_party BEFORE INSERT ON proposal_responses
    FOR EACH ROW EXECUTE FUNCTION directory_response_by_party();

/*
An agreement is recorded only on the version both parties accepted.

The application records one when the second acceptance arrives; this refuses
one recorded any other way — with a party's acceptance missing, on a version
of another engagement, or, for a revision, on a version that is not that
revision's.
*/
CREATE FUNCTION directory_agreement_accepted() RETURNS trigger AS $$
DECLARE
    agreed proposal_versions%ROWTYPE;
BEGIN
    SELECT * INTO agreed FROM proposal_versions WHERE id = NEW.version_id;
    IF agreed.engagement_id IS DISTINCT FROM NEW.engagement_id
       OR agreed.version IS DISTINCT FROM NEW.version
       OR agreed.revision_id IS DISTINCT FROM NEW.revision_id
       OR NEW.client_id IS NULL OR NEW.developer_id IS NULL
       OR NOT EXISTS (SELECT 1 FROM proposal_responses
                      WHERE version_id = NEW.version_id AND kind = 'accept'
                        AND responder_id = NEW.client_id)
       OR NOT EXISTS (SELECT 1 FROM proposal_responses
                      WHERE version_id = NEW.version_id AND kind = 'accept'
                        AND responder_id = NEW.developer_id) THEN
        RAISE EXCEPTION 'an agreement needs both parties to have accepted the same version'
            USING ERRCODE = 'integrity_constraint_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER engagement_agreements_accepted BEFORE INSERT ON engagement_agreements
    FOR EACH ROW EXECUTE FUNCTION directory_agreement_accepted();
