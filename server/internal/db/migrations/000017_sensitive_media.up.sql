-- Media somebody has marked as sensitive, to be obscured until a viewer
-- chooses to see it.
--
-- Who marked it matters as much as whether it is marked. An uploader may
-- change their mind about their own file; a moderator's decision is not theirs
-- to undo, or marking would be advisory and the people most likely to post
-- something unwelcome would be the people able to unmark it.
--
-- Nullable rather than a boolean with a separate "marked_by": an unmarked
-- attachment has nobody who marked it, and two columns that must agree is two
-- columns that can disagree.
ALTER TABLE attachments ADD COLUMN sensitive_by TEXT;

-- What kind of decision it was: 'uploader', 'moderator', or 'automatic' where
-- an operator has configured something to classify. Null when unmarked.
ALTER TABLE attachments ADD COLUMN sensitive_source TEXT;

ALTER TABLE attachments ADD CONSTRAINT attachments_sensitive_source_known
    CHECK (sensitive_source IS NULL
        OR sensitive_source IN ('uploader', 'moderator', 'automatic'));

-- Both columns are set together or neither is, so "marked but by nobody" and
-- "marked by somebody, somehow" cannot be stored.
ALTER TABLE attachments ADD CONSTRAINT attachments_sensitive_complete
    CHECK ((sensitive_by IS NULL) = (sensitive_source IS NULL));
