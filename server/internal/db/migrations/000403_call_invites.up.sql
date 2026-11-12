/*
People invited into one call without being in its conversation: the people of
a call merged into another, and later those let in by a call's link. The
invitation admits them to that call only; it grants nothing in the
conversation, whose messages, members and history stay closed to them.
*/
CREATE TABLE call_invites (
    call_id    UUID        NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invited_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (call_id, user_id)
);
