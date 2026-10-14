DROP TRIGGER IF EXISTS engagement_agreements_accepted ON engagement_agreements;
DROP FUNCTION IF EXISTS directory_agreement_accepted();
DROP TRIGGER IF EXISTS proposal_responses_by_party ON proposal_responses;
DROP FUNCTION IF EXISTS directory_response_by_party();

-- Only the first agreement of each engagement survives going back.
DELETE FROM engagement_agreements WHERE revision_id IS NOT NULL;
DROP INDEX IF EXISTS engagement_agreements_version_idx;
DROP INDEX IF EXISTS engagement_agreements_first_idx;
ALTER TABLE engagement_agreements DROP COLUMN IF EXISTS revision_id;
ALTER TABLE engagement_agreements ADD CONSTRAINT engagement_agreements_engagement_id_key UNIQUE (engagement_id);

DELETE FROM proposal_versions WHERE revision_id IS NOT NULL;
ALTER TABLE proposal_versions DROP COLUMN IF EXISTS revision_id;

DROP TABLE IF EXISTS agreement_revision_outcomes;
DROP TABLE IF EXISTS agreement_revisions;

CREATE OR REPLACE FUNCTION directory_refuse_change() RETURNS trigger AS $$
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
