/*
Interactive messages beyond a row of buttons.

Three things arrive together, because each is the state of a message somebody
can act on:

  - which version of a message's controls a press was made against, so a
    press through a view that has since been replaced is refused rather than
    counted;
  - the current controls of a message whose program has updated it in place;
  - what a program remembers about its exchange with one person in one
    conversation, between one step and the next.
*/

/*
A press now names the version of the controls it was made against, and carries
what was chosen or typed.

The version joins the uniqueness rule. "One press of one button by one person"
was right while a message's controls never changed; once a program can replace
them, a button called "approve" on the second version is a different question
from the one on the first, and somebody who answered the first must be able to
answer the second.
*/
ALTER TABLE action_presses
    ADD COLUMN version INTEGER NOT NULL DEFAULT 0,
    -- What was chosen from a menu or typed into a field. Absent for a button.
    ADD COLUMN value   TEXT;

ALTER TABLE action_presses DROP CONSTRAINT action_presses_once;
ALTER TABLE action_presses
    ADD CONSTRAINT action_presses_once UNIQUE (event_id, user_id, action, version);

DROP INDEX action_presses_event_action_idx;
CREATE INDEX action_presses_event_action_idx ON action_presses (event_id, action, version);

/*
A message's controls as they stand now, when its program has changed them.

The original event cannot be edited — it is signed, and the log is
append-only — so an update is an event of its own, and this row is what makes
"the current version" a single fact rather than something reconstructed by
reading every update in order.

The version is the whole point. An update names the version it replaces, and
the write succeeds only if that is still the version stored: two responses to
one press, racing, produce one update and one refusal, decided by the database
rather than by whichever request the application happened to look at first.

No row means the message is as it was posted, at version zero.
*/
CREATE TABLE message_interactions (
    event_id   TEXT        PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    version    INTEGER     NOT NULL,
    body       TEXT        NOT NULL,
    -- The controls, in the same shape a posted message carries them.
    actions    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    updated_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT message_interactions_version_positive CHECK (version > 0),
    CONSTRAINT message_interactions_actions_array CHECK (jsonb_typeof(actions) = 'array')
);

/*
What a program remembers about an exchange with one person.

Keyed to all three of the program, the conversation and the person, and to
nothing else: there is no query here that reads another program's context, or
one person's context on another's behalf, so "nobody else can read it" is a
property of the table's shape rather than of every handler remembering to
check.

Removed with any of the three. A program taken off the server takes what it
knew with it, and so does a conversation that no longer exists.
*/
CREATE TABLE integration_context (
    integration_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id        UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id        UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state          JSONB       NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (integration_id, room_id, user_id),

    /*
      Small, deliberately. This is where a program keeps the thread of a
      conversation — which build it asked about, what was chosen at step one —
      not a place to store documents. Sixteen kilobytes is a great deal of
      thread and very little of anything else.
    */
    CONSTRAINT integration_context_bounded CHECK (octet_length(state::text) <= 16384)
);

CREATE INDEX integration_context_room_idx ON integration_context (room_id);
