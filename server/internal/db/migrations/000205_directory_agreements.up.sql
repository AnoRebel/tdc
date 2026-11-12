/*
Proposals, negotiation, and the agreement record.

Everything here is evidence of what two people said, and evidence that can be
edited afterwards is not evidence. So a proposal version, a response to one, and
an agreement can be inserted and never changed: there is no update path in the
application, and the database refuses one from anywhere else.
*/

/*
Refuses any change to a row of a table it guards — with one exception.

When an account is deleted, the references to it are set to null so the record
outlives it for the other person. That is an update, and refusing it would make
an account impossible to delete. So a change is allowed only when every column
but the account references is untouched and those references only become null;
anything else, from anywhere, is refused.
*/
CREATE FUNCTION directory_refuse_change() RETURNS trigger AS $$
DECLARE
    people CONSTANT text[] := ARRAY['sent_by', 'responder_id', 'client_id', 'developer_id'];
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
One version of a proposal for an engagement.

Revising a proposal sends a new version; every earlier one stays, readable by
both people. Money is integer minor units with its currency, never a float: a
price that is 0.1 short because of how a number was stored is not a price
anybody agreed to.
*/
CREATE TABLE proposal_versions (
    id            UUID        PRIMARY KEY,
    engagement_id UUID        NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    version       INTEGER     NOT NULL,
    sent_by       UUID        REFERENCES users(id) ON DELETE SET NULL,
    sent_by_label TEXT        NOT NULL,
    scope         TEXT        NOT NULL,
    timeline      TEXT        NOT NULL,
    -- The deliverables, each with its price: [{"description": "...", "amountMinor": 150000}].
    line_items    JSONB       NOT NULL,
    currency      TEXT        NOT NULL,
    total_minor   BIGINT      NOT NULL,
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (engagement_id, version),
    CONSTRAINT proposal_versions_currency_valid CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT proposal_versions_total_not_negative CHECK (total_minor >= 0)
);

CREATE TRIGGER proposal_versions_immutable BEFORE UPDATE ON proposal_versions
    FOR EACH ROW EXECUTE FUNCTION directory_refuse_change();

/*
A response to a version: accepted, declined, or changes asked for — the last
naming the part of the proposal it is about ("scope", "timeline", or "item:N"
for one line), so the request is attached to what it questions.
*/
CREATE TABLE proposal_responses (
    id             UUID        PRIMARY KEY,
    version_id     UUID        NOT NULL REFERENCES proposal_versions(id) ON DELETE CASCADE,
    engagement_id  UUID        NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    responder_id   UUID        REFERENCES users(id) ON DELETE SET NULL,
    responder_label TEXT       NOT NULL,
    kind           TEXT        NOT NULL,
    part           TEXT,
    comment        TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT proposal_responses_kind_valid CHECK (kind IN ('accept', 'decline', 'request_changes')),
    CONSTRAINT proposal_responses_change_names_part
        CHECK (kind <> 'request_changes' OR (part IS NOT NULL AND length(trim(comment)) > 0))
);

-- One acceptance per person per version.
CREATE UNIQUE INDEX proposal_responses_one_acceptance_idx
    ON proposal_responses (version_id, responder_id) WHERE kind = 'accept';

CREATE TRIGGER proposal_responses_immutable BEFORE UPDATE ON proposal_responses
    FOR EACH ROW EXECUTE FUNCTION directory_refuse_change();

/*
The agreement: both people accepted the same version.

It holds a copy of what was agreed, both identities and both acceptance times,
so it stands on its own — an export of it, or a read years later, needs nothing
else. It records what was agreed and nothing about money moving, guarantees, or
anybody settling a dispute, because none of those exist here.
*/
CREATE TABLE engagement_agreements (
    id                    UUID        PRIMARY KEY,
    engagement_id         UUID        NOT NULL UNIQUE REFERENCES engagements(id) ON DELETE CASCADE,
    version_id            UUID        NOT NULL REFERENCES proposal_versions(id),
    version               INTEGER     NOT NULL,
    client_id             UUID        REFERENCES users(id) ON DELETE SET NULL,
    developer_id          UUID        REFERENCES users(id) ON DELETE SET NULL,
    client_label          TEXT        NOT NULL,
    developer_label       TEXT        NOT NULL,
    client_accepted_at    TIMESTAMPTZ NOT NULL,
    developer_accepted_at TIMESTAMPTZ NOT NULL,
    terms                 JSONB       NOT NULL,
    recorded_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER engagement_agreements_immutable BEFORE UPDATE ON engagement_agreements
    FOR EACH ROW EXECUTE FUNCTION directory_refuse_change();
