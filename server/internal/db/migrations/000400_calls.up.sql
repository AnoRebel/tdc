/*
Calls: who is talking in a conversation right now.

A call belongs to one room and is open until its last participant leaves. At
most one is open per room, so "join the call" in a conversation always means
the same call. That a call happened, and for how long, is recorded in the room's
own log (call.started, call.ended); who is in it at this moment is live state,
kept here and nowhere else, and never media — nothing a call carries passes
through or is stored by the server.
*/
CREATE TABLE calls (
    id          UUID        PRIMARY KEY,
    room_id     UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    started_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at    TIMESTAMPTZ
);

CREATE UNIQUE INDEX calls_one_open_per_room ON calls (room_id) WHERE ended_at IS NULL;

CREATE TABLE call_participants (
    call_id    UUID        NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Null while they are in the call.
    left_at    TIMESTAMPTZ,
    -- What they are sending, as they report it, so others can show it.
    audio      BOOLEAN     NOT NULL DEFAULT TRUE,
    video      BOOLEAN     NOT NULL DEFAULT FALSE,
    PRIMARY KEY (call_id, user_id)
);

CREATE INDEX call_participants_present ON call_participants (user_id) WHERE left_at IS NULL;

/*
Calling, like posting, is something every member does unless an operator says
otherwise: the Member role holds both permissions, so switching calls on does
not also mean an afternoon of granting them. An operator who wants messaging
without calls withdraws them, from Member or for one channel.
*/
INSERT INTO role_permissions (role_id, permission, scope_id, granted)
SELECT id, permission, NULL, TRUE
FROM roles
CROSS JOIN (VALUES ('call.start'), ('call.join')) AS added(permission)
WHERE roles.name = 'Member' AND roles.built_in
ON CONFLICT (role_id, permission, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO NOTHING;
