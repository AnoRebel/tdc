/*
Who pressed which button, once.

A press is an event in the room's log like any other, so the program that
posted the message receives it over the live connection it already holds. This
table is not that event — it is the record that makes "exactly once" a rule the
database keeps rather than something the application has to remember.

The distinction matters. Two presses arriving together, from two tabs or from a
retry, are two requests racing; asking "has this been pressed" and then writing
leaves a window between the question and the answer. A unique constraint has no
window.
*/
CREATE TABLE action_presses (
    id         UUID        PRIMARY KEY,

    -- The message offering the button, and the room it is in.
    event_id   TEXT        NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,

    -- Which button. Matches the action's name in the message's content.
    action     TEXT        NOT NULL,

    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    /*
      The address beside the identifier, for the reason events keep a sender
      address: it survives the account and works across servers. A press
      delivered to a program names who pressed, and that naming should not
      stop working because somebody left.
    */
    user_address TEXT      NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    /*
      One press of one button by one person.

      This is the ordinary rule: pressing twice is a mistake or a retry, and
      neither should count twice. An action marked `once` in the message is
      stricter still — one press in total, by anybody — and that is enforced
      by the query rather than by a constraint, because the constraint cannot
      know which actions carry the mark.
    */
    CONSTRAINT action_presses_once UNIQUE (event_id, user_id, action),

    CONSTRAINT action_presses_action_not_empty CHECK (length(trim(action)) > 0)
);

-- Answering "has this button been pressed at all", for a single-use action.
CREATE INDEX action_presses_event_action_idx ON action_presses (event_id, action);
